package agent

import (
	"context"
	"testing"
	"time"
)

// Reconnection can retransmit an approval. Exactly one answer may win, and a
// duplicate must not block the RPC if the waiter has not consumed the first.
func TestPendingDuplicateResolutionDoesNotBlock(t *testing.T) {
	p := NewPendingDecisions()
	p.channels[decisionKey("conv", "tool")] = make(chan Decision, 1)
	if !p.Resolve("conv", "tool", Decision{Allow: true}) {
		t.Fatal("first answer not delivered")
	}
	done := make(chan bool, 1)
	go func() { done <- p.Resolve("conv", "tool", Decision{Allow: false}) }()
	select {
	case accepted := <-done:
		if accepted {
			t.Fatal("duplicate answer accepted")
		}
	case <-time.After(time.Second):
		// Unblock the legacy implementation before failing the probe.
		<-p.channels[decisionKey("conv", "tool")]
		<-done
		t.Fatal("duplicate approval blocks while the original is pending")
	}
}

func TestPendingRegistersBeforeNotification(t *testing.T) {
	p := NewPendingDecisions()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	d, err := p.WaitWithNotify(ctx, "conv", "tool", func() error {
		if p.Resolve("other", "tool", Decision{Allow: true}) {
			t.Fatal("cross-conversation approval")
		}
		if !p.Resolve("conv", "tool", Decision{Allow: true}) {
			t.Fatal("approval prompt published before registration")
		}
		return nil
	})
	if err != nil || !d.Allow {
		t.Fatalf("decision %+v error %v", d, err)
	}
	if p.Resolve("conv", "tool", Decision{Allow: true}) {
		t.Fatal("stale approval accepted")
	}
}
func TestPendingCancellationRemovesApproval(t *testing.T) {
	p := NewPendingDecisions()
	ctx, cancel := context.WithCancel(context.Background())
	_, err := p.WaitWithNotify(ctx, "conv", "tool", func() error { cancel(); return nil })
	if err != context.Canceled {
		t.Fatalf("error %v", err)
	}
	if p.Resolve("conv", "tool", Decision{Allow: true}) {
		t.Fatal("canceled approval accepted")
	}
}
