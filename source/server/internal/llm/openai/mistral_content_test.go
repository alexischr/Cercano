package openai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMistralContentShapes(t *testing.T) {
	for _, field := range []string{"message", "delta"} {
		data := []byte(`{"choices":[{"` + field + `":{"content":[{"type":"thinking","thinking":[{"type":"text","text":"private thought"}]},{"type":"text","text":"answer"}]}}]}`)
		out, err := normalizeMistralContent(data)
		if err != nil {
			t.Fatal(err)
		}
		var decoded struct {
			Choices []map[string]struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning_content"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(out, &decoded); err != nil {
			t.Fatal(err)
		}
		got := decoded.Choices[0][field]
		if got.Content != "answer" || got.Reasoning != "private thought" {
			t.Fatalf("content channels mixed: %+v", got)
		}
	}
	for _, chunk := range []string{`{"type":"thinking","signature":"opaque","thinking":[]}`, `{"type":"unknown"}`} {
		_, err := normalizeMistralContent([]byte(`{"choices":[{"delta":{"content":[` + chunk + `]}}]}`))
		if err == nil || strings.Contains(err.Error(), "opaque") {
			t.Fatalf("unsupported signed content not safely rejected: %v", err)
		}
	}
}

func TestMistralQuirkIsExplicit(t *testing.T) {
	if !quirksFor("mistral").MistralContent {
		t.Fatal("missing adaptation")
	}
	for _, backend := range []string{"", "openai", "deepinfra", "cerebras"} {
		if quirksFor(backend).MistralContent {
			t.Fatalf("unrelated backend %q changed", backend)
		}
	}
}
