package server

import (
	"context"
	"encoding/json"
	"testing"

	"cercano/source/server/internal/capabilities"
	"cercano/source/server/pkg/proto"
)

type conversationScopeProbe struct {
	testEchoCap
	seenConversationID *string
}

func (p conversationScopeProbe) Execute(_ context.Context, c *capabilities.Call) (*capabilities.Result, error) {
	*p.seenConversationID = c.ConversationID
	return capabilities.NewTextResult("observed"), nil
}

// TestMCPConversationScope demonstrates the missing conversation scope issue.
// The MCP adapter currently receives req.Session but InvokeCapability forwards
// only name,args,work_dir - losing the session-bound conversation identity.
func TestMCPConversationScopeMissing(t *testing.T) {
	seen := ""
	reg := capabilities.NewRegistry(capabilities.Services{})
	reg.MustRegister(conversationScopeProbe{seenConversationID: &seen})
	s := NewServer(nil, nil, nil, nil, nil)
	s.toolSvc.SetCapRegistry(reg)

	// Simulate an MCP call that should have conversation scope
	response, err := s.InvokeCapability(context.Background(), &proto.InvokeCapabilityRequest{
		Name:     "echo",
		ArgsJson: json.RawMessage(`{"message":"test"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.IsError {
		t.Fatal(response.Error)
	}

	// Currently: ConversationID is empty because MCP session scope is lost
	if seen != "" {
		t.Fatalf("unexpected conversation scope: got %q, want empty", seen)
	}
}

// TestMCPConversationScopeModelArgumentCannotOverride verifies that
// a model-supplied conversation_id cannot override bound session identity.
func TestMCPConversationScopeModelArgumentCannotOverride(t *testing.T) {
	seen := ""
	reg := capabilities.NewRegistry(capabilities.Services{})
	reg.MustRegister(conversationScopeProbe{seenConversationID: &seen})
	s := NewServer(nil, nil, nil, nil, nil)
	s.toolSvc.SetCapRegistry(reg)

	// Try to override with model argument - should be ignored
	response, err := s.InvokeCapability(context.Background(), &proto.InvokeCapabilityRequest{
		Name:     "echo",
		ArgsJson: json.RawMessage(`{"message":"test", "conversation_id":"model-supplied"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.IsError {
		t.Fatal(response.Error)
	}

	// Model-supplied conversation_id should be ignored when session scope is bound
	if seen != "" {
		t.Fatalf("model-supplied conversation_id should not override bound scope: got %q, want empty", seen)
	}
}
