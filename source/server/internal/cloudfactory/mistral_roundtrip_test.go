package cloudfactory

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"cercano/source/server/internal/inference"
	"cercano/source/server/internal/llm"
	"cercano/source/server/pkg/config"
)

func TestMistralFactoryStreamingToolRoundtrip(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Errorf("wrong endpoint or credential routing: %s", r.URL.Path)
		}
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role       string `json:"role"`
				Content    string `json:"content"`
				ToolCallID string `json:"tool_call_id"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if req.Model != "mistral-fixture" {
			t.Errorf("model=%q", req.Model)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls.Add(1) == 1 {
			fmt.Fprint(w, "data: "+`{"id":"fixture","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"abc123XYZ","type":"function","function":{"name":"Read","arguments":"{\"path\":"}}]}}]}`+"\n\n")
			fmt.Fprint(w, "data: "+`{"id":"fixture","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"file\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`+"\n\n")
		} else {
			last := req.Messages[len(req.Messages)-1]
			if last.Role != "tool" || last.ToolCallID != "abc123XYZ" || last.Content != "contents" {
				t.Errorf("tool correlation lost: %+v", last)
			}
			fmt.Fprint(w, "data: "+`{"id":"fixture2","choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":2,"total_tokens":22}}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	p, err := BuildCloudProvider(config.CloudProfile{Name: "work", Provider: "mistral", Flavor: "chat_completions", BaseURL: srv.URL + "/v1", Model: "mistral-fixture"}, "fixture-key")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	req := inference.Call{Tools: []llm.Tool{{Name: "Read", Description: "Read a file", Schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)}}, Model: "mistral-fixture", Messages: []llm.Message{{Role: llm.RoleUser, Blocks: []llm.Block{{Type: llm.BlockText, Text: "read file"}}}}}
	stream, err := p.StreamChat(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	first, err := llm.CollectStream(ctx, stream, nil, nil)
	stream.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Blocks) != 1 || first.Blocks[0].ToolUseID != "abc123XYZ" || string(first.Blocks[0].ToolInput) != `{"path":"file"}` {
		t.Fatalf("split tool assembly: %+v", first)
	}
	req.Messages = append(req.Messages, llm.Message{Role: llm.RoleAssistant, Blocks: first.Blocks}, llm.Message{Role: llm.RoleUser, Blocks: []llm.Block{{Type: llm.BlockToolResult, ToolUseRef: first.Blocks[0].ToolUseID, Content: "contents"}}})
	stream, err = p.StreamChat(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := llm.CollectStream(ctx, stream, nil, nil)
	stream.Close()
	if err != nil {
		t.Fatal(err)
	}
	if first.InputTokens != 10 || first.OutputTokens != 5 || second.InputTokens != 20 || second.OutputTokens != 2 {
		t.Fatalf("usage lost: first=%+v second=%+v", first, second)
	}
	if len(second.Blocks) != 1 || second.Blocks[0].Text != "done" || calls.Load() != 2 {
		t.Fatalf("roundtrip=%+v calls=%d", second, calls.Load())
	}
}
