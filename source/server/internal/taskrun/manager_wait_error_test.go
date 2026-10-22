package taskrun

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"cercano/source/server/internal/conversation"
	"cercano/source/server/internal/dispatch"
)

type wakeErrorStore struct {
	conversation.TaskStore
	readErr   atomic.Bool
	finishErr error
}

var errWakeRead = errors.New("wake reread failed")

func (s *wakeErrorStore) GetTask(ctx context.Context, parent, id string) (conversation.TaskRecord, error) {
	if s.readErr.Load() {
		return conversation.TaskRecord{}, errWakeRead
	}
	return s.TaskStore.GetTask(ctx, parent, id)
}
func (s *wakeErrorStore) FinishTask(ctx context.Context, rec conversation.TaskRecord) (bool, error) {
	if s.finishErr != nil {
		return false, s.finishErr
	}
	return s.TaskStore.FinishTask(ctx, rec)
}
func TestWakeRereadFailureIsNotSuccess(t *testing.T) {
	m, store := newManager(t, func(ctx context.Context, _ dispatch.Spec) (dispatch.Result, error) {
		<-ctx.Done()
		return dispatch.Result{}, ctx.Err()
	})
	injected := &wakeErrorStore{TaskStore: store.(conversation.TaskStore)}
	m.store = injected
	a, err := m.Launch(deadline(t), taskSpec())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := m.Wait(deadline(t), "parent", a.ID); done <- err }()
	waitRegistered(t, m, "parent")
	injected.readErr.Store(true)
	m.mu.Lock()
	m.signalWaitWakeLocked("parent")
	m.mu.Unlock()
	if err := <-done; !errors.Is(err, errWakeRead) {
		t.Fatalf("reread error = %v, want %v", err, errWakeRead)
	}
}
func TestFailedPersistenceDoesNotWakeSibling(t *testing.T) {
	release := make(chan struct{})
	m, store := newManager(t, func(ctx context.Context, s dispatch.Spec) (dispatch.Result, error) {
		if s.Task == "failing" {
			select {
			case <-release:
				return dispatch.Result{}, nil
			case <-ctx.Done():
			}
		}
		<-ctx.Done()
		return dispatch.Result{}, ctx.Err()
	})
	failure := errors.New("disk write failed")
	m.store = &wakeErrorStore{TaskStore: store.(conversation.TaskStore), finishErr: failure}
	a, err := m.Launch(deadline(t), taskSpec())
	if err != nil {
		t.Fatal(err)
	}
	spec := taskSpec()
	spec.Task = "failing"
	b, err := m.Launch(deadline(t), spec)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(deadline(t), 150*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := m.Wait(ctx, "parent", a.ID); done <- err }()
	waitRegistered(t, m, "parent")
	close(release)
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("failed sibling write woke wait: %v", err)
	}
	if _, err := m.Wait(deadline(t), "parent", b.ID); !errors.Is(err, failure) {
		t.Fatalf("lost persistence error: %v", err)
	}
}
func TestOwnPersistenceFailureWinsOverWake(t *testing.T) {
	// Both channels are ready: the wake path must not hide the execution error.
	failure := errors.New("disk write failed")
	for i := 0; i < 40; i++ {
		m, store := newManager(t, func(ctx context.Context, _ dispatch.Spec) (dispatch.Result, error) {
			<-ctx.Done()
			return dispatch.Result{}, ctx.Err()
		})
		m.store = &wakeOnReadStore{TaskStore: m.store, m: m}
		a, err := m.Launch(deadline(t), taskSpec())
		if err != nil {
			t.Fatal(err)
		}
		// Install completed execution bookkeeping while the real executor remains blocked.
		m.mu.Lock()
		e := &execution{done: make(chan struct{}), persistErr: failure}
		close(e.done)
		m.active[a.ID] = e
		m.mu.Unlock()
		_ = store
		// A wake already queued at registration is injected through GetTask.
		if _, err := m.Wait(deadline(t), "parent", a.ID); !errors.Is(err, failure) {
			t.Fatalf("iteration %d lost persistence error: %v", i, err)
		}
	}
}

type wakeOnReadStore struct {
	conversation.TaskStore
	m     *Manager
	fired atomic.Bool
}

func (s *wakeOnReadStore) GetTask(ctx context.Context, parent, id string) (conversation.TaskRecord, error) {
	// First read occurs under the manager lock. Only signal once.
	if !s.fired.Swap(true) {
		s.m.signalWaitWakeLocked(parent)
	}
	return s.TaskStore.GetTask(ctx, parent, id)
}
