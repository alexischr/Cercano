package server

import (
	"cercano/source/server/internal/capabilities/builtins"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"cercano/source/server/internal/capabilities"
	"cercano/source/server/pkg/proto"
)

type taskScopeProbe struct {
	testEchoCap
	seen *string
}

func (p taskScopeProbe) Execute(_ context.Context, c *capabilities.Call) (*capabilities.Result, error) {
	*p.seen = c.ConversationID
	return capabilities.NewTextResult("observed"), nil
}

// The MCP-facing invocation currently supplies no trusted conversation scope.
// A model-supplied conversation_id is data, not task-control authorization.
func TestInvokeCapabilityDoesNotBindConversationFromArguments(t *testing.T) {
	seen := "not called"
	reg := capabilities.NewRegistry(capabilities.Services{})
	reg.MustRegister(taskScopeProbe{seen: &seen})
	s := NewServer(nil, nil, nil, nil, nil)
	s.toolSvc.SetCapRegistry(reg)
	response, err := s.InvokeCapability(context.Background(), &proto.InvokeCapabilityRequest{Name: "echo", ArgsJson: json.RawMessage(`{"conversation_id":"someone-else"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if response.IsError {
		t.Fatal(response.Error)
	}
	if seen != "" {
		t.Fatalf("unexpected trusted conversation scope %q", seen)
	}
}

// Missing external-chat binding must fail closed, even when an attacker knows
// both the owner's conversation ID and a valid task ID.
func TestInvokeTaskControlsRejectUnboundExternalChat(t *testing.T) {
	reg := capabilities.NewRegistry(capabilities.Services{})
	for _, op := range []string{"list", "inspect", "wait", "stop"} {
		reg.MustRegister(builtins.TaskControl(op))
	}
	s := NewServer(nil, nil, nil, nil, nil)
	s.toolSvc.SetCapRegistry(reg)
	for _, op := range []string{"list", "inspect", "wait", "stop"} {
		response, err := s.InvokeCapability(context.Background(), &proto.InvokeCapabilityRequest{Name: "task_" + op, ArgsJson: []byte(`{"conversation_id":"victim", "task_id":"known-task"}`)})
		if err != nil {
			t.Fatal(err)
		}
		if !response.IsError || !strings.Contains(response.Error, "trusted per-chat binding missing") {
			t.Fatalf("%s: %+v", op, response)
		}
		if len(response.ResultJson) > 0 {
			t.Fatalf("%s leaked a result: %s", op, response.ResultJson)
		}
	}
}
