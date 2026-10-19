package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Regression: when the foreground turn is blocked solely on a task_wait poll
// (the delegated child keeps running independently), submitting a nonempty
// prompt must cancel only the foreground wait and submit on the FIRST Enter.
// The old behavior queued the prompt behind the pending tool and demanded a
// second empty Enter to steer, leaving the session feeling dead.
func TestWaitOnlyFirstEnterSubmitsWithoutSecondPress(t *testing.T) {
	m := New(nil, false)
	m = send(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	canceled := false
	m.cancelStream = func() { canceled = true }
	m.streaming = true // pretend a turn is in flight
	m.mainChat().Apply(toolEntryStartMsg{id: "w", name: "task_wait"})
	m.input.SetValue("did it finish yet")

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)

	if !canceled {
		t.Fatal("first enter with text should cancel the wait-blocked turn")
	}
	if q := m.mainChat().Queued(); len(q) != 0 {
		t.Fatalf("wait-only submit must not queue, got %v", q)
	}
	if m.streaming {
		t.Fatal("prompt should be submitted on the first Enter, not left queued")
	}
	if got := strings.TrimSpace(m.input.Value()); got != "" {
		t.Errorf("input should clear after submitting, got %q", got)
	}
	var userTurns []string
	for _, e := range m.mainChat().Entries() {
		if e.Role == RoleUser {
			userTurns = append(userTurns, e.Content)
		}
		if e.Role == RoleSystem && e.Content == "⊘ canceled" {
			t.Error("wait-only submit should hide the internal cancellation notice")
		}
	}
	if len(userTurns) == 0 || userTurns[len(userTurns)-1] != "did it finish yet" {
		t.Fatalf("submitted prompt missing from transcript, got %v", userTurns)
	}
}

// Ordinary in-flight tools (and no in-flight tools at all) keep the existing
// queue-then-steer behavior: first Enter with text must only queue.
func TestOrdinaryForegroundWorkStillQueuesFirstEnter(t *testing.T) {
	for _, name := range []string{"bash", ""} {
		m := New(nil, false)
		m = send(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
		canceled := false
		m.cancelStream = func() { canceled = true }
		m.streaming = true
		if name != "" {
			m.mainChat().Apply(toolEntryStartMsg{id: "b", name: name})
		}
		m.input.SetValue("and then run the linter")

		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = next.(Model)

		if canceled {
			t.Fatalf("first enter with a %q tool in flight must not cancel", name)
		}
		if q := m.mainChat().Queued(); len(q) != 1 || q[0] != "and then run the linter" {
			t.Fatalf("ordinary foreground work should queue on first enter, got %v", q)
		}
		if !m.streaming {
			t.Fatal("queuing must not end the in-flight turn")
		}
	}
}

// The task_wait row also belongs to the child-turn pane. An in-progress
// task_wait there must count as wait-blocked for the same first-Enter rule.
func TestWaitBlockedChildToolCountsForFirstEnter(t *testing.T) {
	m := New(nil, false)
	m = send(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.applySubAgentEvent(subAgentEventMsg{id: "child-1", title: "sub", kind: "started"})
	tab := m.chatTabs.tabs["child-1"]
	if tab == nil {
		t.Fatal("expected child tab")
	}
	tab.view.Apply(toolEntryStartMsg{id: "w", name: "task_wait"})
	canceled := false
	m.cancelStream = func() { canceled = true }
	m.streaming = true
	m.input.SetValue("steer the child")

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if !canceled || m.streaming {
		t.Fatal("first enter with text should cancel the foreground wait and submit")
	}
	if q := m.mainChat().Queued(); len(q) != 0 {
		t.Fatalf("wait-only submit must not queue, got %v", q)
	}
}

// Quiet-mode leak: when a wait-blocked turn is canceled, the stale task_wait
// tool row is flipped to an interrupted "error". In quiet mode that synthetic
// interruption is not a genuine task error — it must stay hidden — while full
// debug output keeps showing every detail.
func TestInterruptedWaitRowHiddenQuietVisibleInDebug(t *testing.T) {
	c := newTestChatView(100, 30)
	c.setTaskVisibility(false, true) // quiet main chat
	c.Apply(toolEntryStartMsg{id: "w", name: "task_wait"})
	c.resolveStaleInProgressTools()
	c.rebuild()

	quiet := ansi.Strip(chatLayoutContent(c))
	if strings.Contains(quiet, "task_wait") {
		t.Fatalf("canceled task_wait row leaked into quiet mode: %q", quiet)
	}

	c.setTaskVisibility(true, true) // all visible debug
	c.rebuild()
	debug := ansi.Strip(chatLayoutContent(c))
	if !strings.Contains(debug, "task_wait") || !strings.Contains(debug, "interrupted") {
		t.Fatalf("debug output must keep the interrupted task_wait row, got %q", debug)
	}
}
