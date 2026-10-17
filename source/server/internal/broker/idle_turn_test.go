package broker

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"cercano/source/server/internal/runner"
)

func TestIdleClaimPreservesActiveTurnAndReplay(t *testing.T) {
	b := New()
	ctx, gen, release := b.BeginTurn(t.Context(), "chat")
	defer release()
	b.Publish("chat", gen, runner.Event{})
	if _, _, _, ok := b.TryBeginTurn(t.Context(), "chat"); ok {
		t.Fatal("idle claim superseded active work")
	}
	if ctx.Err() != nil || !b.IsCurrent("chat", gen) {
		t.Fatal("active turn changed")
	}
	replay, _, detach := b.Attach("chat")
	defer detach()
	if len(replay) != 1 {
		t.Fatal("denied claim changed replay")
	}
	release()
	background, bgGen, bgRelease, ok := b.TryBeginTurn(t.Context(), "chat")
	if !ok {
		t.Fatal("idle claim denied")
	}
	defer bgRelease()
	if bgGen <= gen {
		t.Fatal("generation not advanced")
	}
	_, humanGen, humanRelease := b.BeginTurn(t.Context(), "chat")
	defer humanRelease()
	if background.Err() == nil || !b.IsCurrent("chat", humanGen) {
		t.Fatal("user did not supersede background turn")
	}
	bgRelease()
	if !b.HasActiveTurn("chat") {
		t.Fatal("background release removed human turn")
	}
}

func TestConcurrentIdleClaimsHaveOneWinner(t *testing.T) {
	b := New()
	var wins atomic.Int32
	var wg sync.WaitGroup
	releases := make(chan func(), 32)
	start := make(chan struct{})
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, release, ok := b.TryBeginTurn(t.Context(), "chat")
			if ok {
				wins.Add(1)
				releases <- release
			}
		}()
	}
	close(start)
	wg.Wait()
	close(releases)
	for release := range releases {
		release()
	}
	if wins.Load() != 1 {
		t.Fatalf("winners=%d", wins.Load())
	}
}

func TestCanceledIdleClaimDoesNotRegisterTurn(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, _, ok := b.TryBeginTurn(ctx, "chat"); ok {
		t.Fatal("canceled claim accepted")
	}
	if b.HasActiveTurn("chat") {
		t.Fatal("canceled claim left active turn")
	}
}
