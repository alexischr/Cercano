package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"cercano/source/server/internal/agenttools"
	"cercano/source/server/internal/dispatch"
	"cercano/source/server/internal/inference"
	"cercano/source/server/internal/llm"
)

// lifecycleBlockingTool exposes the cancellation actually received by a child,
// without inference timing or sleeps. Its result channel is buffered so a
// failing test cannot strand the runner while reporting what it observed.
type lifecycleBlockingTool struct {
	started  chan struct{}
	canceled chan error
}

func (t *lifecycleBlockingTool) Name() string { return "lifecycle_block" }
func (t *lifecycleBlockingTool) Description() string {
	return "Wait for cancellation in a lifecycle test"
}
func (t *lifecycleBlockingTool) Permission() agenttools.Permission { return agenttools.PermR }
func (t *lifecycleBlockingTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *lifecycleBlockingTool) Execute(ctx context.Context, _ json.RawMessage) (*agenttools.Result, error) {
	close(t.started)
	<-ctx.Done()
	t.canceled <- ctx.Err()
	return nil, ctx.Err()
}

// This characterizes the synchronous execution primitive, which must continue
// honoring its owner context. Independent public dispatch must instead supply
// a task-owned context: removing cancellation from this primitive is not a fix.
func TestAgenticExecutionHonorsOwnerCancellation(t *testing.T) {
	tool := &lifecycleBlockingTool{started: make(chan struct{}), canceled: make(chan error, 1)}
	reg := agenttools.NewRegistry()
	reg.MustRegister(tool)
	srv := NewServer(nil, nil, nil, nil, nil)
	srv.SetToolRegistry(reg)
	provider := &scriptedProvider{
		caps:    inference.Capabilities{SupportsTools: true},
		scripts: [][]llm.Block{{{Type: llm.BlockToolUse, ToolUseID: "block-1", ToolName: tool.Name(), ToolInput: json.RawMessage(`{}`)}}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = srv.runAgenticDispatch(ctx, dispatch.Spec{Mode: dispatch.Agentic, Task: "wait", Tools: []string{tool.Name()}}, dispatch.Selection{Provider: provider}, "test-model")
	}()
	select {
	case <-tool.started:
	case <-time.After(5 * time.Second):
		t.Fatal("child never reached its tool")
	}
	cancel()
	select {
	case err := <-tool.canceled:
		if err != context.Canceled {
			t.Fatalf("child cancellation = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child did not observe owner cancellation")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("child execution did not terminate")
	}
}
