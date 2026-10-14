package ui

import (
	"errors"
	"testing"

	"cercano/source/server/pkg/agentclient"
)

func TestSubAgentToolFetchRouting(t *testing.T) {
	for _, outcome := range []string{"success", "error", "missing", "closed", "replaced"} {
		t.Run(outcome, func(t *testing.T) {
			m := New(nil, false)
			m.applySubAgentEvent(subAgentEventMsg{id: "child", kind: "started"})
			m.switchChatTab("child")
			child := m.activeChat()
			childTool := &ToolEntry{ToolUseID: "same-id", ToolName: "Read", Status: ToolStatusComplete, Folded: true}
			mainTool := &ToolEntry{ToolUseID: "same-id", ToolName: "Read", Status: ToolStatusComplete, Loading: true}
			child.SetEntriesSlice([]*Entry{{Tool: childTool}})
			m.mainChat().SetEntriesSlice([]*Entry{{Tool: mainTool}})
			child.toggleEntryFold(0)
			if !childTool.Loading {
				t.Fatal("expansion did not set loading")
			}
			if cmds := m.dispatchToolFetches(); len(cmds) == 0 {
				t.Fatal("child fetch not dispatched")
			}
			if pending := child.TakePendingToolFetches(); len(pending) != 0 {
				t.Fatalf("child queue not drained: %v", pending)
			}
			m.switchChatTab(mainChatTabID)
			msg := toolCallFetchedMsg{id: "same-id", origin: child, tabID: "child", detail: &agentclient.ToolCallDetail{Found: true, ArgsJSON: "args", Result: "body", StartLine: 7}}
			switch outcome {
			case "error":
				msg.err = errors.New("fetch failed")
			case "missing":
				msg.detail.Found = false
			case "closed":
				delete(m.chatTabs.tabs, "child")
			case "replaced":
				delete(m.chatTabs.tabs, "child")
				m.ensureSubAgentTab("child", "", "", nil)
			}
			updated, _ := m.Update(msg)
			m = updated.(Model)
			if !mainTool.Loading || mainTool.FullResult != "" {
				t.Fatal("response changed main tool with identical ID")
			}
			if outcome == "closed" {
				if m.chatTabs.tabs["child"] != nil {
					t.Fatal("late response reopened tab")
				}
				return
			}
			if outcome == "replaced" {
				if !childTool.Loading {
					t.Fatal("late response updated replaced view")
				}
				return
			}
			if childTool.Loading {
				t.Fatal("child spinner not cleared")
			}
			if outcome == "success" {
				if childTool.FullResult != "body" || childTool.FullArgs != "args" || childTool.StartLine != 7 {
					t.Fatalf("details not populated: %+v", childTool)
				}
			} else if childTool.FullResult != "" {
				t.Fatal("failed fetch populated result")
			}
		})
	}
}

func TestMainToolFetchWhileChildActive(t *testing.T) {
	m := New(nil, false)
	main := m.mainChat()
	tool := &ToolEntry{ToolUseID: "main-tool", ToolName: "Read", Status: ToolStatusComplete, Folded: true}
	main.SetEntriesSlice([]*Entry{{Tool: tool}})
	main.toggleEntryFold(0)
	m.applySubAgentEvent(subAgentEventMsg{id: "child", kind: "started"})
	m.switchChatTab("child")
	if len(m.dispatchToolFetches()) == 0 {
		t.Fatal("main fetch not dispatched")
	}
	if len(main.TakePendingToolFetches()) != 0 {
		t.Fatal("main queue not drained")
	}
	updated, _ := m.Update(toolCallFetchedMsg{id: "main-tool", origin: main, tabID: mainChatTabID, detail: &agentclient.ToolCallDetail{Found: true, Result: "main body"}})
	m = updated.(Model)
	if tool.Loading || tool.FullResult != "main body" {
		t.Fatal("main fetch not applied")
	}
	if m.chatTabs.active != "child" {
		t.Fatal("fetch stole focus")
	}
}
