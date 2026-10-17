package agent

import (
	"testing"

	"cercano/source/server/internal/llm"
)

// reasoningMessage mirrors a Responses-API assistant turn that carried
// encrypted reasoning forward: opaque base64 in Block.ReasoningData with no
// text the tokenizer would otherwise see (incident CERCANO - PUBLISHING
// 612dc7356798440f: preflight/budget estimates ignored ReasoningData).
func reasoningMessage(id string, size int) llm.Message {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte('A' + i%26)
	}
	return llm.Message{Role: llm.RoleAssistant, Blocks: []llm.Block{{
		Type:          llm.BlockReasoning,
		ReasoningID:   id,
		ReasoningData: string(data),
	}}}
}

func TestEstimateRequestBudget_CountsReasoningData(t *testing.T) {
	// 32KB of opaque encrypted reasoning. Before the fix this message cost
	// ~1 token (its ID) and sailed through a 1200-token window; the provider
	// then replayed the blob as encrypted_content input.
	budget := EstimateRequestBudget(RequestBudgetInput{
		Messages:      []llm.Message{reasoningMessage("rs_1", 32*1024)},
		ContextWindow: 1200,
	})
	if budget.MessageTokens < 8*1024 {
		t.Fatalf("reasoning data must be charged (conservative len/4 ≈ 8k tokens), got MessageTokens=%d: %+v", budget.MessageTokens, budget)
	}
	if budget.Fits {
		t.Fatalf("reasoning-heavy request must overflow the 1200-token window: %+v", budget)
	}
}

func TestTrimMessagesToBudget_TrimsReasoningHeavyHistory(t *testing.T) {
	messages := []llm.Message{
		reasoningMessage("rs_1", 24*1024),
		reasoningMessage("rs_2", 24*1024),
		textMessage(llm.RoleUser, "current"),
	}
	trimmed, budget := TrimMessagesToBudget(RequestBudgetInput{
		Messages:      messages,
		MaxTokens:     64,
		ContextWindow: 1024,
	}, 1)
	if budget.MessageTokens < 12*1024 && len(trimmed) == len(messages) {
		t.Fatalf("reasoning-heavy history was not accounted or trimmed: %+v", budget)
	}
	if !budget.Fits {
		t.Fatalf("expected trimming to fit, got %+v", budget)
	}
	if len(trimmed) >= len(messages) {
		t.Fatalf("expected reasoning-heavy messages to be trimmed, kept %d of %d", len(trimmed), len(messages))
	}
	last := trimmed[len(trimmed)-1]
	if len(last.Blocks) != 1 || last.Blocks[0].Text != "current" {
		t.Fatalf("must preserve the current turn, got %+v", last)
	}
}
