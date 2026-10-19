package ui

import (
	"cercano/source/server/pkg/agentclient"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestHostCompletionEnvelopeNotRenderedOnResume(t *testing.T) {
	c := newTestChatView(100, 30)
	c.setTaskVisibility(true, true) // Resumed dev-mode conversations must stay readable.
	c.SetEntries(resumeEntries([]agentclient.PersistedTurn{
		{Role: "system", Content: "<host:task-completions>\n[]\n</host:task-completions>\nDelegated tasks have finished."},
		{Role: "assistant", Content: "The work is complete."},
	}, 0))
	text := ansi.Strip(chatLayoutContent(c))
	if strings.Contains(text, "host:task-completions") || strings.Contains(text, "Delegated tasks have finished") {
		t.Fatal("internal completion envelope leaked into transcript")
	}
	if !strings.Contains(text, "The work is complete.") {
		t.Fatal("meaningful reply hidden")
	}
}

func TestRoutineTaskActivityVisibility(t *testing.T) {
	for _, name := range []string{"task_list", "task_inspect", "task_wait"} {
		t.Run(name, func(t *testing.T) {
			c := newTestChatView(100, 30)
			c.setTaskVisibility(false, true)
			c.Apply(toolEntryStartMsg{id: "poll", name: name})
			c.Apply(chatProgressMsg{note: "running " + name})
			c.Apply(toolEntryExecCompleteMsg{id: "poll", summary: "retained evidence"})
			c.Apply(toolEntryStartMsg{id: "stop", name: "task_stop"})
			c.Apply(toolEntryExecCompleteMsg{id: "stop", summary: "stop acknowledged"})
			c.rebuild()
			text := ansi.Strip(chatLayoutContent(c))
			if strings.Contains(text, name) {
				t.Fatalf("normal poll visible: %s", text)
			}
			if !strings.Contains(text, "task_stop") {
				t.Fatalf("stop hidden: %s", text)
			}
			if indices := c.toolEntryIndices(); len(indices) != 1 {
				t.Fatalf("hidden poll is keyboard navigable: %v", indices)
			}
			c.setTaskVisibility(true, true)
			c.rebuild()
			// Dev mode is still the main chat's ordinary presentation: routine
			// successful polls and their running-progress notes stay hidden
			// there too (interrupted/stale rows and genuine failures remain).
			if strings.Contains(ansi.Strip(chatLayoutContent(c)), name) {
				t.Fatal("debug leaked routine poll")
			}
			c.setTaskVisibility(false, true)
			c.Apply(toolEntryExecCompleteMsg{id: "poll", isError: true, summary: "poll failed"})
			c.rebuild()
			if !strings.Contains(ansi.Strip(chatLayoutContent(c)), name) {
				t.Fatal("poll error hidden")
			}
			c.setTaskVisibility(false, false)
			c.rebuild()
			if !strings.Contains(ansi.Strip(chatLayoutContent(c)), name) {
				t.Fatal("child evidence hidden")
			}
		})
	}
}

// Reproduces the reported noise: a dev-mode main-chat turn whose assistant
// prose announces a retry check, then polls (task_inspect success, "running
// task_inspect" progress note, task_wait). The dev-mode presentation override
// must not leak the routine poll rows or their running progress into the
// transcript; the assistant prose and retained evidence must survive.
func TestDevModeMainChatHidesRoutinePollNoise(t *testing.T) {
	c := newTestChatView(100, 30)
	c.setTaskVisibility(true, true) // /d dev mode, ordinary main chat
	c.Apply(chatAssistantDeltaMsg{token: "I'll check the retry's status and retained results."})
	c.Apply(chatProgressMsg{note: "running task_inspect"})
	c.Apply(toolEntryStartMsg{id: "i", name: "task_inspect"})
	c.Apply(toolEntryExecCompleteMsg{id: "i", summary: "retained evidence"})
	c.Apply(toolEntryStartMsg{id: "w", name: "task_wait"})
	c.Apply(toolEntryExecCompleteMsg{id: "w", summary: "completed"})
	c.rebuild()

	text := ansi.Strip(chatLayoutContent(c))
	if !strings.Contains(text, "I'll check the retry's status and retained results.") {
		t.Fatal("meaningful assistant prose must stay visible")
	}
	for _, noise := range []string{"task_inspect", "running task_inspect", "task_wait", "retained evidence"} {
		if strings.Contains(text, noise) {
			t.Fatalf("routine poll noise leaked into dev-mode main chat: %q", noise)
		}
	}
	// Presentation-only hiding: raw entries and results are retained.
	for _, id := range []string{"i", "w"} {
		if c.findToolEntry(id) == nil {
			t.Fatalf("raw tool evidence for %q dropped", id)
		}
	}
}

func TestHostMetadataVisibilityDoesNotSuppressUserText(t *testing.T) {
	c := newTestChatView(100, 30)
	const marker = "<host:task-completions>\n[]\n</host:task-completions>"
	c.SetEntries(resumeEntries([]agentclient.PersistedTurn{{Role: "user", Content: marker}}, 0))
	if !strings.Contains(ansi.Strip(chatLayoutContent(c)), "host:task-completions") {
		t.Fatal("user text suppressed")
	}
	c.setTaskVisibility(true, true)
	c.SetEntries(resumeEntries([]agentclient.PersistedTurn{{Role: "system", Content: marker}}, 0))
	if strings.Contains(ansi.Strip(chatLayoutContent(c)), "host:task-completions") {
		t.Fatal("debug mode leaked host metadata")
	}
}
