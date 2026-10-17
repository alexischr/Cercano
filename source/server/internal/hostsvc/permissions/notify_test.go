package permissions

import (
	"cercano/source/server/internal/agent"
	"context"
	"errors"
	"testing"
)

func TestWaitWithNotifyRegistersBeforePublishing(t *testing.T) {
	broker := New(nil, agent.NewPendingDecisions(), nil)
	decision, err := broker.WaitWithNotify(t.Context(), "owner", "tool", func() error {
		if broker.Resolve("other", "tool", agent.Decision{Allow: true}) {
			t.Fatal("foreign approval accepted")
		}
		if !broker.Resolve("owner", "tool", agent.Decision{Allow: true}) {
			t.Fatal("prompt published without waiter")
		}
		return nil
	})
	if err != nil || !decision.Allow {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
	if broker.Resolve("owner", "tool", agent.Decision{Allow: true}) {
		t.Fatal("stale approval accepted")
	}
}
func TestWaitWithNotifyFailureCleansUp(t *testing.T) {
	broker := New(nil, agent.NewPendingDecisions(), nil)
	failed := errors.New("delivery failed")
	decision, err := broker.WaitWithNotify(t.Context(), "owner", "tool", func() error { return failed })
	if !errors.Is(err, failed) || decision.Allow {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
	if broker.Resolve("owner", "tool", agent.Decision{Allow: true}) {
		t.Fatal("failed prompt left an approval waiter")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = broker.WaitWithNotify(ctx, "owner", "tool", func() error { t.Fatal("canceled prompt published"); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestWaitWithNotifyWithoutBarrierDoesNotPublish(t *testing.T) {
	broker := New(nil, nil, nil)
	decision, err := broker.WaitWithNotify(t.Context(), "owner", "tool", func() error { t.Fatal("prompt published without barrier"); return nil })
	if err != nil || decision.Allow {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
}
