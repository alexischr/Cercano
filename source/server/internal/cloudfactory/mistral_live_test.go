package cloudfactory

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"cercano/source/server/internal/inference"
	"cercano/source/server/internal/llm"
	"cercano/source/server/internal/mistralcatalog"
	"cercano/source/server/pkg/config"
)

// TestMistralLiveAdapterSmoke is explicitly opt-in and spends paid API quota.
// Run only with CERCANO_MISTRAL_LIVE_SMOKE=1 and MISTRAL_API_KEY in the environment.
// It makes one discovery request and at most three inference requests; it stops
// on the first failure. Credentials and private model names are never logged.
func TestMistralLiveAdapterSmoke(t *testing.T) {
	if os.Getenv("CERCANO_MISTRAL_LIVE_SMOKE") != "1" {
		t.Skip("paid live test requires explicit opt-in")
	}
	key := os.Getenv("MISTRAL_API_KEY")
	if key == "" {
		t.Fatal("MISTRAL_API_KEY is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	fail := func(stage string, err error) {
		t.Helper()
		var providerErr *llm.Error
		if errors.As(err, &providerErr) {
			t.Fatalf("%s: adapter error class=%s HTTP=%d", stage, providerErr.Class, providerErr.StatusCode)
		}
		t.Fatalf("%s: error type=%T (details omitted to avoid credential leakage)", stage, err)
	}
	models, err := mistralcatalog.Fetch(ctx, nil, mistralcatalog.Account{Name: "live-smoke", BaseURL: mistralcatalog.BaseURL, Key: key})
	if err != nil {
		fail("catalog", err)
	}
	selected := ""
	for _, preferred := range []string{"mistral-small-latest", "ministral-8b-latest", "mistral-medium-latest"} {
		for _, m := range models {
			if m.ID == preferred && m.Metadata.AgentEligible(time.Now()) {
				selected = m.ID
				break
			}
		}
		if selected != "" {
			break
		}
	}
	t.Logf("production catalog parser: %d models; selected public model=%q", len(models), selected)
	if selected == "" {
		t.Fatal("no preferred eligible model; no inference performed")
	}
	provider, err := BuildCloudProvider(config.CloudProfile{Name: "live-smoke", Provider: "mistral", Flavor: FlavorChatCompletions, BaseURL: mistralcatalog.BaseURL, Model: selected}, key)
	if err != nil {
		fail("factory", err)
	}
	t.Log("provider constructed through BuildCloudProvider; using production OpenAI-compatible Mistral adapter")
	req := inference.Call{Model: selected, MaxTokens: 32, Messages: []llm.Message{{Role: llm.RoleUser, Blocks: []llm.Block{{Type: llm.BlockText, Text: "Reply with exactly OK."}}}}}
	stream, err := provider.StreamChat(ctx, req)
	if err != nil {
		fail("stream open", err)
	}
	reply, err := llm.CollectStream(ctx, stream, nil, nil)
	stream.Close()
	if err != nil {
		fail("stream read", err)
	}
	text := func(blocks []llm.Block) string {
		var out strings.Builder
		for _, b := range blocks {
			if b.Type == llm.BlockText {
				out.WriteString(b.Text)
			}
		}
		return strings.TrimSpace(out.String())
	}
	if text(reply.Blocks) != "OK" {
		t.Fatal("stream output did not match expected literal")
	}
	t.Logf("stream passed; input_tokens=%d output_tokens=%d", reply.InputTokens, reply.OutputTokens)
	req = inference.Call{Model: selected, MaxTokens: 96, Tools: []llm.Tool{{Name: "smoke_echo", Description: "Harmless smoke-test echo; call once with value ping.", Schema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`)}}, Messages: []llm.Message{{Role: llm.RoleUser, Blocks: []llm.Block{{Type: llm.BlockText, Text: "Call smoke_echo with value ping. After its result, reply with exactly pong."}}}}}
	first, err := provider.Chat(ctx, req)
	if err != nil {
		fail("tool request", err)
	}
	var calls []llm.Block
	for _, b := range first.Blocks {
		if b.Type == llm.BlockToolUse {
			calls = append(calls, b)
		}
	}
	if len(calls) != 1 || calls[0].ToolName != "smoke_echo" || calls[0].ToolUseID == "" {
		t.Fatal("unexpected tool request; nothing executed")
	}
	var args map[string]string
	if json.Unmarshal(calls[0].ToolInput, &args) != nil || len(args) != 1 || args["value"] != "ping" {
		t.Fatal("unexpected tool arguments; nothing executed")
	}
	req.MaxTokens = 32
	req.Messages = append(req.Messages, llm.Message{Role: llm.RoleAssistant, Blocks: first.Blocks}, llm.Message{Role: llm.RoleUser, Blocks: []llm.Block{{Type: llm.BlockToolResult, ToolUseRef: calls[0].ToolUseID, Content: "pong"}}})
	second, err := provider.Chat(ctx, req)
	if err != nil {
		fail("tool continuation", err)
	}
	if text(second.Blocks) != "pong" {
		t.Fatal("tool continuation did not match expected literal")
	}
	t.Logf("tool continuation passed; exact tool ID retained (length=%d); no real tools executed", len(calls[0].ToolUseID))
}
