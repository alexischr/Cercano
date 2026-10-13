package server

import (
	"cercano/source/server/pkg/proto"
	"context"
	"testing"
	"time"
)

func TestForceStopTransportDoesNotWaitForActiveRPC(t *testing.T) {
	agent := &drainTestAgent{release: make(chan struct{})}
	defer close(agent.release)
	gs, client := startDrainTestServer(t, agent)
	defer gs.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, e := client.StreamProcessRequest(ctx, &proto.ProcessRequestRequest{Input: "owned fixture"})
	if e != nil {
		t.Fatal(e)
	}
	if msg, e := stream.Recv(); e != nil || msg.GetTokenDelta().GetContent() != "started" {
		t.Fatal(msg, e)
	}
	done := make(chan bool, 1)
	go func() { done <- StopWithoutDrain(gs) }()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("transport stop incomplete")
		}
	case <-ctx.Done():
		t.Fatal("force path waited for handler completion")
	}
	if _, e = stream.Recv(); e == nil {
		t.Fatal("transport remained open")
	}
	// The test still owns and releases the blocked handler: transport shutdown
	// alone is intentionally not treated as handler/child-process exit proof.
}
