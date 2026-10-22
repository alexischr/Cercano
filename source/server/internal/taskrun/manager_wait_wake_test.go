package taskrun

import (
	"context"
	"errors"
	"testing"
	"time"

	"cercano/source/server/internal/conversation"
	"cercano/source/server/internal/dispatch"
)

// waitRegistered polls until the goroutine's Wait has registered its wake for
// parent, giving the regression a deterministic "wait is blocked" boundary.
func waitRegistered(t *testing.T, m *Manager, parent string) {
	waitRegisteredCount(t, m, parent, 1)
}

func waitRegisteredCount(t *testing.T, m *Manager, parent string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		n := len(m.waitWake[parent])
		m.mu.Unlock()
		if n >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("wait never registered")
}

// Core regression: a foreground Wait on task A must return promptly — with
// ErrTaskWaitInterrupted and A's nonterminal record — when another task in the
// SAME chat completes, so the main turn can consume the pending completion.
// Pre-fix, Wait ignored same-chat completions and blocked until its deadline.
func TestWaitReturnsWhenSameChatSiblingCompletes(t *testing.T) {
	releaseA := make(chan struct{})
	releaseB := make(chan struct{})
	started := make(chan string, 2)
	m, _ := newManager(t, func(ctx context.Context, s dispatch.Spec) (dispatch.Result, error) {
		started <- s.Task
		ch := releaseA
		if s.Task == "b" {
			ch = releaseB
		}
		select {
		case <-ch:
			return dispatch.Result{Text: "done " + s.Task}, nil
		case <-ctx.Done():
			return dispatch.Result{}, ctx.Err()
		}
	})
	specA := taskSpec()
	specA.Task = "a"
	specB := taskSpec()
	specB.Task = "b"
	a, err := m.Launch(deadline(t), specA)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Launch(deadline(t), specB)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	<-started
	type outcome struct {
		record conversation.TaskRecord
		err    error
	}
	waited := make(chan outcome, 1)
	go func() {
		record, err := m.Wait(deadline(t), "parent", a.ID)
		waited <- outcome{record, err}
	}()
	waitRegistered(t, m, "parent")
	close(releaseB) // b finishes while the foreground turn waits on a.
	select {
	case got := <-waited:
		if !errors.Is(got.err, conversation.ErrTaskWaitInterrupted) {
			t.Fatalf("wait returned %v, want ErrTaskWaitInterrupted", got.err)
		}
		if got.record.ID != a.ID {
			t.Fatalf("returned record for %q, want waited task %q", got.record.ID, a.ID)
		}
		if got.record.Terminal() {
			t.Fatalf("waited task became terminal: %+v", got.record)
		}
		if got.record.Status != "running" {
			t.Fatalf("waited task status = %q, want running", got.record.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wait on a did not return within 2s of sibling b completing")
	}
	// The wake must not touch the waited task: it keeps running.
	final, err := m.Inspect(deadline(t), "parent", a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != "running" {
		t.Fatalf("waited task status = %q, want running", final.Status)
	}
	if b.Status == "" || b.ID == "" {
		t.Fatal("launch receipt lost")
	}
	// Registration must be cleaned up after each wait returns.
	m.mu.Lock()
	remaining := len(m.waitWake["parent"])
	m.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("wait registrations leaked: %d", remaining)
	}
	// The waited task still runs to completion on its own.
	close(releaseA)
	done, err := m.Wait(deadline(t), "parent", a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != "completed" {
		t.Fatalf("waited task final status = %q, want completed", done.Status)
	}
}

// Completions in OTHER chats must not wake a wait: chat isolation is preserved.
func TestWaitIgnoresOtherChatCompletion(t *testing.T) {
	releaseA := make(chan struct{})
	releaseOther := make(chan struct{})
	started := make(chan string, 2)
	m, _ := newManager(t, func(ctx context.Context, s dispatch.Spec) (dispatch.Result, error) {
		started <- s.ConversationID
		ch := releaseA
		if s.ConversationID == "other" {
			ch = releaseOther
		}
		select {
		case <-ch:
			return dispatch.Result{Text: "done"}, nil
		case <-ctx.Done():
			return dispatch.Result{}, ctx.Err()
		}
	})
	specA := taskSpec()
	specOther := taskSpec()
	specOther.ConversationID = "other"
	a, err := m.Launch(deadline(t), specA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Launch(deadline(t), specOther); err != nil {
		t.Fatal(err)
	}
	<-started
	<-started
	waited := make(chan conversation.TaskRecord, 1)
	go func() {
		record, err := m.Wait(deadline(t), "parent", a.ID)
		if err != nil {
			t.Errorf("wait error: %v", err)
		}
		waited <- record
	}()
	waitRegistered(t, m, "parent")
	close(releaseOther)
	select {
	case got := <-waited:
		t.Fatalf("wait returned on other-chat completion: %+v", got)
	case <-time.After(150 * time.Millisecond):
	}
	close(releaseA)
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatal("wait did not return when its own task completed")
	}
}

// A completion that predates the wait registration must not replay into the
// new wait: the wake is edge-triggered, so an immediately-completed sibling
// leaves Wait to block normally until its own task or deadline.
func TestStaleCompletionDoesNotReplayIntoNewWait(t *testing.T) {
	releaseA := make(chan struct{})
	started := make(chan string, 2)
	m, _ := newManager(t, func(ctx context.Context, s dispatch.Spec) (dispatch.Result, error) {
		started <- s.Task
		if s.Task == "b" {
			return dispatch.Result{Text: "done b"}, nil
		}
		select {
		case <-releaseA:
			return dispatch.Result{Text: "done " + s.Task}, nil
		case <-ctx.Done():
			return dispatch.Result{}, ctx.Err()
		}
	})
	specA := taskSpec()
	specA.Task = "a"
	specB := taskSpec()
	specB.Task = "b"
	a, err := m.Launch(deadline(t), specA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Launch(deadline(t), specB); err != nil {
		t.Fatal(err)
	}
	<-started // a started
	// Complete b BEFORE anyone waits.
	deadlineB := time.Now().Add(2 * time.Second)
	for {
		tasks, err := m.List(deadline(t), "parent", 10)
		if err != nil {
			t.Fatal(err)
		}
		done := false
		for _, rec := range tasks {
			if rec.Task == "b" && rec.Terminal() {
				done = true
			}
		}
		if done {
			break
		}
		if time.Now().After(deadlineB) {
			t.Fatal("b never completed")
		}
		time.Sleep(time.Millisecond)
	}
	// A fresh wait on a must block: no stale wake, no busy loop. Bound it by
	// a short deadline instead of sleeping.
	ctx, cancel := context.WithTimeout(deadline(t), 150*time.Millisecond)
	defer cancel()
	_, err = m.Wait(ctx, "parent", a.ID)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait returned %v, want DeadlineExceeded from its own bound", err)
	}
	// a survives untouched.
	rec, err := m.Inspect(deadline(t), "parent", a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != "running" {
		t.Fatalf("waited task status = %q, want running", rec.Status)
	}
	close(releaseA)
}

// The waited task's own completion must win over any wake: waiters registered
// while the task finishes get the real terminal record, not the sentinel.
func TestOwnCompletionWinsOverWake(t *testing.T) {
	release := make(chan struct{})
	m, _ := newManager(t, func(ctx context.Context, s dispatch.Spec) (dispatch.Result, error) {
		<-release
		return dispatch.Result{Text: "done"}, nil
	})
	a, err := m.Launch(deadline(t), taskSpec())
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		record conversation.TaskRecord
		err    error
	}
	waited := make(chan outcome, 2)
	for i := 0; i < 2; i++ {
		go func() {
			record, err := m.Wait(deadline(t), "parent", a.ID)
			waited <- outcome{record, err}
		}()
	}
	waitRegistered(t, m, "parent")
	close(release)
	for i := 0; i < 2; i++ {
		select {
		case got := <-waited:
			if got.err != nil {
				t.Fatalf("own completion wait errored: %v", got.err)
			}
			if got.record.Status != "completed" {
				t.Fatalf("status = %q, want completed", got.record.Status)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("waiter did not return on own task completion")
		}
	}
}

// Multiple waiters across tasks in the same chat all wake when one task
// completes there, and each gets an accurate per-task record.
func TestMultipleWaitersWakeOnSiblingCompletion(t *testing.T) {
	releaseA := make(chan struct{})
	releaseB := make(chan struct{})
	started := make(chan string, 2)
	m, _ := newManager(t, func(ctx context.Context, s dispatch.Spec) (dispatch.Result, error) {
		started <- s.Task
		ch := releaseA
		if s.Task == "b" {
			ch = releaseB
		}
		select {
		case <-ch:
			return dispatch.Result{Text: "done " + s.Task}, nil
		case <-ctx.Done():
			return dispatch.Result{}, ctx.Err()
		}
	})
	specA := taskSpec()
	specA.Task = "a"
	specB := taskSpec()
	specB.Task = "b"
	specC := taskSpec()
	specC.Task = "c"
	a, err := m.Launch(deadline(t), specA)
	if err != nil {
		t.Fatal(err)
	}
	c, err := m.Launch(deadline(t), specC)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Launch(deadline(t), specB); err != nil {
		t.Fatal(err)
	}
	<-started
	<-started
	<-started
	type outcome struct {
		id     string
		record conversation.TaskRecord
		err    error
	}
	waited := make(chan outcome, 2)
	for _, id := range []string{a.ID, c.ID} {
		go func(id string) {
			record, err := m.Wait(deadline(t), "parent", id)
			waited <- outcome{id: id, record: record, err: err}
		}(id)
	}
	waitRegisteredCount(t, m, "parent", 2)
	close(releaseB)
	for i := 0; i < 2; i++ {
		select {
		case got := <-waited:
			if !errors.Is(got.err, conversation.ErrTaskWaitInterrupted) {
				t.Fatalf("waiter %s returned %v, want ErrTaskWaitInterrupted", got.id, got.err)
			}
			if got.record.ID != got.id || got.record.Terminal() {
				t.Fatalf("waiter %s got inaccurate record %+v", got.id, got.record)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("a waiter did not wake on sibling completion")
		}
	}
}

// Caller cancellation and wait deadlines keep their existing contracts: they
// stop the wait, never the task.
func TestWaitDeadlineAndCancelDoNotWakeIntoChildStop(t *testing.T) {
	release := make(chan struct{})
	m, _ := newManager(t, func(ctx context.Context, s dispatch.Spec) (dispatch.Result, error) {
		select {
		case <-release:
			return dispatch.Result{Text: "done"}, nil
		case <-ctx.Done():
			return dispatch.Result{}, ctx.Err()
		}
	})
	a, err := m.Launch(deadline(t), taskSpec())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(deadline(t))
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err = m.Wait(ctx, "parent", a.ID)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("wait returned %v, want context.Canceled", err)
	}
	rec, err := m.Inspect(deadline(t), "parent", a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != "running" {
		t.Fatalf("canceled wait stopped the task: %q", rec.Status)
	}
	close(release)
}
