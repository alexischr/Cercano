package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"cercano/source/server/pkg/agentclient"
	"cercano/source/server/pkg/proto"
	tea "charm.land/bubbletea/v2"
)

// Regression probe for "task child tabs lost the formatted kickoff front
// matter": the durable SubscribeTasks replay must render the same
// subAgentStartEntry card the live dispatch path builds from its ephemeral
// "started" event, populated from the persisted dispatch_start record
// (DispatchStartEvent: mode/task/work_dir/provider/model/tier/is_cloud/
// granted_tools/...). Tests below use the real record shapes: a
// TaskLifecycleSnapshot followed by a TaskProgressRecord whose payload is the
// Recorder's DispatchStartEvent JSON.

// taskDispatchStartProgress builds the progress event the server streams for a
// persisted dispatch_start record (server/internal/server/task_subscription.go
// turns each DispatchEvent row into a TaskProgressRecord verbatim).
func taskDispatchStartProgress(taskID, childID string, seq int64, start DispatchStartEventShape) *proto.TaskSubscriptionEvent {
	payload, err := json.Marshal(start)
	if err != nil {
		panic(err)
	}
	return &proto.TaskSubscriptionEvent{Payload: &proto.TaskSubscriptionEvent_Progress{Progress: &proto.TaskProgressRecord{
		TaskId: taskID, ChildId: childID, Sequence: seq, Kind: "dispatch_start", PayloadJson: string(payload),
	}}}
}

// DispatchStartEventShape mirrors source/server/internal/dispatchhistory
// DispatchStartEvent's JSON tags without importing the server internals.
type DispatchStartEventShape struct {
	Mode         string   `json:"mode,omitempty"`
	Task         string   `json:"task,omitempty"`
	WorkDir      string   `json:"work_dir,omitempty"`
	Provider     string   `json:"provider,omitempty"`
	Model        string   `json:"model,omitempty"`
	Tier         string   `json:"tier,omitempty"`
	IsCloud      bool     `json:"is_cloud,omitempty"`
	GrantedTools []string `json:"granted_tools,omitempty"`
	IgnoredTools []string `json:"ignored_tools,omitempty"`
}

func deliverTaskEventRaw(m *Model, event *proto.TaskSubscriptionEvent) {
	m.applyTaskSubscription(taskSubscriptionMsg{
		parent:     m.convID,
		generation: m.taskSubscriptionGeneration,
		update:     agentclient.TaskUpdate{Event: event},
	})
}

func newTaskSubscriptionModel(t *testing.T) *Model {
	t.Helper()
	m := New(nil, false)
	m = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.convID = "owner"
	m.ensureTaskSubscription()
	return &m
}

func taskSnapshotEvent(taskID, childID, status, prompt string) *proto.TaskSubscriptionEvent {
	return &proto.TaskSubscriptionEvent{Payload: &proto.TaskSubscriptionEvent_Task{Task: &proto.TaskLifecycleSnapshot{
		TaskId: taskID, ChildId: childID, Status: status, Task: prompt,
	}}}
}

func taskKickoffCardEntries(m *Model, childID string) []*Entry {
	tab := m.chatTabs.tabs[childID]
	if tab == nil {
		return nil
	}
	var out []*Entry
	for _, e := range tab.view.Entries() {
		if e != nil && e.SubAgentStart != nil {
			out = append(out, e)
		}
	}
	return out
}

// A fresh task snapshot followed by the replayed dispatch_start record must
// show the formatted kickoff card (model, route, tier, tools) with the prompt
// preserved inside it — not a bare raw-prompt user entry.
func TestTaskSnapshotReplayShowsFormattedKickoffCard(t *testing.T) {
	m := newTaskSubscriptionModel(t)

	deliverTaskEventRaw(m, taskSnapshotEvent("task", "child", "running", "trace the dispatch behavior"))
	deliverTaskEventRaw(m, taskDispatchStartProgress("task", "child", 1, DispatchStartEventShape{
		Mode: "subagent", Task: "trace the dispatch behavior", Provider: "ollama",
		Model: "qwen3:8b", Tier: "standard", GrantedTools: []string{"Read", "Grep"},
	}))

	cards := taskKickoffCardEntries(m, "child")
	if len(cards) != 1 {
		t.Fatalf("expected exactly one kickoff card entry, got %d", len(cards))
	}
	card := cards[0].SubAgentStart
	if card.Model != "qwen3:8b" {
		t.Fatalf("kickoff card missing durable model metadata: %+v", card)
	}
	if card.Provider != "ollama" || card.Tier != "standard" {
		t.Fatalf("kickoff card missing provider/tier metadata: %+v", card)
	}
	if card.Route == "" {
		t.Fatalf("kickoff card missing route metadata: %+v", card)
	}
	if len(card.Tools) != 2 || card.Tools[0] != "Read" || card.Tools[1] != "Grep" {
		t.Fatalf("kickoff card missing granted tools: %+v", card)
	}
	if card.Task != "trace the dispatch behavior" {
		t.Fatalf("kickoff card lost the task prompt: %q", card.Task)
	}
	// The prompt must remain visible in the rendered transcript.
	tab := m.chatTabs.tabs["child"]
	rendered := tab.view.renderSubAgentStartCard(card, 100)
	if !strings.Contains(rendered, "trace the dispatch behavior") || !strings.Contains(rendered, "qwen3:8b") {
		t.Fatalf("kickoff card render missing prompt or model:\n%s", rendered)
	}
	// No duplicate raw-prompt user entry alongside the card.
	for _, e := range tab.view.Entries() {
		if e.SubAgentStart == nil && e.Role == RoleUser && e.Content == "trace the dispatch behavior" {
			t.Fatal("raw prompt entry duplicated alongside kickoff card")
		}
	}
}

// An accepted task that has not started yet has no dispatch_start record: the
// card must still appear (so the prompt is formatted, not raw) but without any
// fabricated route/model/tier/tools metadata.
func TestTaskAcceptedBeforeStartShowsPromptWithoutFabricatedMetadata(t *testing.T) {
	m := newTaskSubscriptionModel(t)

	deliverTaskEventRaw(m, taskSnapshotEvent("task", "child", "accepted", "trace the dispatch behavior"))

	cards := taskKickoffCardEntries(m, "child")
	if len(cards) != 1 {
		t.Fatalf("expected exactly one kickoff card entry, got %d", len(cards))
	}
	card := cards[0].SubAgentStart
	if card.Task != "trace the dispatch behavior" {
		t.Fatalf("accepted task card lost the prompt: %q", card.Task)
	}
	if card.Model != "" || card.Provider != "" || card.Tier != "" || card.Route != "" || len(card.Tools) != 0 {
		t.Fatalf("accepted task card fabricated start metadata: %+v", card)
	}
	rendered := m.chatTabs.tabs["child"].view.renderSubAgentStartCard(card, 100)
	if strings.Contains(rendered, "Model") || strings.Contains(rendered, "Route") || strings.Contains(rendered, "Tools") {
		t.Fatalf("accepted task card rendered fabricated metadata rows:\n%s", rendered)
	}
}

// Reconnect re-delivers the snapshot and replays dispatch_start from zero. The
// fresh snapshot replaces the view (existing behavior), so exactly one card
// and one prompt must remain — no duplicated header or prompt.
func TestTaskKickoffNoDuplicateHeaderOrPromptOnReconnect(t *testing.T) {
	m := newTaskSubscriptionModel(t)

	deliver := func() {
		deliverTaskEventRaw(m, taskSnapshotEvent("task", "child", "running", "trace the dispatch behavior"))
		deliverTaskEventRaw(m, taskDispatchStartProgress("task", "child", 1, DispatchStartEventShape{
			Provider: "ollama", Model: "qwen3:8b", Tier: "standard", GrantedTools: []string{"Read"},
		}))
	}
	deliver()
	// Simulate a reconnect: ensureTaskSubscription resets progress sequences.
	m.taskProgressSequences = make(map[string]int64)
	deliver()

	cards := taskKickoffCardEntries(m, "child")
	if len(cards) != 1 {
		t.Fatalf("reconnect duplicated the kickoff header: %d cards", len(cards))
	}
	if card := cards[0].SubAgentStart; card.Model != "qwen3:8b" {
		t.Fatalf("reconnect dropped card metadata: %+v", card)
	}
	tab := m.chatTabs.tabs["child"]
	prompts := 0
	for _, e := range tab.view.Entries() {
		if e.SubAgentStart == nil && e.Role == RoleUser && e.Content == "trace the dispatch behavior" {
			prompts++
		}
	}
	if prompts != 0 {
		t.Fatalf("reconnect duplicated the raw prompt entry %d time(s)", prompts)
	}
}

// A repeated dispatch_start replay (identical immutable record redelivered
// within one subscription) must update the existing card in place, never
// append a second header.
func TestTaskDispatchStartReplayUpdatesCardInPlace(t *testing.T) {
	m := newTaskSubscriptionModel(t)

	deliverTaskEventRaw(m, taskSnapshotEvent("task", "child", "running", "trace the dispatch behavior"))
	start := taskDispatchStartProgress("task", "child", 1, DispatchStartEventShape{
		Provider: "ollama", Model: "qwen3:8b", Tier: "standard", GrantedTools: []string{"Read"},
	})
	deliverTaskEventRaw(m, start)
	before := len(m.chatTabs.tabs["child"].view.Entries())
	deliverTaskEventRaw(m, start)
	after := len(m.chatTabs.tabs["child"].view.Entries())
	if before != after {
		t.Fatalf("replayed dispatch_start changed entry count: %d -> %d", before, after)
	}
	if cards := taskKickoffCardEntries(m, "child"); len(cards) != 1 {
		t.Fatalf("replayed dispatch_start duplicated the card: %d", len(cards))
	}
}
