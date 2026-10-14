package compactor

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"cercano/source/server/internal/compaction"
	"cercano/source/server/internal/contextmeter"
	"cercano/source/server/internal/conversation"
	"cercano/source/server/internal/llm"
)

func TestAdvanceHistoricalOrphan(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		laterAssistant, result bool
		want                   bool
	}{
		{"historical", true, false, true},
		{"possibly_running", false, false, false},
		{"result_in_tail", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			turns := []conversation.Turn{
				{Role: "assistant", BlocksJSON: `[{"type":"tool_use","id":"lost","name":"dispatch","input":{"task":"investigate"}}]`, CreatedAt: time.Unix(100, 0)},
				turn("later", "user", "followup", 101),
				turn("tail", "user", "keep live", 102),
			}
			if tc.laterAssistant {
				turns[1].Role = "assistant"
			}
			if tc.result {
				turns[2].BlocksJSON = `[{"type":"tool_result","tool_use_id":"lost","content":"actual result"}]`
			}
			before := append([]conversation.Turn(nil), turns...)
			var seen string
			summarize := func(_ context.Context, msgs []llm.Message) (compaction.StructuredSummary, error) {
				seen += flat(msgs)
				return compaction.StructuredSummary{Goal: "summary"}, nil
			}
			st, changed, _, err := Advance(context.Background(), turns, conversation.Compaction{}, summarize, Config{ActivationFloorTokens: 1, SegmentTokens: 1, VerbatimRecent: 1}, contextmeter.Default())
			if err != nil {
				t.Fatal(err)
			}
			if changed != tc.want {
				t.Fatalf("changed=%v want=%v boundary=%d summary=%q", changed, tc.want, st.FrozenThrough, seen)
			}
			if tc.want && (!strings.Contains(seen, "no recorded result") || !strings.Contains(seen, "investigate")) {
				t.Fatalf("lost orphan evidence: %s", seen)
			}
			if !reflect.DeepEqual(turns, before) {
				t.Fatal("mutated stored history")
			}
		})
	}
}

func TestHistoricalOrphanViewGuards(t *testing.T) {
	call := conversation.Turn{Role: "assistant", BlocksJSON: `[{"type":"tool_use","id":"old","name":"dispatch","input":{}}]`}
	latest := conversation.Turn{Role: "assistant", BlocksJSON: `[{"type":"tool_use","id":"current","name":"Read","input":{}}]`}
	for _, tc := range []struct {
		name    string
		turns   []conversation.Turn
		changed bool
	}{
		{"latest_call_is_protected", []conversation.Turn{call, latest}, true},
		{"user_is_not_completion", []conversation.Turn{call, {Role: "user", Content: "still running?"}}, false},
		{"malformed_result_fails_closed", []conversation.Turn{call, {Role: "user", BlocksJSON: `[{broken`}, latest}, false},
		{"late_result_preserved", []conversation.Turn{call, latest, {Role: "user", BlocksJSON: `[{"type":"tool_result","tool_use_id":"old","content":"done"}]`}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]conversation.Turn(nil), tc.turns...)
			out := historicalOrphanView(tc.turns)
			if changed := !reflect.DeepEqual(out, tc.turns); changed != tc.changed {
				t.Fatalf("changed=%v want=%v", changed, tc.changed)
			}
			if !reflect.DeepEqual(before, tc.turns) {
				t.Fatal("mutated input")
			}
			for i, tr := range tc.turns {
				if tr.BlocksJSON == latest.BlocksJSON && out[i] != tr {
					t.Fatal("changed potentially running call")
				}
			}
		})
	}
}
