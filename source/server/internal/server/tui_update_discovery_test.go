package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"cercano/source/server/internal/updatecoord/activation"
	"cercano/source/server/internal/updatecoord/enrollment"
	"cercano/source/server/internal/updatecoord/operation"
	"cercano/source/server/internal/updatecoord/privdir"
	"cercano/source/server/internal/updatecoord/selection"
	"cercano/source/server/internal/updatecoord/state"
)

func TestTUIUpdateDiscoveryAndObservationOfCompletedInstall(t *testing.T) {
	parent, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(parent, "installed")
	if _, e = privdir.Ensure(root); e != nil {
		t.Fatal(e)
	}
	a, c := "cercano", "cercano-cli"
	if runtime.GOOS == "windows" {
		a += ".exe"
		c += ".exe"
	}
	for _, n := range []string{a, c} {
		if e = os.WriteFile(filepath.Join(root, n), []byte("inert"), 0700); e != nil {
			t.Fatal(e)
		}
	}
	id, e := enrollment.Create(context.Background(), root)
	if e != nil && !errors.Is(e, enrollment.ErrCommitted) {
		t.Fatal(e)
	}
	stateRoot := t.TempDir()
	srv := &Server{events: newEventHub()}
	launcher := filepath.Join(root, a)
	// Missing state is optional/unavailable, never initialized by discovery.
	if ok, e := srv.SyncEnrolledTUIUpdate(context.Background(), launcher, stateRoot); ok {
		t.Fatal("missing database announced", e)
	}

	entries, e := os.ReadDir(stateRoot)
	if e != nil || len(entries) != 0 {
		t.Fatal("discovery initialized state", entries, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.watchEnrolledTUIUpdates(ctx, launcher, stateRoot, 10*time.Millisecond, nil)
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("observer did not stop")
		}
	}()
	org := "Cercano"
	if runtime.GOOS == "linux" {
		org = "cercano"
	}
	dir := stateRoot
	for _, part := range []string{org, "updater", id.InstallID} {
		dir = filepath.Join(dir, part)
		if _, e = privdir.Ensure(dir); e != nil {
			t.Fatal(e)
		}
	}
	store, e := state.Open(stateRoot, id.InstallID)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	op, e := state.NewAdapter(store).Start(context.Background(), "1.2.3")
	if e != nil {
		t.Fatal(e)
	}
	digest := strings.Repeat("a", 64)
	j := state.ActivationJournal{SchemaVersion: state.ActivationJournalSchemaVersion, InstallID: id.InstallID, OpID: op.ID, TargetVersion: "1.2.3", StagedVersionDir: "version-123", VerifiedArtifactSHA256: digest, Checkpoint: state.JournalPrepared}
	rev, e := store.SaveActivationJournal(context.Background(), 0, j)
	if e != nil {
		t.Fatal(e)
	}
	for _, cp := range []state.JournalCheckpoint{state.JournalSwitchIntent, state.JournalSelected, state.JournalHealthVerified, state.JournalComplete} {
		j.Checkpoint = cp
		rev, e = store.SaveActivationJournal(context.Background(), rev, j)
		if e != nil {
			t.Fatal(e)
		}
	}
	rec, r, e := store.LoadOperationRecord(context.Background(), op.ID)
	if e != nil {
		t.Fatal(e)
	}
	rec.State = operation.StateComplete
	rec.HealthVerified = true
	if _, e = store.SaveOperationRecord(context.Background(), r, rec); e != nil {
		t.Fatal(e)
	}
	sel := activation.Selection{SchemaVersion: 1, InstallID: id.InstallID, Generation: 1, SelectedVersion: "1.2.3", StagedVersionDir: "version-123", VerifiedArtifactSHA256: digest}
	data, _ := json.Marshal(sel)
	if e = os.WriteFile(filepath.Join(store.Directory(), selection.FileName), data, 0600); e != nil {
		t.Fatal(e)
	}
	if e = store.Close(); e != nil {
		t.Fatal(e)
	} // Checkpoint header for passive startup reader.
	deadline := time.Now().Add(3 * time.Second)
	for {
		srv.events.mu.Lock()
		notice := srv.events.latestTUI
		srv.events.mu.Unlock()
		if notice != nil {
			if notice.GetTuiUpdateInstalled().GetOperationId() != op.ID {
				t.Fatal(notice)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("completed installation was not observed")
		}
		time.Sleep(time.Millisecond)
	}
	replacement := &Server{events: newEventHub()}
	if ok, e := replacement.SyncEnrolledTUIUpdate(context.Background(), launcher, stateRoot); !ok || e != nil {
		t.Fatal("replacement startup snapshot", ok, e)
	}
}
func TestTUIUpdateObserverDoesNotEnrollOrPollUnknownLauncher(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	dir := t.TempDir()
	s := &Server{events: newEventHub()}
	s.watchEnrolledTUIUpdates(ctx, filepath.Join(dir, "unknown"), dir, time.Millisecond, nil)
	files, e := os.ReadDir(dir)
	if e != nil || len(files) != 0 {
		t.Fatal("observer created state", files, e)
	}
}
