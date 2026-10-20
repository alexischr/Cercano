package ui

import "testing"

func TestAllToolRowsSuppressRedundantRunningProgress(t *testing.T) {
	for _, name := range []string{"git_push", "Read", "task_wait", "dispatch", "workflow", "mcp__vendor__new_tool", "future_tool"} {
		t.Run(name, func(t *testing.T) {
			m := newTaskSubscriptionModel(t)
			view := m.mainChat()
			view.Apply(toolEntryStartMsg{id: "tool", name: name})
			view.Apply(chatProgressMsg{note: "running " + name})
			for _, e := range view.Entries() {
				if e.Content == "running "+name || e.Status == "running "+name {
					t.Fatalf("redundant tool progress retained: %+v", e)
				}
			}
			if view.findToolEntry("tool") == nil {
				t.Fatal("tool row removed")
			}
			view.Apply(chatProgressMsg{note: "⚠ retrying connection"})
			found := false
			for _, e := range view.Entries() {
				if e.Content == "⚠ retrying connection" {
					found = true
				}
			}
			if !found {
				t.Fatal("warning suppressed")
			}
		})
	}
}
