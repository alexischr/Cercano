package compaction

import (
	"testing"

	"cercano/source/server/internal/contextmeter"
	"cercano/source/server/internal/llm"
)

func TestMessageTokens_CountsReasoningData(t *testing.T) {
	tok := contextmeter.Default()
	// Incident (CERCANO - PUBLISHING 612dc7356798440f): estimates ignored
	// Block.ReasoningData while the responses adapter replays the blob to the
	// provider as encrypted_content. A reasoning-heavy history must not look
	// free to the canonical calculator.
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte('a' + i%26)
	}
	plain := textMsg(llm.RoleAssistant, "done")
	reasoned := llm.Message{Role: llm.RoleAssistant, Blocks: []llm.Block{{
		Type:          llm.BlockReasoning,
		ReasoningID:   "rs_1",
		ReasoningData: string(data),
	}}}

	rawCost := MessageTokens(tok, plain)
	rawReasoned := MessageTokens(tok, reasoned)
	if rawReasoned <= rawCost {
		t.Fatalf("reasoning data must add raw cost: got %d, plain text cost %d", rawReasoned, rawCost)
	}
	if want := len(data) / 4; rawReasoned < want {
		t.Fatalf("reasoning charge %d below conservative len/4 estimate %d", rawReasoned, want)
	}

	providerCost := ProviderMessageTokens(tok, reasoned)
	if providerCost <= 0 {
		t.Fatalf("provider-facing reasoning cost = %d, want nonzero (adapter replays encrypted_content)", providerCost)
	}
	if providerCost < len(data)/4 {
		t.Fatalf("provider reasoning charge %d below conservative len/4 estimate %d", providerCost, len(data)/4)
	}

	// Totals inherit the charge: a reasoning-heavy history must dominate a
	// same-size plain-text history in both views.
	heavy := []llm.Message{reasoned}
	if TotalTokens(tok, heavy) <= TotalTokens(tok, []llm.Message{textMsg(llm.RoleAssistant, "done")}) {
		t.Fatalf("TotalTokens ignores reasoning data")
	}
	if ProviderTotalTokens(tok, heavy) <= ProviderTotalTokens(tok, []llm.Message{textMsg(llm.RoleAssistant, "done")}) {
		t.Fatalf("ProviderTotalTokens ignores reasoning data")
	}
}
