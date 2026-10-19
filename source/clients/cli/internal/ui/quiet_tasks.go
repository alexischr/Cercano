package ui

// Visibility is presentation only: retain raw entries and full tool results.
// In particular, a failed poll and an explicit stop remain actionable.
func (c *chatView) entryVisible(e *Entry) bool {
	// Host continuation envelopes are orchestration input, not chat prose.
	// Dev mode exposes diagnostic tool activity, but must not expose these
	// envelopes (including their instructions) when history is restored.
	if e.Bookkeeping {
		return false
	}
	if c.showBookkeeping {
		return true
	}
	if c.quietTaskActivity && e.TaskActivity {
		return false
	}
	if c.quietTaskActivity && e.Tool != nil && e.Tool.Status != ToolStatusError {
		switch e.Tool.ToolName {
		case "task_list", "task_inspect", "task_wait":
			return false
		}
	}
	return true
}

func (c *chatView) setTaskVisibility(debug, main bool) {
	if c.showBookkeeping == debug && c.quietTaskActivity == main {
		return
	}
	c.showBookkeeping, c.quietTaskActivity = debug, main
	if c.focusedToolIdx >= 0 && c.focusedToolIdx < len(c.entries) && !c.entryVisible(c.entries[c.focusedToolIdx]) {
		c.ExitToolNav()
	}
	c.markTranscriptDirty()
}

func routineTaskTool(name string) bool {
	switch name {
	case "task_list", "task_inspect", "task_wait":
		return true
	}
	return false
}
