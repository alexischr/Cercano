package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"cercano/source/server/internal/agent"
	"cercano/source/server/internal/agenttools"
	"cercano/source/server/internal/llm"
	"cercano/source/server/pkg/proto"
	"google.golang.org/grpc"
)

type immediatePermissionStream struct {
	grpc.ServerStream
	ctx   context.Context
	reply func(*proto.PermissionRequired)
}

func (s *immediatePermissionStream) Context() context.Context { return s.ctx }
func (s *immediatePermissionStream) Send(response *proto.StreamProcessResponse) error {
	if prompt := response.GetPermissionRequired(); prompt != nil {
		s.reply(prompt)
	}
	return nil
}

func TestPermissionReplyDuringPromptDelivery(t *testing.T) {
	srv, _ := newServerWithStore(t)
	gated := &gatedExecTool{}
	registry := agenttools.NewRegistry()
	registry.MustRegister(gated)
	srv.SetToolRegistry(registry)
	srv.SetPermissions(agent.NewStaticPermissionStore(agent.ModePermissive), agent.NewPendingDecisions())
	srv.SetCloudLLMProvider(&scriptedProvider{caps: inferenceCapabilities(), scripts: [][]llm.Block{
		{{Type: llm.BlockToolUse, ToolUseID: "immediate", ToolName: "gated_work", ToolInput: json.RawMessage(`{}`)}},
		{{Type: llm.BlockText, Text: "done"}},
	}})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	replies := make(chan bool, 1)
	stream := &immediatePermissionStream{ctx: ctx, reply: func(prompt *proto.PermissionRequired) {
		accepted := srv.permBroker.Resolve("immediate-owner", prompt.ToolUseId, agent.Decision{Allow: true})
		replies <- accepted
		if !accepted {
			cancel()
		}
	}}
	err := srv.streamProcessRequestWithToolLoop(&proto.ProcessRequestRequest{ConversationId: "immediate-owner", Input: "work"}, stream)
	select {
	case accepted := <-replies:
		if !accepted {
			t.Fatal("prompt became visible before its approval waiter was registered")
		}
	default:
		t.Fatal("permission prompt not delivered")
	}
	if err != nil {
		t.Fatal(err)
	}
	if gated.execCount() != 1 {
		t.Fatalf("executions=%d", gated.execCount())
	}
}
