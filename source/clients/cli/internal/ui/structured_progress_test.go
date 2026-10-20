package ui

import (
	"cercano/source/server/pkg/agentclient"
	"cercano/source/server/pkg/proto"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestStructuredActivityDoesNotDependOnWordingOrArrivalOrder(t *testing.T) {
	for _, debug := range []bool{false, true} {
		for _, before := range []bool{false, true} {
			for _, kind := range []proto.ProgressKind{proto.ProgressKind_PROGRESS_KIND_TOOL_ACTIVITY, proto.ProgressKind_PROGRESS_KIND_HOST_ACTIVITY} {
				for _, note := range []string{"running RunCommand", "Processing delegated task completions.", "Completely different wording."} {
					c := newTestChatView(100, 30)
					c.setTaskVisibility(debug, true)
					if !before {
						c.Apply(toolEntryStartMsg{id: "tool", name: "RunCommand"})
					}
					c.Apply(streamMsgToEvent(agentclient.StreamMsg{Type: agentclient.TypeProgress, Note: note, ProgressKind: kind}))
					if before {
						c.Apply(toolEntryStartMsg{id: "tool", name: "RunCommand"})
					}
					c.rebuild()
					text := ansi.Strip(chatLayoutContent(c))
					if strings.Contains(text, normalizeProgress(note)) {
						t.Fatalf("activity leaked: kind=%v debug=%v before=%v: %s", kind, debug, before, text)
					}
					if c.findToolEntry("tool") == nil {
						t.Fatal("tool row lost")
					}
				}
			}
		}
	}
}
func TestUnclassifiedProgressAndAssistantTextRemainVisible(t *testing.T) {
	for _, kind := range []proto.ProgressKind{0, 99} {
		c := newTestChatView(100, 30)
		c.setTaskVisibility(true, true)
		c.Apply(streamMsgToEvent(agentclient.StreamMsg{Type: agentclient.TypeProgress, Note: "Processing delegated task completions.", ProgressKind: kind}))
		c.Apply(chatAssistantDeltaMsg{token: "Actual assistant answer"})
		c.rebuild()
		text := ansi.Strip(chatLayoutContent(c))
		if !strings.Contains(text, "processing delegated task completions") || !strings.Contains(text, "Actual assistant answer") {
			t.Fatalf("meaningful output suppressed: %s", text)
		}
	}
}
