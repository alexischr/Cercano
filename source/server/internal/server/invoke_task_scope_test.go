package server

import (
	"context"
	"encoding/json"
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
