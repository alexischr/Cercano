package state

import (
	"cercano/source/server/internal/updatecoord/operation"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCompletedSelectionOperationRequiresBothRecords(t *testing.T) {
	root := t.TempDir()
	s, e := Open(root, "test-install")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	op := startCurrentOperation(t, s, "9.9.9")
	j := journalFor(op.ID, "9.9.9")
	target := SelectedVersion{Version: j.TargetVersion, Directory: j.StagedVersionDir, ArtifactSHA256: j.VerifiedArtifactSHA256, Generation: 1}
	if _, found, e := s.CompletedSelectionOperation(ctx, target); e != nil || found {
		t.Fatal("missing journal", found, e)
	}
	rev, e := s.SaveActivationJournal(ctx, 0, j)
	if e != nil {
		t.Fatal(e)
	}
	for _, cp := range []JournalCheckpoint{JournalSwitchIntent, JournalSelected, JournalHealthVerified, JournalComplete} {
		j.Checkpoint = cp
		rev, e = s.SaveActivationJournal(ctx, rev, j)
		if e != nil {
			t.Fatal(e)
		}
		if _, found, e := s.CompletedSelectionOperation(ctx, target); e != nil || found {
			t.Fatal("unfinished operation announced", cp, found, e)
		}
	}
	rec, r, e := s.LoadOperationRecord(ctx, op.ID)
	if e != nil {
		t.Fatal(e)
	}
	rec.State = operation.StateComplete
	rec.HealthVerified = true
	if _, e = s.SaveOperationRecord(ctx, r, rec); e != nil {
		t.Fatal(e)
	}
	if id, found, e := s.CompletedSelectionOperation(ctx, target); e != nil || !found || id != op.ID {
		t.Fatal("complete operation missing", id, found, e)
	}
	// New checks must not hide the still-selected completed installation.
	if _, e = NewAdapter(s).Start(ctx, "10.0.0"); e != nil {
		t.Fatal(e)
	}
	if id, found, e := s.CompletedSelectionOperation(ctx, target); e != nil || !found || id != op.ID {
		t.Fatal("history lookup failed", id, found, e)
	}
	for _, bad := range []SelectedVersion{
		{Version: "other", Directory: target.Directory, ArtifactSHA256: target.ArtifactSHA256, Generation: 1},
		{Version: target.Version, Directory: "other", ArtifactSHA256: target.ArtifactSHA256, Generation: 1},
		{Version: target.Version, Directory: target.Directory, ArtifactSHA256: target.ArtifactSHA256, Generation: 2},
	} {
		if _, found, e := s.CompletedSelectionOperation(ctx, bad); e != nil || found {
			t.Fatal("mismatch accepted", found, e)
		}
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(root, "test-install")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if id, found, e := s.CompletedSelectionOperation(ctx, target); e != nil || !found || id != op.ID {
		t.Fatal("durable lookup failed", id, found, e)
	}
	before, revision, e := s.LoadActivationJournal(ctx, op.ID)
	if e != nil || revision != rev || before != j {
		t.Fatal("journal changed", before, revision, e)
	}
}

func TestCompletedSelectionManifestBindsHistoricalInstalledFacts(t *testing.T) {
	ctx := context.Background()
	s, e := Open(t.TempDir(), "test-install")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	op := startCurrentOperation(t, s, "9.9.9")
	j := journalFor(op.ID, "9.9.9")
	target := SelectedVersion{Version: j.TargetVersion, Directory: j.StagedVersionDir, ArtifactSHA256: j.VerifiedArtifactSHA256, Generation: 1}
	m := ActivationManifest{SchemaVersion: ActivationManifestSchemaVersion, InstallID: s.InstallID(), OpID: op.ID, TargetVersion: j.TargetVersion, StagedVersionDir: j.StagedVersionDir, VerifiedArtifactSHA256: j.VerifiedArtifactSHA256, Members: []ManifestMember{{Path: "bin/cercano", Length: 10, SHA256: strings.Repeat("a", 64)}}}
	if _, e = s.SaveActivationManifest(ctx, m); e != nil {
		t.Fatal(e)
	}
	rev, e := s.SaveActivationJournal(ctx, 0, j)
	if e != nil {
		t.Fatal(e)
	}
	for _, cp := range []JournalCheckpoint{JournalSwitchIntent, JournalSelected, JournalHealthVerified, JournalComplete} {
		j.Checkpoint = cp
		rev, e = s.SaveActivationJournal(ctx, rev, j)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.LoadCompletedSelectionManifest(ctx, target); !errors.Is(e, ErrRecordNotFound) {
			t.Fatal("unfinished operation accepted", cp, e)
		}
	}
	rec, r, e := s.LoadOperationRecord(ctx, op.ID)
	if e != nil {
		t.Fatal(e)
	}
	rec.State = operation.StateComplete
	rec.HealthVerified = true
	if _, e = s.SaveOperationRecord(ctx, r, rec); e != nil {
		t.Fatal(e)
	}
	if _, e = NewAdapter(s).Start(ctx, "10.0.0"); e != nil {
		t.Fatal(e)
	}
	got, e := s.LoadCompletedSelectionManifest(ctx, target)
	if e != nil || got.OpID != op.ID || got.Members[0] != m.Members[0] {
		t.Fatal("historical installed manifest lost", got, e)
	}
	for _, kind := range []string{"version", "directory", "artifact", "missing", "corrupt"} {
		t.Run(kind, func(t *testing.T) {
			bad := m
			switch kind {
			case "version":
				bad.TargetVersion = "1.0.0"
			case "directory":
				bad.StagedVersionDir = "other"
			case "artifact":
				bad.VerifiedArtifactSHA256 = strings.Repeat("b", 64)
			}
			payload, _ := json.Marshal(bad)
			if kind == "corrupt" {
				payload = []byte(`{}`)
			}
			if kind == "missing" {
				_, e = s.db.Exec(`DELETE FROM activation_manifests WHERE op_id=?`, op.ID)
			} else {
				_, e = s.db.Exec(`UPDATE activation_manifests SET manifest_json=? WHERE op_id=?`, string(payload), op.ID)
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.LoadCompletedSelectionManifest(ctx, target); e == nil {
				t.Fatal("unsafe manifest accepted")
			}
			original, _ := json.Marshal(m)
			if _, e = s.db.Exec(`INSERT OR REPLACE INTO activation_manifests(op_id,install_id,revision,manifest_json) VALUES(?,?,1,?)`, op.ID, s.InstallID(), string(original)); e != nil {
				t.Fatal(e)
			}
		})
	}
	if _, e = s.db.Exec(`UPDATE install_state SET next_op_id=? WHERE install_id=?`, op.ID, s.InstallID()); e != nil {
		t.Fatal(e)
	}
	if _, e = s.LoadCompletedSelectionManifest(ctx, target); !errors.Is(e, ErrCorruptDatabase) {
		t.Fatal("unallocated selected operation accepted", e)
	}

}
