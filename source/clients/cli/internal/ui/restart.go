package ui

import (
	"fmt"
	"io"
	"os"

	"cercano/source/clients/cli/internal/tuirestart"
	tea "charm.land/bubbletea/v2"
)

// TUIUpdateAppliedMsg is emitted by trusted update integration only after the
// client is installed. No producer is wired until that integration is ready.
type TUIUpdateAppliedMsg struct{ Target string }
type tuiRestartFailedMsg struct{ err error }

func (m Model) restartNotice(text string) (Model, tea.Cmd) {
	m.mainChat().AppendNotice(&Entry{Role: RoleSystem, Content: text})
	m.refreshViewport()
	return m, nil
}
func (m Model) requestTUIRestart() (Model, tea.Cmd) {
	if m.pendingConfirm != nil {
		return m.restartNotice("Finish the pending decision before restarting this TUI.")
	}
	if m.restartBusy() {
		return m.restartNotice("This terminal has active work. Finish or interrupt it before /restart. The shared agent will not be restarted.")
	}
	title := "Restart this TUI now?"
	if m.tuiUpdateAvailable {
		title = "An update to the TUI has been applied. Restart this TUI now?"
	}
	detail := []string{"Only this terminal client restarts. The shared agent and other terminals stay running.", "Your conversation, working directory and unsent draft will be carried over. Other temporary views may need to be reopened."}
	if !tuirestart.Supported() {
		detail = []string{"Windows TUI restart is manual for now. Save your unsent draft, exit this client with /quit, then reopen Cercano in the same directory.", "Use /history to resume this conversation. Do not stop the shared agent or close other terminals."}
	}
	m.pendingConfirm = &confirmRequest{title: title, details: detail, hints: "y restart · n later · d details", onYes: func(m Model) (Model, tea.Cmd) {
		m.pendingConfirm = nil
		if !tuirestart.Supported() {
			return m.restartNotice("Manual restart: save your draft, /quit this TUI, reopen Cercano here, then use /history. The agent stays running.")
		}
		if m.restartBusy() {
			return m.restartNotice("Active work started while confirmation was open; restart refused without changing this TUI.")
		}
		target := m.tuiRestartTarget
		if target == "" {
			var e error
			target, e = tuirestart.Target()
			if e != nil {
				return m.restartNotice("TUI restart unavailable: " + e.Error())
			}
		}
		dir := m.workDirOverride
		if dir == "" {
			dir, _ = os.Getwd()
		}
		snapshot := tuirestart.State{ConversationID: m.convID, WorkingDirectory: dir, Draft: m.input.Value()}
		return m, tea.Exec(&tuiRestartExec{target: target, state: snapshot}, func(e error) tea.Msg { return tuiRestartFailedMsg{err: e} })
	}, onNo: func(m Model) (Model, tea.Cmd) {
		m.pendingConfirm = nil
		return m.restartNotice("TUI restart deferred. Use /restart when ready.")
	}, extras: map[string]func(Model) (Model, tea.Cmd){"d": func(m Model) (Model, tea.Cmd) {
		return m.restartNotice("/restart reloads only this client. It does not call /restart-agent. No command or interrupted work is automatically replayed. On Windows, restart manually; on macOS/Linux the installed client must support a safe handoff.")
	}}}
	return m, nil
}

type tuiRestartExec struct {
	target string
	state  tuirestart.State
}

func (c *tuiRestartExec) Run() error        { return tuirestart.Replace(c.target, c.state) }
func (*tuiRestartExec) SetStdin(io.Reader)  {}
func (*tuiRestartExec) SetStdout(io.Writer) {}
func (*tuiRestartExec) SetStderr(io.Writer) {}

// RestoreTUI resumes a saved conversation before first paint. Resume failure
// keeps the draft visible and reports the ID; it never submits/replays text.
func (m Model) RestoreTUI(s tuirestart.State) Model {
	if s.ConversationID != "" {
		m, m.restartInit = m.applyResume(s.ConversationID)
		if m.convID != s.ConversationID {
			m, _ = m.restartNotice(fmt.Sprintf("Could not resume conversation %s; use /history. Your draft is preserved.", s.ConversationID))
		}
	}
	m.workDirOverride = s.WorkingDirectory
	if m.wdRef != nil {
		m.wdRef.dir = s.WorkingDirectory
	}
	m.input.SetValue(s.Draft)
	return m
}

func (m Model) restartBusy() bool {
	return m.streaming || m.resumeHydrating || m.compacting || m.content != nil || m.authLogin != nil || m.composeToolUseID != "" || len(m.confirmQueue) != 0
}
