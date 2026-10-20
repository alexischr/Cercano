package ui

import (
	"cercano/source/server/pkg/proto"
	"strings"
	"testing"
)

func TestDispatchStatusAndLink(t *testing.T) {
	for _, snapshotFirst := range []bool{false, true} {
		m := newTaskSubscriptionModel(t)
		for _, name := range []string{"dispatch", "workflow"} {
			id := "launch-" + name
			child := "child-" + name
			m.mainChat().Apply(toolEntryStartMsg{id: id, name: name})
			m.mainChat().Apply(chatProgressMsg{note: "running " + name})
			snapshot := func() {
				deliverTaskEventRaw(m, &proto.TaskSubscriptionEvent{Payload: &proto.TaskSubscriptionEvent_Task{Task: &proto.TaskLifecycleSnapshot{TaskId: "task-" + name, ChildId: child, Status: "running", Task: "work"}}})
			}
			if snapshotFirst {
				snapshot()
			}
			m.applySubAgentEvent(subAgentEventMsg{id: child, parentID: "owner", kind: "started", toolUseID: id})
			m.mainChat().Apply(toolEntryExecCompleteMsg{id: id, summary: "truncated receipt"})
			if !snapshotFirst {
				snapshot()
			}
			tool := m.mainChat().findToolEntry(id)
			if tool.SubAgentID != child {
				t.Fatalf("wrong child for %s: %+v", id, tool)
			}
			if !strings.Contains(stripAnsiCSI(renderToolEntry(*tool, 160, false, m.styles, m.mainChat().md)), "open tab") {
				t.Errorf("missing link for %s", name)
			}
		}
		for _, e := range m.mainChat().Entries() {
			if e.Content == "running dispatch" || e.Content == "running workflow" {
				t.Errorf("redundant launch status: %q", e.Content)
			}
		}
	}
}
func TestDispatchProgressWithoutToolRowStillVisible(t *testing.T) {
	m := newTaskSubscriptionModel(t)
	m.mainChat().Apply(chatProgressMsg{note: "running dispatch"})
	found := false
	for _, e := range m.mainChat().Entries() {
		if e.Content == "running dispatch" {
			found = true
		}
	}
	if !found {
		t.Fatal("unrepresented status suppressed")
	}
}
