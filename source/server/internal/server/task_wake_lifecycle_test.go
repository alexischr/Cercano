package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"cercano/source/server/internal/dispatch"
	"cercano/source/server/internal/runner"
)

func TestTaskWakeInitializedOnceWithProductionManager(t *testing.T) {
	s, _ := newServerWithStore(t)
	s.SetDispatchEngine(&dispatch.Engine{})
	t.Cleanup(s.closeTaskManager)
	if s.taskRunManager() == nil {
		t.Fatal("manager not initialized")
	}
	if s.taskWakeCtx == nil || s.taskWakeCancel == nil {
		t.Fatal("production initialization omitted the completion wake context")
	}
	wake := s.taskWakeCtx
	if !s.ensureTaskManager() || s.taskWakeCtx != wake {
		t.Fatal("repeated initialization replaced the wake context")
	}
	s.closeTaskManager()
	if !errors.Is(wake.Err(), context.Canceled) {
		t.Fatal("shutdown did not cancel the wake context")
	}
	if s.ensureTaskManager() {
		t.Fatal("manager restarted after shutdown")
	}
}

func TestTaskWakeProductionCallbackReachesIdleParent(t *testing.T) {
	s, _ := newServerWithStore(t)
	// Use the real initialization path, not a manually installed task manager or
	// wake context. The durable completion stands in for a finished executor.
	s.SetDispatchEngine(&dispatch.Engine{})
	t.Cleanup(s.closeTaskManager)
	store := seedCompletion(t, s, "idle")
	woken := make(chan runner.Request, 1)
	s.inProcessRunner = completionProbeRunner{run: func(_ context.Context, req runner.Request, _ runner.PermissionRequester) (runner.Result, error) {
		woken <- req
		return runner.Result{FinalText: "reported completed work", Model: "fixture"}, nil
	}}
	s.notifyTaskChange("owner")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	select {
	case req := <-woken:
		if req.ConversationID != "owner" {
			t.Fatalf("wrong parent: %s", req.ConversationID)
		}
	case <-ctx.Done():
		t.Fatal("completion callback never woke the idle parent")
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		pending, err := store.PendingTasks(ctx, "owner")
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 0 {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("completion was not acknowledged")
		}
	}
}

func TestTaskWakeShutdownCancelsAndDrainsActiveTurn(t *testing.T) {
	s, _ := newServerWithStore(t)
	s.SetDispatchEngine(&dispatch.Engine{})
	t.Cleanup(s.closeTaskManager)
	seedCompletion(t, s, "shutdown")
	started := make(chan struct{})
	finished := make(chan struct{})
	s.inProcessRunner = completionProbeRunner{run: func(ctx context.Context, _ runner.Request, _ runner.PermissionRequester) (runner.Result, error) {
		close(started)
		<-ctx.Done()
		close(finished)
		return runner.Result{}, ctx.Err()
	}}
	s.notifyTaskChange("owner")
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("background turn never started")
	}
	closed := make(chan struct{})
	go func() { s.closeTaskManager(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not drain the background turn")
	}
	select {
	case <-finished:
	default:
		t.Fatal("shutdown returned with a background turn still active")
	}
	if s.turnBroker.HasActiveTurn("owner") {
		t.Fatal("shutdown left a background turn registered")
	}
}
