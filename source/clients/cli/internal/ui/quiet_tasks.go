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
	if c.quietTaskActivity {
		// The main chat's ordinary presentation — dev mode included — hides
		// routine polling noise:
		//   - successful/in-progress or synthetically interrupted
		//     task_list/task_inspect/task_wait rows
		//     (their retained results are still in the entry data and on the
		//     child tab / inspection paths), and
		//   - "running <task tool>" progress notes.
		if e.Tool == nil {
			if e.TaskActivity {
				return false
			}
		} else if e.Tool.Status != ToolStatusError && routineTaskTool(e.Tool.ToolName) {
			return false
		}
	}
	// Ordinary main chat (including dev mode) additionally hides interrupted
	// routine rows via their TaskActivity mark. These are synthetic observer
	// cancellations, not task failures. Raw entries and non-main diagnostic
	// views retain the interruption; genuine tool errors have no such mark.
	if c.quietTaskActivity && e.TaskActivity {
		return false
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
