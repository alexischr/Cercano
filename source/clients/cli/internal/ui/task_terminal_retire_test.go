package ui

import (
	"testing"

	"cercano/source/server/pkg/agentclient"
	"cercano/source/server/pkg/proto"
	tea "charm.land/bubbletea/v2"
)

// Regression probe for "completed subagent tabs no longer auto-close": an
// independent (host-owned) task child that reaches `completed` while no parent
// turn is running must retire its own tab on the terminal snapshot — the same
// eager retirement dispatch sub-agents already get on their own "done" event.
func deliverTaskEvent(m Model, event *proto.TaskSubscriptionEvent) {
	p := &m
	p.applyTaskSubscription(taskSubscriptionMsg{
		parent:     m.convID,
		generation: m.taskSubscriptionGeneration,
		update:     agentclient.TaskUpdate{Event: event},
	})
}

func taskSnapshot(taskID, childID, status string) *proto.TaskSubscriptionEvent {
	return &proto.TaskSubscriptionEvent{Payload: &proto.TaskSubscriptionEvent_Task{Task: &proto.TaskLifecycleSnapshot{
		TaskId: taskID, ChildId: childID, Status: status, Task: "work",
	}}}
}

func TestTaskCompletionRetiresOwnTab(t *testing.T) {
	m := New(nil, false)
	m = send(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.convID = "owner"
	m.ensureTaskSubscription()

	deliverTaskEvent(m, taskSnapshot("task", "child", "running"))
	if tab := m.chatTabs.tabs["child"]; tab == nil || tab.done {
		t.Fatalf("running task tab missing or already terminal: %+v", m.chatTabs.tabs["child"])
	}
	// Parent turn is idle; the task finishes on its own.
	deliverTaskEvent(m, taskSnapshot("task", "child", "completed"))
	if m.chatTabs.tabs["child"] != nil {
		t.Fatalf("completed task tab retained instead of retiring: %+v", m.chatTabs.tabs["child"])
	}
	for _, id := range m.chatTabs.order {
		if id == "child" {
			t.Fatal("completed task id left in tab strip order")
		}
	}
	if m.chatTabs.active == "child" {
		t.Fatal("active tab left pointing at retired tab")
	}
}

func TestTaskFailureRetainsTabForPostMortem(t *testing.T) {
	m := New(nil, false)
	m = send(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.convID = "owner"
	m.ensureTaskSubscription()

	deliverTaskEvent(m, taskSnapshot("task", "child", "running"))
	deliverTaskEvent(m, taskSnapshot("task", "child", "failed"))
	tab := m.chatTabs.tabs["child"]
	if tab == nil || !tab.done || !tab.errored {
		t.Fatalf("failed task tab should linger done+errored for post-mortem: %+v", tab)
	}
}

func TestRestoredCompletedTaskTabNotRetired(t *testing.T) {
	m := New(nil, false)
	m = send(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.convID = "owner"
	m.ensureTaskSubscription()

	// Simulate a deliberately restored (reopened) task tab.
	m.ensureSubAgentTab("child", "", "", nil)
	restored := m.chatTabs.tabs["child"]
	restored.restored = true
	restored.applyTaskLifecycle("task", "running")
	deliverTaskEvent(m, taskSnapshot("task", "child", "completed"))
	if m.chatTabs.tabs["child"] == nil {
		t.Fatal("restored completed task tab was retired; restored tabs must be spared")
	}
	if tab := m.chatTabs.tabs["child"]; tab != nil && !tab.done {
		t.Fatal("restored tab should still reflect terminal status")
	}
}
