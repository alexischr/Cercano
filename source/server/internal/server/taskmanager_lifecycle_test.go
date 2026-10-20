package server

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"cercano/source/server/internal/conversation"
	"cercano/source/server/internal/dispatch"
	persistsvc "cercano/source/server/internal/hostsvc/persistence"
	toolssvc "cercano/source/server/internal/hostsvc/tools"
	"cercano/source/server/internal/taskrun"
)

// openServerTaskStore opens a real SQLite store in a temp dir and exposes its
// host-only TaskStore extension. The caller gets cleanup via t.Cleanup.
func openServerTaskStore(t *testing.T) conversation.TaskStore {
	t.Helper()
	st, err := conversation.Open(":memory:")
	if err != nil {
		t.Fatalf("open conversation store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ts, ok := st.(conversation.TaskStore)
	if !ok {
		t.Fatalf("store does not implement conversation.TaskStore")
	}
	return ts
}

// stubPersistSvc overrides only the store accessor the task-manager seam reads;
// all other Service methods panic if ever reached (they must not be).
type stubPersistSvc struct {
	persistsvc.Service
	store conversation.Store
}

func (p *stubPersistSvc) Store() conversation.Store { return p.store }

// stubToolCatalog overrides only the engine accessors; other Catalog methods
// panic if reached.
type stubToolCatalog struct {
	toolssvc.Catalog
	engine *dispatch.Engine
}

func (c *stubToolCatalog) Engine() *dispatch.Engine { return c.engine }
func (c *stubToolCatalog) SetEngine(e *dispatch.Engine) {
	c.engine = e
}

// TestServerTaskManager_InitializeOnceAndReconcile verifies that the first
// ensure constructs exactly one manager over the durable store, reconciles a
// leftover accepted task from a "previous process" to interrupted exactly once,
// and that a second ensure returns the same manager rather than building another.
func TestServerTaskManager_InitializeOnceAndReconcile(t *testing.T) {
	s, _ := newTestServer()
	ts := openServerTaskStore(t)
	s.persistSvc = &stubPersistSvc{store: ts.(conversation.Store)}
	// ensureTaskManager also requires a wired engine; a zero-value engine is
	// enough to construct the manager (no dispatch runs in this test).
	s.toolSvc = &stubToolCatalog{engine: &dispatch.Engine{}}

	// Seed durable leftover state as a prior process would have left it.
	ctx := context.Background()
	if err := ts.InitTasks(ctx); err != nil {
		t.Fatalf("InitTasks: %v", err)
	}
	leftover := conversation.TaskRecord{
		ID: "task-leftover", ParentID: "conv-1", Status: "accepted",
		Task: "orphaned work", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := ts.InsertTask(ctx, leftover); err != nil {
		t.Fatalf("InsertTask: %v", err)
	}

	s.SetDispatchEngine(&dispatch.Engine{})
	first := s.taskRunManager()
	if first == nil {
		t.Fatalf("manager nil after first ensure")
	}
	if !s.ensureTaskManager() {
		t.Fatalf("second ensureTaskManager failed")
	}
	if s.taskRunManager() != first {
		t.Fatalf("second ensure constructed a second manager")
	}

	// Reconciliation at construction: accepted → interrupted, no replay.
	rec, err := ts.GetTask(ctx, "conv-1", "task-leftover")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if rec.Status != "interrupted" {
		t.Fatalf("leftover status = %q, want interrupted", rec.Status)
	}
}

// TestServerTaskManager_ShutdownInterruptsAndRejectsLaunch verifies the
// shutdown contract: an in-flight execution is canceled and drained, the
// manager is closed, and launch is rejected after shutdown.
func TestServerTaskManager_ShutdownInterruptsAndRejectsLaunch(t *testing.T) {
	s, _ := newTestServer()
	ts := openServerTaskStore(t)
	s.persistSvc = &stubPersistSvc{store: ts.(conversation.Store)}

	// A blocking executor stands in for a real dispatch engine; it observes
	// host cancellation the same way the engine does. Built directly over
	// taskrun.New and installed as the server's manager so Shutdown's
	// closeTaskManager path is exercised.
	release := make(chan struct{})
	defer close(release) // unblock even on failure paths
	started := make(chan struct{})
	var executed atomic.Int32
	m, err := taskrun.New(ts, func(ctx context.Context, spec dispatch.Spec) (dispatch.Result, error) {
		executed.Add(1)
		close(started) // signal executor entry
		select {
		case <-ctx.Done():
			return dispatch.Result{}, ctx.Err()
		case <-release:
			return dispatch.Result{}, nil
		}
	}, nil)
	if err != nil {
		t.Fatalf("taskrun.New: %v", err)
	}
	s.taskRunMu.Lock()
	s.taskManager = m
	s.taskRunMu.Unlock()

	spec := dispatch.Spec{ConversationID: "conv-lc", Task: "long work"}
	if _, err := m.Launch(context.Background(), spec); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	// Wait for executor to start before shutdown to ensure cancellation is tested
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatalf("Executor did not start within timeout")
	}

	// Server shutdown must cancel the executor and wait for it to terminate.
	done := make(chan struct{})
	go func() { s.Shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("Shutdown blocked on task execution")
	}

	if s.taskRunManager() != nil {
		t.Fatalf("taskManager live after Shutdown")
	}
	if s.ensureTaskManager() {
		t.Fatalf("ensureTaskManager succeeded after shutdown")
	}
	if _, launchErr := m.Launch(context.Background(), spec); launchErr == nil {
		t.Fatalf("Launch after shutdown succeeded, want rejection")
	}

	// The interrupted task must be durably terminal: interrupted, not replayed.
	recs, err := ts.ListTasks(context.Background(), "conv-lc", 10)
	if err != nil || len(recs) != 1 {
		t.Fatalf("ListTasks: %v %d records", err, len(recs))
	}
	rec := recs[0]
	if rec.Status != "interrupted" {
		t.Fatalf("task status after shutdown = %q, want interrupted", rec.Status)
	}
}

// TestServerTaskManager_PersistUnavailableRejects verifies that when the
// durable store is unavailable the launch seam reports itself disabled rather
// than constructing a memory-only manager (durable acceptance precedes launch
// acknowledgement).
func TestServerTaskManager_PersistUnavailableRejects(t *testing.T) {
	s, _ := newTestServer()
	s.persistSvc = &stubPersistSvc{store: nil}
	if s.ensureTaskManager() {
		t.Fatalf("ensureTaskManager succeeded without a durable store")
	}
	if s.taskRunManager() != nil {
		t.Fatalf("manager constructed without a durable store")
	}
}
