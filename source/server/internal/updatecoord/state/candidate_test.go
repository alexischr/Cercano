package state

import (
	"context"
	"errors"
	"strings"
	"testing"

	"cercano/source/server/internal/updatecoord/operation"
)

// selectedCandidateFixture builds a store whose CURRENT, allocated operation
// has a persisted manifest and a journal sitting at the approved Selected
// checkpoint. prior/priorGen set the journal's prior-selection block; the
// selection generation the child would present is priorGen+1. The operation
// record's lifecycle state is left untouched (it is whatever Start produced).
func selectedCandidateFixture(t *testing.T, prior string, priorGen int64) (*Store, int64, ActivationManifest, SelectedVersion) {
	t.Helper()
	ctx := context.Background()
	s, err := Open(t.TempDir(), "test-install")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	snap := startCurrentOperation(t, s, "9.9.9")
	j := journalFor(snap.ID, "9.9.9")
	j.PriorSelectedVersion = prior
	j.PriorSelectionGeneration = priorGen
	if prior == "" {
		// No prior selection block: validation requires the explicit-none form
		// (generation 0 and empty digest) when there is no prior version.
		j.PriorSelectionGeneration = 0
		j.PriorSelectionDigest = ""
	} else {
		j.PriorSelectionDigest = strings.Repeat("b", 64)
	}
	m := ActivationManifest{SchemaVersion: ActivationManifestSchemaVersion, InstallID: s.InstallID(), OpID: snap.ID, TargetVersion: j.TargetVersion, StagedVersionDir: j.StagedVersionDir, VerifiedArtifactSHA256: j.VerifiedArtifactSHA256, Members: []ManifestMember{{Path: "bin/cercano", Length: 10, SHA256: strings.Repeat("c", 64)}}}
	if _, err = s.SaveActivationManifest(ctx, m); err != nil {
		t.Fatal(err)
	}
	rev, err := s.SaveActivationJournal(ctx, 0, j)
	if err != nil {
		t.Fatal(err)
	}
	for _, cp := range []JournalCheckpoint{JournalSwitchIntent, JournalSelected} {
		j.Checkpoint = cp
		rev, err = s.SaveActivationJournal(ctx, rev, j)
		if err != nil {
			t.Fatal(err)
		}
	}
	target := SelectedVersion{Version: j.TargetVersion, Directory: j.StagedVersionDir, ArtifactSHA256: j.VerifiedArtifactSHA256, Generation: priorGen + 1}
	return s, snap.ID, m, target
}

func setOperationState(t *testing.T, s *Store, id int64, state operation.State) {
	t.Helper()
	ctx := context.Background()
	rec, rev, err := s.LoadOperationRecord(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	rec.State = state
	if _, err = s.SaveOperationRecord(ctx, rev, rec); err != nil {
		t.Fatal(err)
	}
}

func TestLoadCandidateSelectionManifestAcceptsSelectedCandidate(t *testing.T) {
	ctx := context.Background()
	s, opID, m, target := selectedCandidateFixture(t, "", 0)
	for _, st := range []operation.State{operation.StateRestarting, operation.StateHealthCheck} {
		setOperationState(t, s, opID, st)
		got, err := s.LoadCandidateSelectionManifest(ctx, target)
		if err != nil {
			t.Fatalf("candidate startup refused in %q: %v", st, err)
		}
		if got.OpID != opID || got.TargetVersion != m.TargetVersion || got.StagedVersionDir != m.StagedVersionDir || got.VerifiedArtifactSHA256 != m.VerifiedArtifactSHA256 || len(got.Members) != len(m.Members) || got.Members[0] != m.Members[0] {
			t.Fatalf("loaded manifest mismatch in %q: got %+v want %+v", st, got, m)
		}
	}
	// A selection whose journal carries an explicit prior-selection block
	// resolves by the exact generation after that prior.
	s2, opID2, _, target2 := selectedCandidateFixture(t, "8.8.8", 6)
	setOperationState(t, s2, opID2, operation.StateRestarting)
	if _, err := s2.LoadCandidateSelectionManifest(ctx, target2); err != nil {
		t.Fatalf("prior-generation selection refused: %v", err)
	}
}

func TestLoadCandidateSelectionManifestRefusesOutsideStartupWindow(t *testing.T) {
	ctx := context.Background()
	for _, st := range []operation.State{operation.StateInstalling, operation.StateCleanup, operation.StateComplete, operation.StateFailed} {
		t.Run(string(st), func(t *testing.T) {
			s, opID, _, target := selectedCandidateFixture(t, "", 0)
			setOperationState(t, s, opID, st)
			if _, err := s.LoadCandidateSelectionManifest(ctx, target); !errors.Is(err, ErrRecordNotFound) {
				t.Fatalf("lifecycle %q accepted: %v", st, err)
			}
		})
	}
}

func TestLoadCandidateSelectionManifestRefusesInconsistentSelection(t *testing.T) {
	ctx := context.Background()
	t.Run("wrong generation", func(t *testing.T) {
		s, opID, _, target := selectedCandidateFixture(t, "", 0)
		setOperationState(t, s, opID, operation.StateRestarting)
		target.Generation += 2
		if _, err := s.LoadCandidateSelectionManifest(ctx, target); !errors.Is(err, ErrCorruptDatabase) {
			t.Fatalf("wrong generation accepted: %v", err)
		}
	})
	t.Run("wrong version", func(t *testing.T) {
		s, opID, _, target := selectedCandidateFixture(t, "", 0)
		setOperationState(t, s, opID, operation.StateRestarting)
		target.Version = "other"
		if _, err := s.LoadCandidateSelectionManifest(ctx, target); !errors.Is(err, ErrCorruptDatabase) {
			t.Fatalf("wrong version accepted: %v", err)
		}
	})
	t.Run("prepared checkpoint", func(t *testing.T) {
		// The journal must sit at the approved Selected checkpoint: at
		// Prepared the selection has not been journaled as selected yet.
		ctx := context.Background()
		s, err := Open(t.TempDir(), "test-install")
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		snap := startCurrentOperation(t, s, "9.9.9")
		j := journalFor(snap.ID, "9.9.9")
		if _, err = s.SaveActivationJournal(ctx, 0, j); err != nil {
			t.Fatal(err)
		}
		target := SelectedVersion{Version: j.TargetVersion, Directory: j.StagedVersionDir, ArtifactSHA256: j.VerifiedArtifactSHA256, Generation: 1}
		setOperationState(t, s, snap.ID, operation.StateRestarting)
		if _, err := s.LoadCandidateSelectionManifest(ctx, target); !errors.Is(err, ErrRecordNotFound) {
			t.Fatalf("prepared checkpoint accepted: %v", err)
		}
	})
	t.Run("missing journal", func(t *testing.T) {
		s, opID, _, target := selectedCandidateFixture(t, "", 0)
		setOperationState(t, s, opID, operation.StateRestarting)
		if _, err := s.db.Exec(`DELETE FROM activation_journals WHERE op_id=?`, opID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.LoadCandidateSelectionManifest(ctx, target); !errors.Is(err, ErrRecordNotFound) {
			t.Fatalf("missing journal accepted: %v", err)
		}
	})
	t.Run("missing manifest", func(t *testing.T) {
		s, opID, _, target := selectedCandidateFixture(t, "", 0)
		setOperationState(t, s, opID, operation.StateRestarting)
		if _, err := s.db.Exec(`DELETE FROM activation_manifests WHERE op_id=?`, opID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.LoadCandidateSelectionManifest(ctx, target); !errors.Is(err, ErrRecordNotFound) {
			t.Fatalf("missing manifest accepted: %v", err)
		}
	})
	t.Run("unallocated operation", func(t *testing.T) {
		s, opID, _, target := selectedCandidateFixture(t, "", 0)
		setOperationState(t, s, opID, operation.StateRestarting)
		if _, err := s.db.Exec(`UPDATE install_state SET next_op_id=? WHERE install_id=?`, opID, s.InstallID()); err != nil {
			t.Fatal(err)
		}
		if _, err := s.LoadCandidateSelectionManifest(ctx, target); !errors.Is(err, ErrCorruptDatabase) {
			t.Fatalf("unallocated operation accepted: %v", err)
		}
	})
	t.Run("no operation", func(t *testing.T) {
		s, err := Open(t.TempDir(), "test-install")
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		target := SelectedVersion{Version: "9.9.9", Directory: "staged-9.9.9", ArtifactSHA256: strings.Repeat("a", 64), Generation: 1}
		if _, err := s.LoadCandidateSelectionManifest(ctx, target); !errors.Is(err, ErrRecordNotFound) {
			t.Fatalf("absent operation accepted: %v", err)
		}
	})
}

func TestLoadCompletedSelectionManifestRefusesSelectedCandidate(t *testing.T) {
	ctx := context.Background()
	// The completed-only API must keep refusing a selection that is journaled
	// as selected but whose operation is still restarting or health-checking.
	s, opID, _, target := selectedCandidateFixture(t, "", 0)
	setOperationState(t, s, opID, operation.StateRestarting)
	if _, err := s.LoadCompletedSelectionManifest(ctx, target); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("candidate accepted by completed API: %v", err)
	}
	setOperationState(t, s, opID, operation.StateHealthCheck)
	if _, err := s.LoadCompletedSelectionManifest(ctx, target); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("candidate accepted by completed API: %v", err)
	}
}
