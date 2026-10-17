package server

import (
	"cercano/source/server/internal/updatecoord/enrollment"
	"cercano/source/server/internal/updatecoord/privdir"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestTUIUpdateBoundObserverRejectsChangedStartupIdentity(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "installed")
	if _, err = privdir.Ensure(root); err != nil {
		t.Fatal(err)
	}
	agent, client := "cercano", "cercano-cli"
	if runtime.GOOS == "windows" {
		agent += ".exe"
		client += ".exe"
	}
	for _, name := range []string{agent, client} {
		if err = os.WriteFile(filepath.Join(root, name), []byte("inert"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	current, err := enrollment.Create(context.Background(), root)
	if err != nil && !errors.Is(err, enrollment.ErrCommitted) {
		t.Fatal(err)
	}
	expected := current
	expected.InstallID = strings.Repeat("0", 32)
	if expected.InstallID == current.InstallID {
		expected.InstallID = strings.Repeat("1", 32)
	}
	s := &Server{events: newEventHub()}
	s.SetProcessForceStopRequester(func(string) { t.Error("unexpected stop") })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	stateRoot := t.TempDir()
	s.watchBoundEnrolledTUIUpdates(ctx, filepath.Join(root, agent), stateRoot, &expected, time.Millisecond, nil)
	if s.localForceControl != nil {
		t.Fatal("changed startup enrollment was adopted before force-control initialization")
	}
	entries, err := os.ReadDir(stateRoot)
	if err != nil || len(entries) != 0 {
		t.Fatal("observer created state", entries, err)
	}
}

func TestTUIUpdateBindingRevalidatedBeforePublication(t *testing.T) {
	_, store, _, _ := installedNoticeFixture(t)
	s := &Server{events: newEventHub()}
	changed := errors.New("enrollment changed")
	calls := 0
	ok, err := s.syncTUIUpdateFromState(context.Background(), store, func() error { calls++; return changed })
	if ok || !errors.Is(err, changed) || calls != 1 {
		t.Fatal(ok, err, calls)
	}
	if s.events.latestTUI != nil || s.events.tuiInstallID != "" {
		t.Fatal("refused binding published or bound event hub")
	}
	ok, err = s.syncTUIUpdateFromState(context.Background(), store, func() error { return nil })
	if !ok || err != nil {
		t.Fatal("unchanged binding refused", ok, err)
	}
}
func TestTUIUpdateBoundSyncRejectsMissingBinding(t *testing.T) {
	s := &Server{events: newEventHub()}
	if ok, err := s.SyncBoundEnrolledTUIUpdate(context.Background(), "", t.TempDir(), nil); ok || err == nil {
		t.Fatal(ok, err)
	}
}
