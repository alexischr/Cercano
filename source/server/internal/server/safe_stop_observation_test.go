package server

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"
)

// A committed response is not a join on the original handler's callback.
func TestSafeStopCommittedRepeatDoesNotJoinCallback(t *testing.T) {
	entered, proceed, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(proceed) }) }
	defer unblock()
	f := startSafeStopServer(t, func(s *Server, stop *fakeStopRecorder) {
		s.SetProcessStopRequester(func(reason string) { close(entered); <-proceed; stop.stop(reason); close(finished) })
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := f.client.ShutdownAgentWhenIdle(ctx, safeStopReq(int64(os.Getpid()))); done <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	resp, err := f.client.ShutdownAgentWhenIdle(ctx, safeStopReq(int64(os.Getpid())))
	if err != nil || !resp.GetAccepted() {
		t.Fatalf("repeat: %v %v", resp, err)
	}
	if got := f.stop.invocations(); got != 0 {
		t.Fatalf("blocked callback unexpectedly recorded %d", got)
	}
	t.Log("accepted repeat observed while callback count remains zero")
	unblock()
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if got := f.stop.invocations(); got != 1 {
		t.Fatalf("callback count=%d", got)
	}
}
