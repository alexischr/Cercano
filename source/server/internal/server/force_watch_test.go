package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"cercano/source/server/internal/updatecoord/enrollment"
	"cercano/source/server/internal/updatecoord/privdir"
	"cercano/source/server/internal/updatecoord/state"
)

func TestForceLocalObserverUsesProtectedRequestWithoutNetworkCommand(t *testing.T) {
	parent, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(parent, "installed")
	if _, e = privdir.Ensure(root); e != nil {
		t.Fatal(e)
	}
	agent, client := "cercano", "cercano-cli"
	if runtime.GOOS == "windows" {
		agent += ".exe"
		client += ".exe"
	}
	for _, name := range []string{agent, client} {
		if e = os.WriteFile(filepath.Join(root, name), []byte("inert"), 0700); e != nil {
			t.Fatal(e)
		}
	}
	record, e := enrollment.Create(context.Background(), root)
	if e != nil && !errors.Is(e, enrollment.ErrCommitted) {
		t.Fatal(e)
	}
	stateRoot, store, opID := forceLocalFixture(t, record.InstallID)
	s := &Server{events: newEventHub()}
	var stops atomic.Int32
	s.SetProcessForceStopRequester(func(string) { stops.Add(1) })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.watchEnrolledTUIUpdates(ctx, filepath.Join(root, agent), stateRoot, 10*time.Millisecond, nil)
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("observer leaked")
		}
	}()
	deadline := time.Now().Add(3 * time.Second)
	var session state.ForceAgent
	for {
		a, found, e := store.ReadForceAgent(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if found {
			session = a
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("agent presence missing")
		}
		time.Sleep(time.Millisecond)
	}
	if stops.Load() != 0 {
		t.Fatal("registration interrupted agent")
	}
	now := time.Now()
	intent := state.ForceIntent{OpID: opID, Agent: session, IssuedMS: now.UnixMilli(), ExpiresMS: now.Add(20 * time.Second).UnixMilli()}
	if e = store.SubmitForceIntent(ctx, intent, true, time.Now); e != nil {
		t.Fatal(e)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("authorized force request not consumed")
	}
	if stops.Load() != 1 {
		t.Fatal("wrong stop count", stops.Load())
	}
	got, found, consumed, e := store.ReadForceIntent(context.Background(), opID)
	if e != nil || !found || !consumed || got != intent {
		t.Fatal("stop was not preceded by durable consumption", got, found, consumed, e)
	}
	// Only a callback counter is invoked. No real process is signaled or exited.
}
