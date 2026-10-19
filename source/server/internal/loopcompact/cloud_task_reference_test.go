package loopcompact

import (
	"cercano/source/server/internal/compaction"
	"cercano/source/server/internal/inference"
	"cercano/source/server/internal/llm"
	"cercano/source/server/pkg/config"
	"strings"
	"testing"
)

// Exercise the actual cloud factory, not an injected summarizer that reads
// TaskReferenceFrom itself. The local and cloud prompt paths used to diverge.
func TestCloudSummarizerRetainsAssignedTaskReference(t *testing.T) {
	const task = "Implement quiet task UI; preserve approvals and stop acknowledgments."
	for _, assigned := range []bool{false, true} {
		p := &policyProvider{name: "secondary"}
		cfg := config.Config{}
		tiers := inference.Tiers{Cloud: p, TaskFor: cfg.TaskAssignment, Destinations: map[config.Destination]inference.Candidate{config.DestinationSecondary: {Provider: p, IsCloud: true}}}
		summarize := BuildSummarizer(WiringDeps{Cfg: cfg, Candidates: func() inference.Tiers { return tiers }})
		ctx := t.Context()
		if assigned {
			ctx = compaction.WithTaskReference(ctx, task)
		}
		_, err := summarize(ctx, []llm.Message{{Role: llm.RoleUser, Blocks: []llm.Block{{Type: llm.BlockText, Text: "inspected source code"}}}})
		if err != nil {
			t.Fatal(err)
		}
		if len(p.requests) != 1 {
			t.Fatalf("requests=%d", len(p.requests))
		}
		var prompt string
		for _, m := range p.requests[0].Messages {
			for _, b := range m.Blocks {
				prompt += b.Text
			}
		}
		if strings.Contains(prompt, task) != assigned {
			t.Fatalf("assigned=%v: cloud prompt lost or invented task reference", assigned)
		}
		if strings.Contains(prompt, "read-only task reference") != assigned {
			t.Fatal("task reference not separately delimited")
		}
	}
}
