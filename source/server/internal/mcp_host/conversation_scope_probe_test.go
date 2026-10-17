package mcphost

import (
	"context"
	"encoding/json"
	"testing"

	"cercano/source/server/internal/agenttools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Characterizes the missing binding; replace these absence expectations when
// conversation scope is explicitly carried across the MCP boundary.
func TestConversationScopeProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type observation struct {
		SessionID      string   `json:"session_id"`
		Meta           mcp.Meta `json:"meta"`
		ConversationID string   `json:"conversation_id"`
	}
	connect := func() *conn {
		srv := mcp.NewServer(&mcp.Implementation{Name: "scope-probe", Version: "1"}, nil)
		mcp.AddTool(srv, &mcp.Tool{Name: "probe"}, func(ctx context.Context, req *mcp.CallToolRequest, in echoIn) (*mcp.CallToolResult, any, error) {
			data, err := json.Marshal(observation{req.Session.ID(), req.Params.Meta, agenttools.ConversationIDFromContext(ctx)})
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, err
		})
		serverT, clientT := mcp.NewInMemoryTransports()
		go func() { _ = srv.Run(ctx, serverT) }()
		c, err := dial(ctx, clientT)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.close() })
		return c
	}
	probe := func(c *conn, id string) {
		callCtx := agenttools.WithConversationID(ctx, id)
		text, _, isErr, err := c.call(callCtx, "probe", json.RawMessage(`{"text":"probe"}`))
		if err != nil || isErr {
			t.Fatalf("call: %v, %s", err, text)
		}
		var got observation
		if err := json.Unmarshal([]byte(text), &got); err != nil {
			t.Fatal(err)
		}
		t.Logf("caller=%s receiver=%s", id, text)
		if got.SessionID != "" || len(got.Meta) != 0 || got.ConversationID != "" {
			t.Fatalf("scope is now transmitted; update this characterization: %+v", got)
		}
	}
	c := connect()
	probe(c, "conversation-a")
	probe(c, "conversation-b")
	if err := c.close(); err != nil {
		t.Fatal(err)
	}
	probe(connect(), "conversation-a")
}
