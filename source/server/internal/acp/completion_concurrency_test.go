package acp

import (
	"context"
	"encoding/json"
	"io"
	"runtime"
	"strings"
	"testing"
)

// All workers become runnable before the single output pump can run.
func TestSimultaneousPromptCompletions(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	b := newFake()
	s := newServer(context.Background(), nil)
	defer s.cancel()
	s.backend, s.initialized = b, true
	go s.pump(io.Discard)
	for i := 0; i < 3; i++ {
		sid, _ := b.NewSession(s.ctx, "/workspace")
		s.sessions[sid] = &session{cwd: "/workspace"}
	}
	for sid := range b.events {
		p, _ := json.Marshal(map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": "hello"}}})
		s.handle(frame{ID: json.RawMessage(`1`), Method: "session/prompt", Params: p})
		recv(t, b.started)
	}
	for _, events := range b.events {
		events <- Event{Kind: "done"}
	}
	s.wg.Wait()
	if s.ctx.Err() != nil {
		t.Fatal("healthy output cancelled on simultaneous completions")
	}
}

func completionPrompt(sid string) frame {
	p, _ := json.Marshal(map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": "hello"}}})
	return frame{ID: json.RawMessage(`1`), Method: "session/prompt", Params: p}
}

func TestCompletionAdmissionLimitAndRelease(t *testing.T) {
	b := newFake()
	s := newServer(context.Background(), nil)
	defer s.cancel()
	s.backend, s.initialized = b, true
	var first string
	for i := 0; i < maxOutstandingPrompts; i++ {
		sid, _ := b.NewSession(s.ctx, "/workspace")
		if i == 0 {
			first = sid
		}
		s.sessions[sid] = &session{cwd: "/workspace"}
		s.handle(completionPrompt(sid))
		recv(t, b.started)
		recv(t, b.texts)
		b.events[sid] <- Event{Kind: "done"}
		s.wg.Wait()
		recv(t, b.stopped)
	}
	if len(s.completions) != maxOutstandingPrompts {
		t.Fatal("lost final responses")
	}
	extra, _ := b.NewSession(s.ctx, "/workspace")
	s.sessions[extra] = &session{cwd: "/workspace"}
	reject := func(sid, message string) {
		t.Helper()
		done := make(chan struct{})
		go func() { s.handle(completionPrompt(sid)); close(done) }()
		f := recv(t, s.output)
		r := f.value.(Response)
		if r.Error == nil || !strings.Contains(r.Error.Message, message) {
			t.Fatalf("unexpected rejection: %+v", r)
		}
		close(f.done)
		recv(t, done)
	}
	reject(extra, "outstanding prompt limit (64)")
	reject(first, "final response is pending")
	if s.ctx.Err() != nil {
		t.Fatal("admission failure dropped existing connection")
	}
	// Deliver one final response: only that session and one global slot release.
	f := recv(t, s.completions)
	if f.session != s.sessions[first] {
		t.Fatal("completion order changed")
	}
	if !s.deliver(json.NewEncoder(io.Discard), f) {
		t.Fatal("delivery failed")
	}
	s.handle(completionPrompt(first))
	recv(t, b.started)
	recv(t, b.texts)
	b.events[first] <- Event{Kind: "done"}
	s.wg.Wait()
	recv(t, b.stopped)
	if len(s.completions) != maxOutstandingPrompts {
		t.Fatal("released slot not reused")
	}
	// All workers exited with output entirely blocked and without dropping it.
	if s.ctx.Err() != nil {
		t.Fatal("completed workers cancelled connection")
	}
	for len(s.completions) > 0 {
		s.deliver(json.NewEncoder(io.Discard), <-s.completions)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.outstanding != 0 {
		t.Fatalf("leaked %d slots", s.outstanding)
	}
	for _, session := range s.sessions {
		if session.pending || session.cancel != nil {
			t.Fatal("session slot leaked")
		}
	}
}
