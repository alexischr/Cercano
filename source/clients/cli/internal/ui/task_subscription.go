package ui

import (
	"context"
	"encoding/json"

	"cercano/source/server/pkg/agentclient"
	tea "charm.land/bubbletea/v2"
)

type taskSubscriptionMsg struct {
	parent     string
	generation uint64
	update     agentclient.TaskUpdate
	next       tea.Cmd
}

func (m *Model) ensureTaskSubscription() tea.Cmd {
	if m.taskSubscriptionParent == m.convID && m.taskSubscriptionClient == m.agent {
		return nil
	}
	if m.taskSubscriptionCancel != nil {
		m.taskSubscriptionCancel()
		m.taskSubscriptionCancel = nil
	}
	m.taskSubscriptionClient = m.agent
	m.taskSubscriptionParent = m.convID
	m.taskSubscriptionGeneration++
	m.taskProgressSequences = make(map[string]int64)
	m.backgroundTurns = nil
	m.backgroundPrompts = nil
	if m.agent == nil || m.convID == "" {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.taskSubscriptionCancel = cancel
	parent, generation, client := m.convID, m.taskSubscriptionGeneration, m.agent
	taskCmd := func() tea.Msg {
		updates, err := client.SubscribeTasks(ctx, parent)
		if err != nil {
			return taskSubscriptionMsg{parent: parent, generation: generation, update: agentclient.TaskUpdate{Err: err}}
		}
		var wait tea.Cmd
		wait = func() tea.Msg {
			select {
			case update, ok := <-updates:
				if !ok {
					return nil
				}
				return taskSubscriptionMsg{parent: parent, generation: generation, update: update, next: wait}
			case <-ctx.Done():
				return nil
			}
		}
		return wait()
	}
	return tea.Batch(taskCmd, attachMainConversationCmd(ctx, client, parent, generation))
}
func (m *Model) applyTaskSubscription(msg taskSubscriptionMsg) tea.Cmd {
	if msg.parent != m.convID || msg.generation != m.taskSubscriptionGeneration {
		return nil
	}
	if msg.update.Err != nil {
		m.mainChat().AppendEntry(&Entry{Role: RoleSystem, Content: "Task updates disconnected: " + msg.update.Err.Error()})
		return nil
	}
	event := msg.update.Event
	if event == nil {
		return msg.next
	}
	m.ensureChatTabs()
	if task := event.GetTask(); task != nil && !m.chatTabs.closed[task.ChildId] {
		m.ensureSubAgentTab(task.ChildId, "", "", nil)
		tab := m.chatTabs.tabs[task.ChildId]
		fresh := false
		if _, seen := m.taskProgressSequences[task.TaskId]; !seen {
			fresh = true
			// A fresh subscription replays durable progress from zero. Replace any
			// transcript restored through the legacy path instead of appending it twice.
			tab.view = newChatView(m.styles, m.palette, m.root, m.home, tab.view.Width(), tab.view.Height())
			tab.startup = nil
			tab.view.AppendEntry(&Entry{Role: RoleUser, Content: task.Task})
			m.taskProgressSequences[task.TaskId] = 0
		}
		previous := tab.taskStatus
		changed := tab.applyTaskLifecycle(task.TaskId, task.Status)
		if (fresh || changed) && task.Error != "" {
			tab.view.AppendEntry(&Entry{Role: RoleSystem, Content: "Task " + task.Status + ": " + task.Error})
		}
		tab.view.SetStreaming(!tab.done)
		tab.view.rebuild()
		// Regression probe task_terminal_retire_test.go: an independent task that
		// completes while no parent turn is running must retire its own tab now,
		// matching dispatch children that retire on their own "done" event,
		// instead of lingering until the next cleanupFinishedSubAgentTabs sweep.
		// retireFinishedSubAgentTab spares restored and user-dismissed tabs, and
		// failed/stopped/interrupted tabs stay for post-mortem review.
		if task.Status == "completed" && previous != "completed" {
			m.retireFinishedSubAgentTab(task.ChildId)
		}
	}
	if progress := event.GetProgress(); progress != nil && !m.chatTabs.closed[progress.ChildId] {
		tab := m.chatTabs.tabs[progress.ChildId]
		if tab == nil || tab.taskID != progress.TaskId || progress.Sequence <= m.taskProgressSequences[progress.TaskId] {
			return msg.next
		}
		m.taskProgressSequences[progress.TaskId] = progress.Sequence
		switch progress.Kind {
		case "model_response":
			var response struct{ Blocks []struct{ Type, Text string } }
			if json.Unmarshal([]byte(progress.PayloadJson), &response) == nil {
				for _, block := range response.Blocks {
					if block.Type == "text" && block.Text != "" {
						tab.view.Apply(chatAssistantMsg{text: block.Text})
					}
				}
			}
		case "tool_call":
			var call struct {
				ID   string `json:"tool_use_id"`
				Name string `json:"tool_name"`
				Args string `json:"args"`
			}
			if json.Unmarshal([]byte(progress.PayloadJson), &call) == nil {
				tab.view.Apply(toolEntryStartMsg{id: call.ID, name: call.Name})
				tab.view.Apply(toolEntryStopMsg{id: call.ID, argsSummary: call.Args})
				tab.view.Apply(toolEntryExecStartMsg{id: call.ID})
			}
		case "tool_result":
			var result struct {
				ID      string `json:"tool_use_id"`
				Content string `json:"content"`
				IsError bool   `json:"is_error"`
			}
			if json.Unmarshal([]byte(progress.PayloadJson), &result) == nil {
				tab.view.Apply(toolEntryExecCompleteMsg{id: result.ID, summary: result.Content, isError: result.IsError})
			}
		case "note":
			var note struct {
				Text string `json:"text"`
			}
			if json.Unmarshal([]byte(progress.PayloadJson), &note) == nil && note.Text != "" {
				tab.view.AppendEntry(&Entry{Role: RoleSystem, Content: note.Text})
			}
		}
		tab.applyTaskLifecycle(tab.taskID, tab.taskStatus)
		tab.view.rebuild()
	}
	return msg.next
}
