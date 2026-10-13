package broker

import (
	"cercano/source/server/internal/runner"
	"context"
	"testing"
)

func TestShutdownInterruptionPreservesTerminalGenerationAndReplay(t *testing.T) {
	b := New()
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx, gen, release := b.BeginTurn(parent, "one")
	other, otherGen, otherRelease := b.BeginTurn(parent, "two")
	before := runner.Event{}
	b.Publish("one", gen, before)
	if n := b.InterruptActiveTurns(); n != 2 {
		t.Fatal("owned turn count", n)
	}
	if ctx.Err() != context.Canceled || other.Err() != context.Canceled {
		t.Fatal("owned contexts not interrupted")
	}
	if parent.Err() != nil {
		t.Fatal("parent/unrelated context canceled")
	}
	if !b.IsCurrent("one", gen) || !b.IsCurrent("two", otherGen) {
		t.Fatal("terminal persistence fenced out")
	}
	if !b.HasActiveTurn("one") || !b.HasActiveTurn("two") {
		t.Fatal("cancellation was treated as completion")
	}
	b.mu.Lock()
	n := len(b.convs["one"].buffer)
	b.mu.Unlock()
	if n != 1 {
		t.Fatal("replay discarded")
	}
	b.Publish("one", gen, runner.Event{}) // terminal event can still be delivered
	b.mu.Lock()
	n = len(b.convs["one"].buffer)
	b.mu.Unlock()
	if n != 2 {
		t.Fatal("terminal event lost")
	}
	if n := b.InterruptActiveTurns(); n != 2 {
		t.Fatal("repeat forgot live ownership", n)
	}
	release()
	otherRelease()
	if b.InterruptActiveTurns() != 0 {
		t.Fatal("released turn retained")
	}
}
func TestShutdownInterruptionDoesNotResurrectSupersededTurns(t *testing.T) {
	b := New()
	old, oldGen, oldRelease := b.BeginTurn(context.Background(), "one")
	defer oldRelease()
	current, gen, release := b.BeginTurn(context.Background(), "one")
	defer release()
	if old.Err() != context.Canceled || b.IsCurrent("one", oldGen) {
		t.Fatal("supersession fixture")
	}
	b.InterruptActiveTurns()
	if current.Err() != context.Canceled || !b.IsCurrent("one", gen) || b.IsCurrent("one", oldGen) {
		t.Fatal("generation ownership changed")
	}
}
