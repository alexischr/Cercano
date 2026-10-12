package state

import (
	"cercano/source/server/internal/updatecoord/operation"
	"context"
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
