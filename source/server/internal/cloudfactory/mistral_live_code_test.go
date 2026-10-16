package cloudfactory

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"cercano/source/server/internal/inference"
	"cercano/source/server/internal/llm"
	"cercano/source/server/internal/mistralcatalog"
	"cercano/source/server/pkg/config"
)

// Explicitly paid, opt-in integration test. At most two inference requests;
// the only executed tool reads one fixed repository source file.
func TestMistralLiveCodeRead(t *testing.T) {
	if os.Getenv("CERCANO_MISTRAL_LIVE_CODE") != "1" {
		t.Skip("paid coding-model test requires explicit opt-in")
	}
	key := os.Getenv("MISTRAL_API_KEY")
	if key == "" {
		t.Fatal("MISTRAL_API_KEY is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	models, err := mistralcatalog.Fetch(ctx, nil, mistralcatalog.Account{Name: "live-code", BaseURL: mistralcatalog.BaseURL, Key: key})
	if err != nil {
		t.Fatalf("catalog error type=%T", err)
	}
	selected := ""
	for _, id := range []string{"devstral-small-latest", "devstral-latest", "mistral-small-latest"} {
		for _, m := range models {
			if m.ID == id && m.Metadata.AgentEligible(time.Now()) {
				selected = id
				break
			}
		}
		if selected != "" {
			break
		}
	}
	// Prefer an exact coding-model ID discovered from this account's catalog.
	for _, m := range models {
		if strings.HasPrefix(m.ID, "devstral") && m.Metadata.AgentEligible(time.Now()) {
			selected = m.ID
			break
		}
	}
	if selected == "" {
		t.Fatal("no preferred eligible model; no inference performed")
	}
	t.Logf("live catalog confirmed model=%s", selected)
	provider, err := BuildCloudProvider(config.CloudProfile{Name: "live-code", Provider: "mistral", Flavor: FlavorChatCompletions, BaseURL: mistralcatalog.BaseURL, Model: selected}, key)
	if err != nil {
		t.Fatalf("factory error type=%T", err)
	}
	const path = "internal/llm/openai/mistral_content.go"
	req := inference.Call{Model: selected, MaxTokens: 256, Tools: []llm.Tool{{Name: "Read", Description: "Read the specified repository source file.", Schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","enum":["internal/llm/openai/mistral_content.go"]}},"required":["path"],"additionalProperties":false}`)}}, Messages: []llm.Message{{Role: llm.RoleUser, Blocks: []llm.Block{{Type: llm.BlockText, Text: "Read internal/llm/openai/mistral_content.go using the Read tool. Then analyze the implementation and return only a JSON object with these fields: signed_reasoning_supported (boolean: does it accept signed thinking chunks?), stream_buffering (string: per-line or whole-stream), response_limit_bytes (integer). Do not guess before reading the file."}}}}}
	run := func(call inference.Call) llm.ChatResponse {
		t.Helper()
		stream, err := provider.StreamChat(ctx, call)
		if err != nil {
			t.Fatalf("stream open error type=%T", err)
		}
		result, err := llm.CollectStream(ctx, stream, nil, nil)
		stream.Close()
		if err != nil {
			t.Fatalf("stream read error type=%T", err)
		}
		t.Logf("stream completed: input_tokens=%d output_tokens=%d", result.InputTokens, result.OutputTokens)
		return result
	}
	first := run(req)
	var calls []llm.Block
	for _, b := range first.Blocks {
		if b.Type == llm.BlockToolUse {
			calls = append(calls, b)
		}
	}
	if len(calls) != 1 || calls[0].ToolName != "Read" || calls[0].ToolUseID == "" {
		t.Fatal("expected exactly one Read tool call; nothing executed")
	}
	var args map[string]string
	if json.Unmarshal(calls[0].ToolInput, &args) != nil || len(args) != 1 || args["path"] != path {
		t.Fatal("tool requested unexpected arguments; nothing executed")
	}
	// Do not use a model-supplied filesystem path, even after validation.
	source, err := os.ReadFile("../llm/openai/mistral_content.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(source) > 32768 {
		t.Fatal("source exceeds test read bound")
	}
	t.Logf("executed restricted Read of %s (%d bytes)", path, len(source))
	req.Messages = append(req.Messages, llm.Message{Role: llm.RoleAssistant, Blocks: first.Blocks}, llm.Message{Role: llm.RoleUser, Blocks: []llm.Block{{Type: llm.BlockToolResult, ToolUseRef: calls[0].ToolUseID, Content: string(source)}}})
	second := run(req)
	var text strings.Builder
	for _, b := range second.Blocks {
		if b.Type == llm.BlockToolUse {
			t.Fatal("unexpected additional tool call; not executed")
		}
		if b.Type == llm.BlockText {
			text.WriteString(b.Text)
		}
	}
	var answer struct {
		Signed    *bool  `json:"signed_reasoning_supported"`
		Buffering string `json:"stream_buffering"`
		Limit     int    `json:"response_limit_bytes"`
	}
	payload := strings.TrimSpace(text.String())
	if strings.HasPrefix(payload, "```json\n") && strings.HasSuffix(payload, "\n```") {
		t.Log("model used a Markdown JSON fence; checking the enclosed answer, not claiming strict JSON compliance")
		payload = strings.TrimSuffix(strings.TrimPrefix(payload, "```json\n"), "\n```")
	}
	if err := json.Unmarshal([]byte(payload), &answer); err != nil {
		t.Fatalf("answer was not the requested JSON: %.512q", text.String())
	}
	if answer.Signed == nil || *answer.Signed || answer.Buffering != "per-line" || answer.Limit != 8388608 {
		t.Fatalf("incorrect code analysis: signed=%v buffering=%q limit=%d", answer.Signed, answer.Buffering, answer.Limit)
	}
	t.Log("verified real source analysis: signed reasoning rejected, per-line stream buffering, 8388608-byte response limit")
}
