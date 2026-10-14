package compactor

import (
	"encoding/json"
	"fmt"

	"cercano/source/server/internal/conversation"
	"cercano/source/server/internal/llm"
)

// historicalOrphanView preserves missing-result evidence as text in the derived
// compaction view, never in the stored transcript. A later persisted assistant
// message is the progress barrier: the tool loop persists results before asking
// the model for its next response. A user message alone is not such a barrier.
// Calls in the latest assistant emission remain protected, however old they are.
// Search the whole snapshot before cutting it: a result in the verbatim tail
// must continue to hold back its call at the freeze boundary.
func historicalOrphanView(turns []conversation.Turn) []conversation.Turn {
	blocks := make([][]llm.Block, len(turns))
	lastResult := map[string]int{}
	lastAssistant := -1
	for i, t := range turns {
		if t.BlocksJSON != "" {
			if err := json.Unmarshal([]byte(t.BlocksJSON), &blocks[i]); err != nil {
				// An unreadable turn might contain the missing result. Fail closed.
				return turns
			}
		}
		if t.Role == string(llm.RoleAssistant) && (len(blocks[i]) > 0 || t.Content != "") {
			lastAssistant = i
		}
		for _, b := range blocks[i] {
			if b.Type == llm.BlockToolResult {
				lastResult[b.ToolUseRef] = i
			}
		}
	}
	var out []conversation.Turn
	for i := 0; i < lastAssistant; i++ {
		changed := false
		for j, b := range blocks[i] {
			if b.Type != llm.BlockToolUse {
				continue
			}
			if result, ok := lastResult[b.ToolUseID]; ok && result > i {
				continue
			}
			raw, _ := json.Marshal(b)
			blocks[i][j] = llm.Block{Type: llm.BlockText, Text: fmt.Sprintf("[Historical tool call has no recorded result; execution outcome is unknown. Do not infer success or failure. Original call: %s]", raw)}
			changed = true
		}
		if changed {
			if out == nil {
				out = append([]conversation.Turn(nil), turns...)
			}
			encoded, _ := json.Marshal(blocks[i])
			out[i].BlocksJSON = string(encoded)
		}
	}
	if out == nil {
		return turns
	}
	return out
}
