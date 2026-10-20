// Package taskrun owns independent delegated executions for one host lifetime.
// It deliberately does not own routing or grants: the injected executor must
// enforce the same policy as synchronous dispatch.
package taskrun

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"cercano/source/server/internal/conversation"
	"cercano/source/server/internal/dispatch"
)

type Executor func(context.Context, dispatch.Spec) (dispatch.Result, error)
type execution struct {
	cancel     context.CancelFunc
	done       chan struct{}
	stop       bool
	persistErr error
}
type Manager struct {
	mu      sync.Mutex
	store   conversation.TaskStore
	execute Executor
	ctx     context.Context
	cancel  context.CancelFunc
	active  map[string]*execution
	closed  bool
	wg      sync.WaitGroup
	// Notify is nonblocking host scheduling, not a turn-scoped progress callback.
	notify func(string)
}

// New reconciles prior unfinished executions exactly once per host lifetime.
// A manager must not be constructed per tool call or per client attachment.
func New(store conversation.TaskStore, execute Executor, notify func(string)) (*Manager, error) {
	if store == nil || execute == nil {
		return nil, errors.New("task persistence and executor are required")
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := store.InitTasks(ctx); err != nil {
		cancel()
		return nil, err
	}
	if err := store.ReconcileTasks(ctx); err != nil {
		cancel()
		return nil, err
	}
	return &Manager{store: store, execute: execute, ctx: ctx, cancel: cancel, active: make(map[string]*execution), notify: notify}, nil
}

func (m *Manager) Launch(ctx context.Context, spec dispatch.Spec) (conversation.TaskRecord, error) {
	if spec.ConversationID == "" || spec.Task == "" {
		return conversation.TaskRecord{}, errors.New("task and parent conversation are required")
	}
	if err := ctx.Err(); err != nil {
		return conversation.TaskRecord{}, err
	}
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return conversation.TaskRecord{}, err
	}
	now := time.Now()
	t := conversation.TaskRecord{ID: "task_" + hex.EncodeToString(entropy[:]), ParentID: spec.ConversationID, Status: "accepted", ChildID: conversation.NewID(), Task: spec.Task, WorkDir: spec.WorkDir, CreatedAt: now, UpdatedAt: now}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return conversation.TaskRecord{}, errors.New("task host is shutting down")
	}
	// Acknowledge only durable acceptance. A canceled caller cannot leave a
	// fictitious task handle if persistence failed.
	if err := m.store.InsertTask(ctx, t); err != nil {
		return conversation.TaskRecord{}, err
	}
	// Preserve policy/context values, not the launching turn's cancellation or
	// deadline. The manager provides the only execution lifetime here.
	child, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stopHost := context.AfterFunc(m.ctx, cancel)
	e := &execution{cancel: cancel, done: make(chan struct{})}
	m.active[t.ID] = e
	spec.Tools = append([]string(nil), spec.Tools...)
	spec.SubConversationID = t.ChildID
	spec.Emit = nil // main-turn callbacks must never escape into independent work
	m.wg.Add(1)
	go func() { defer m.wg.Done(); defer stopHost(); defer cancel(); m.run(child, t, spec, e) }()
	return t, nil
}
func (m *Manager) run(ctx context.Context, t conversation.TaskRecord, spec dispatch.Spec, e *execution) {
	var result dispatch.Result
	var runErr error
	func() {
		defer func() {
			if p := recover(); p != nil {
				runErr = fmt.Errorf("task executor panic: %v", p)
			}
		}()
		if err := m.store.StartTask(ctx, t.ParentID, t.ID); err != nil {
			runErr = err
			return
		}
		result, runErr = m.execute(ctx, spec)
	}()
	t.Status = "completed"
	if runErr != nil {
		t.Status = "failed"
		t.Error = runErr.Error()
	}
	if result.SubConversationID != "" {
		t.ChildID = result.SubConversationID
	}
	t.ResultJSON, _ = json.Marshal(result) // Result contains only JSON-safe fields.
	m.mu.Lock()
	// Serialize explicit stop versus completion; the first terminal write wins.
	if e.stop {
		t.Status = "stopped"
	} else if m.closed {
		t.Status = "interrupted"
	}
	if t.Status == "interrupted" && t.Error == "" {
		t.Error = "Agent stopped; execution was not replayed. Inspect partial work before retrying."
	}
	persistCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	_, e.persistErr = m.store.FinishTask(persistCtx, t)
	cancel()
	close(e.done)
	// Keep failed persistence visible to Wait rather than pretending completion
	// was durably saved. Successful entries need no retained goroutine state.
	if e.persistErr == nil {
		delete(m.active, t.ID)
	}
	notify := m.notify
	m.mu.Unlock()
	if notify != nil && e.persistErr == nil {
		notify(t.ParentID)
	}
}
func (m *Manager) Inspect(ctx context.Context, parent, id string) (conversation.TaskRecord, error) {
	t, err := m.store.GetTask(ctx, parent, id)
	if err != nil {
		return t, err
	}
	if store, ok := m.store.(conversation.TaskEvidenceStore); ok {
		t.Evidence, err = store.TaskEvidence(ctx, parent, id)
	}
	return t, err
}
func (m *Manager) List(ctx context.Context, parent string, limit int) ([]conversation.TaskRecord, error) {
	return m.store.ListTasks(ctx, parent, limit)
}
func (m *Manager) Stop(ctx context.Context, parent, id string) (conversation.TaskRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.store.GetTask(ctx, parent, id)
	if err != nil {
		return t, err
	}
	if e := m.active[id]; e != nil && !t.Terminal() {
		e.stop = true
		e.cancel()
	}
	return t, nil
}

// Wait observes; canceling its context never cancels the task itself.
func (m *Manager) Wait(ctx context.Context, parent, id string) (conversation.TaskRecord, error) {
	m.mu.Lock()
	t, err := m.store.GetTask(ctx, parent, id)
	if err != nil {
		m.mu.Unlock()
		return t, err
	}
	e := m.active[id]
	m.mu.Unlock()
	if e != nil {
		select {
		case <-ctx.Done():
			return t, ctx.Err()
		case <-e.done:
		}
		if e.persistErr != nil {
			return t, fmt.Errorf("persist task outcome: %w", e.persistErr)
		}
	}
	return m.store.GetTask(ctx, parent, id)
}

// Close stops host-owned execution; it does not replay or wait without bounds.
// The caller must keep persistence open until this returns successfully.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}
