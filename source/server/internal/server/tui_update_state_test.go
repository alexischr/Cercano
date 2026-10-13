package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"cercano/source/server/internal/updatecoord/activation"
	"cercano/source/server/internal/updatecoord/operation"
	"cercano/source/server/internal/updatecoord/privdir"
	"cercano/source/server/internal/updatecoord/selection"
	"cercano/source/server/internal/updatecoord/state"
)

func installedNoticeFixture(t *testing.T, ids ...string) (string, *state.Store, activation.Selection, int64) {
	t.Helper()
	installID := "notice-install"
	if len(ids) > 0 {
		installID = ids[0]
	}
	root := t.TempDir()
	org := "Cercano"
	if runtime.GOOS == "linux" {
		org = "cercano"
	}
	dir := root
	for _, part := range []string{org, "updater", installID} {
		dir = filepath.Join(dir, part)
		if _, e := privdir.Ensure(dir); e != nil {
			t.Fatal(e)
		}
	}
	store, e := state.Open(root, installID)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	op, e := state.NewAdapter(store).Start(ctx, "1.2.3")
	if e != nil {
		t.Fatal(e)
	}
	digest := strings.Repeat("a", 64)
	journal := state.ActivationJournal{SchemaVersion: state.ActivationJournalSchemaVersion, InstallID: store.InstallID(), OpID: op.ID, TargetVersion: "1.2.3", StagedVersionDir: "version-123", VerifiedArtifactSHA256: digest, Checkpoint: state.JournalPrepared}
	rev, e := store.SaveActivationJournal(ctx, 0, journal)
	if e != nil {
		t.Fatal(e)
	}
	for _, cp := range []state.JournalCheckpoint{state.JournalSwitchIntent, state.JournalSelected, state.JournalHealthVerified, state.JournalComplete} {
		journal.Checkpoint = cp
		rev, e = store.SaveActivationJournal(ctx, rev, journal)
		if e != nil {
			t.Fatal(e)
		}
	}
	rec, r, e := store.LoadOperationRecord(ctx, op.ID)
	if e != nil {
		t.Fatal(e)
	}
	rec.State = operation.StateComplete
	rec.HealthVerified = true
	if _, e = store.SaveOperationRecord(ctx, r, rec); e != nil {
		t.Fatal(e)
	}
	sel := activation.Selection{SchemaVersion: 1, InstallID: store.InstallID(), Generation: 1, SelectedVersion: "1.2.3", StagedVersionDir: "version-123", VerifiedArtifactSHA256: digest}
	data, e := json.Marshal(sel)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, selection.FileName), data, 0600); e != nil {
		t.Fatal(e)
	}
	return root, store, sel, op.ID
}
func TestTUIUpdateRebuiltFromCompletedSelectedState(t *testing.T) {
	root, store, sel, id := installedNoticeFixture(t)
	ctx := context.Background()
	first := &Server{events: newEventHub()}
	if ok, e := first.SyncTUIUpdateFromState(ctx, store); e != nil || !ok {
		t.Fatal(ok, e)
	}
	notice := first.events.latestTUI.GetTuiUpdateInstalled()
	if notice.GetOperationId() != id || notice.GetVersion() != sel.SelectedVersion {
		t.Fatal(notice)
	}
	before, e := os.ReadFile(filepath.Join(store.Directory(), selection.FileName))
	if e != nil {
		t.Fatal(e)
	}
	if e = store.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := state.Open(root, "notice-install")
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	replacement := &Server{events: newEventHub()}
	if ok, e := replacement.SyncTUIUpdateFromState(ctx, reopened); e != nil || !ok {
		t.Fatal("replacement snapshot", ok, e)
	}
	if replacement.events.latestTUI.GetTuiUpdateInstalled().GetOperationId() != id {
		t.Fatal("lost completed operation")
	}
	after, e := os.ReadFile(filepath.Join(reopened.Directory(), selection.FileName))
	if e != nil || string(after) != string(before) {
		t.Fatal("source rewrote selection", e)
	}
}
func TestTUIUpdateStateRefusesUnrelatedOrAbsentSelection(t *testing.T) {
	_, store, sel, _ := installedNoticeFixture(t)
	ctx := context.Background()
	path := filepath.Join(store.Directory(), selection.FileName)
	for _, kind := range []string{"foreign", "wrong-target", "corrupt", "absent"} {
		t.Run(kind, func(t *testing.T) {
			s := &Server{events: newEventHub()}
			changed := sel
			switch kind {
			case "foreign":
				changed.InstallID = "foreign"
			case "wrong-target":
				changed.SelectedVersion = "other"
			}
			data, _ := json.Marshal(changed)
			if kind == "corrupt" {
				data = []byte("{")
			}
			if kind == "absent" {
				if e := os.Remove(path); e != nil {
					t.Fatal(e)
				}
			} else {
				if e := os.WriteFile(path, data, 0600); e != nil {
					t.Fatal(e)
				}
			}
			ok, e := s.SyncTUIUpdateFromState(ctx, store)
			if ok || s.events.latestTUI != nil {
				t.Fatal("unproven selection announced", ok, e)
			}
			if (kind == "foreign" || kind == "corrupt") && e == nil {
				t.Fatal("unsafe source not diagnosed")
			}
		})
	}
}
