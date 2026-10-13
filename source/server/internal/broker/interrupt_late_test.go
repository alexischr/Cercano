package broker

import (
	"context"
	"testing"
)

func TestShutdownInterruptionRefusesLateTurnRegistration(t *testing.T) {
	b := New()
	b.InterruptActiveTurns()
	// A handler admitted just before the outer seal can arrive here afterward.
	ctx, gen, release := b.BeginTurn(context.Background(), "late")
	defer release()
	if ctx.Err() != context.Canceled || b.HasActiveTurn("late") || b.IsCurrent("late", gen) {
		t.Fatal("late registration escaped shutdown interruption")
	}
}
