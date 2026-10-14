package state

import (
	"context"
	"testing"
	"time"
)

func TestForceControlPendingSaveDecision(t *testing.T) {
	s, a, f, id, now := publishedSaveFailure(t)
	ctx := context.Background()
	c := &ForceControl{store: s}
	pending, ok, e := c.PendingSaveDecision(ctx, now)
	if e != nil || !ok || pending.ReceiptID != id || pending.OpID != f.OpID {
		t.Fatal(pending, ok, e)
	}
	if _, ok, e = c.PendingSaveDecision(ctx, func() time.Time { return now().Add(time.Minute) }); e != nil || ok {
		t.Fatal("expired prompt", ok, e)
	}
	replacement, e := NewForceAgent(a.PID)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RegisterForceAgent(ctx, replacement); e != nil {
		t.Fatal(e)
	}
	if _, ok, e = c.PendingSaveDecision(ctx, now); e == nil || ok {
		t.Fatal("foreign lifetime prompt", ok, e)
	}
	if e = s.RegisterForceAgent(ctx, a); e != nil {
		t.Fatal(e)
	}
	if e = c.SubmitSaveDecision(ctx, id, ForceDecisionCancel, false, now); e != nil {
		t.Fatal(e)
	}
	if _, ok, e = c.PendingSaveDecision(ctx, now); e != nil || ok {
		t.Fatal("decided prompt", ok, e)
	}
}
