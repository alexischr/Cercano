package state

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestCompletedSelectionRejectsMalformedAndAmbiguousRows(t *testing.T) {
	for _, kind := range []string{"bad-payload", "bad-revision", "wrong-install", "ambiguous"} {
		t.Run(kind, func(t *testing.T) {
			s, e := Open(t.TempDir(), "test-install")
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			ctx := context.Background()
			op := startCurrentOperation(t, s, "9.9.9")
			j := journalFor(op.ID, "9.9.9")
			if _, e = s.SaveActivationJournal(ctx, 0, j); e != nil {
				t.Fatal(e)
			}
			target := SelectedVersion{Version: j.TargetVersion, Directory: j.StagedVersionDir, ArtifactSHA256: j.VerifiedArtifactSHA256, Generation: 1}
			switch kind {
			case "bad-payload":
				_, e = s.db.Exec(`UPDATE activation_journals SET journal_json='{'`)
			case "bad-revision":
				_, e = s.db.Exec(`UPDATE activation_journals SET revision=0`)
			case "wrong-install":
				_, e = s.db.Exec(`UPDATE activation_journals SET install_id='foreign'`)
			case "ambiguous":
				other := j
				other.OpID++
				payload, _ := json.Marshal(other)
				_, e = s.db.Exec(`INSERT INTO activation_journals(op_id,install_id,revision,journal_json) VALUES(?,?,1,?)`, other.OpID, other.InstallID, string(payload))
			}
			if e != nil {
				t.Fatal(e)
			}
			var before string
			if e = s.db.QueryRow(`SELECT journal_json FROM activation_journals WHERE op_id=?`, j.OpID).Scan(&before); e != nil {
				t.Fatal(e)
			}
			if _, ok, e := s.CompletedSelectionOperation(ctx, target); e == nil || ok {
				t.Fatal("bad state produced a notice", ok, e)
			}
			var after string
			if e = s.db.QueryRow(`SELECT journal_json FROM activation_journals WHERE op_id=?`, j.OpID).Scan(&after); e != nil {
				t.Fatal(e)
			}
			if before != after {
				t.Fatal("reader rewrote corrupt data")
			}
		})
	}
}
func TestCompletedSelectionRejectsInvalidObservation(t *testing.T) {
	s, e := Open(t.TempDir(), "test-install")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, ok, e := s.CompletedSelectionOperation(context.Background(), SelectedVersion{}); ok || !errors.Is(e, ErrInvalidRecord) {
		t.Fatal(ok, e)
	}
}
