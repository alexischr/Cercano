// Package ui — claude_login_modal.go: the modal that drives Claude
// subscription sign-in from the settings page's Cloud Providers section.
//
// The Claude sibling of chatgpt_login_modal.go. It differs in the mechanism:
// the agent runs a PKCE loopback (not device-code). The first frame carries two
// URLs for the same grant. The loopback one is opened in a browser on this
// machine when one can be launched; approving it lets the agent's loopback
// catch the redirect. The manual one is available through the copy shortcut for a
// browser anywhere (the SSH case); approving it shows a code, which the user
// pastes into the modal and the modal submits under the frame's login id.
// State machine and streaming plumbing mirror the ChatGPT modal.
package ui

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"cercano/source/clients/cli/internal/theme"
	"cercano/source/server/pkg/agentclient"
	"github.com/charmbracelet/x/ansi"
	"google.golang.org/grpc/status"
)

// claudeLoginState is the modal's small state machine.
type claudeLoginState int

const (
	// claudeLoginWaiting shows the authorize URL and a "waiting for approval…"
	// prompt while the loopback flow blocks on the browser redirect.
	claudeLoginWaiting claudeLoginState = iota
	// claudeLoginDone means the terminal frame reported ok=true.
	claudeLoginDone
	// claudeLoginFailed means the RPC errored or the terminal frame carried
	// ok=false (declined, timed out, network).
	claudeLoginFailed
)

// claudeLoginModal owns the modal's transient state. Kept separate from Model
// so tests can exercise transitions without wiring the full TUI.
type claudeLoginModal struct {
	state claudeLoginState
	// authorizeURL is populated from the first stream frame — the loopback
	// URL opened in a browser on this machine.
	authorizeURL string
	// manualURL and loginID come from the same frame. manualURL is retained for
	// copying to any browser; the code it yields is submitted under loginID.
	// Both are empty against an older agent, which only does the loopback.
	manualURL string
	loginID   string
	// code is the pasted (or typed) code awaiting Enter. submitting is set
	// while a submit is in flight and stays set once accepted, until the
	// stream's terminal frame lands. codeErr explains the last rejection;
	// notice is a transient hint (e.g. the URL was copied).
	code       string
	submitting bool
	codeErr    string
	notice     string
	// profile + model are what we asked the agent to create; shown in the copy.
	profile string
	model   string
	// errMsg is set in the failed state.
	errMsg string
	// cancel tears down the sign-in RPC context — called on Esc during
	// waiting to abort, nil once the stream has settled.
	cancel context.CancelFunc
	// browserOpened guards the one-shot auto-open so re-draws / extra frames
	// don't spawn a browser tab twice.
	browserOpened  bool
	browserFocused bool
}

// newClaudeLoginModal opens the modal in the waiting state for a given profile
// + model. The caller batches startClaudeLoginCmd alongside.
func newClaudeLoginModal(profile, model string) *claudeLoginModal {
	return &claudeLoginModal{state: claudeLoginWaiting, profile: profile, model: model}
}

// setURL records the sign-in URLs and login id from the first frame.
func (mo *claudeLoginModal) setURL(authorizeURL, manualURL, loginID string) {
	mo.authorizeURL, mo.manualURL, mo.loginID = authorizeURL, manualURL, loginID
}

// autoOpenBrowser opens the loopback URL once, the moment it arrives, so the
// user lands on it without hunting for the link — but only where a browser can
// show up on this machine. Over SSH or without a display the attempt would
// fail silently or, worse, start a text-mode browser in this terminal; the
// copy-link shortcut covers those cases.
func (mo *claudeLoginModal) autoOpenBrowser() tea.Cmd {
	if mo.browserOpened || mo.authorizeURL == "" || !browserLaunchable(os.Getenv, runtime.GOOS) {
		return nil
	}
	mo.browserOpened = true
	return openBrowserCmd(mo.authorizeURL)
}

// acceptsCode reports whether the modal is taking a pasted code: the agent
// offered a manual URL and no submit is in flight.
func (mo *claudeLoginModal) acceptsCode() bool {
	return mo.state == claudeLoginWaiting && mo.loginID != "" && !mo.submitting
}

// shownURL is the URL used by the explicit open/copy actions: the manual one when the
// agent offers it (it works from any browser), else the loopback one.
func (mo *claudeLoginModal) shownURL() string {
	if mo.manualURL != "" {
		return mo.manualURL
	}
	return mo.authorizeURL
}

// pasteCode appends pasted text to the code. Codes are single tokens, so ANSI
// and all whitespace are dropped — terminals often add a trailing newline.
func (mo *claudeLoginModal) pasteCode(text string) {
	if !mo.acceptsCode() {
		return
	}
	mo.browserFocused = false
	clean := strings.Join(strings.Fields(ansi.Strip(text)), "")
	if clean != "" {
		mo.code += clean
		mo.codeErr = ""
	}
}

// setDone transitions to the success state.
func (mo *claudeLoginModal) setDone() {
	mo.state = claudeLoginDone
	if mo.cancel != nil {
		mo.cancel()
	}
	mo.cancel = nil
}

// setFailed transitions to the failure state carrying a reason.
func (mo *claudeLoginModal) setFailed(msg string) {
	mo.state = claudeLoginFailed
	mo.errMsg = msg
	if mo.cancel != nil {
		mo.cancel()
	}
	mo.cancel = nil
}

// --- streaming plumbing ---

// openClaudeLoginModalMsg is emitted from the settings page when the user
// activates the "sign in with Claude" button. Carries the profile name +
// model to create and whether to activate it on success.
type openClaudeLoginModalMsg struct {
	createOnly bool
	profile    string
	model      string
	setActive  bool
}

// claudeLoginStartedMsg carries the opened stream (or the open error).
type claudeLoginStartedMsg struct {
	attempt uint64
	cancel  context.CancelFunc
	ch      <-chan agentclient.ClaudeLoginMsg
	err     error
}

// claudeLoginFrameMsg carries one drained frame plus the channel to keep
// draining (nil once the stream is exhausted).
type claudeLoginFrameMsg struct {
	attempt uint64
	frame   agentclient.ClaudeLoginMsg
	ch      <-chan agentclient.ClaudeLoginMsg
}

// startClaudeLoginCmd opens the StartClaudeLogin streaming RPC under a
// cancellable context and returns the stream (or error) for the drain loop.
func startClaudeLoginCmd(ag *agentclient.Client, profile, model string, setActive bool, attempt uint64, createOnly ...bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		ch, err := ag.StartClaudeLogin(ctx, profile, model, setActive, createOnly...)
		if err != nil {
			cancel()
			return claudeLoginStartedMsg{attempt: attempt, err: err}
		}
		return claudeLoginStartedMsg{attempt: attempt, cancel: cancel, ch: ch}
	}
}

// drainClaudeLoginCmd reads one frame from the stream channel and re-arms. A
// closed channel without a terminal frame is reported as a failure so the
// modal never hangs in the waiting state.
func drainClaudeLoginCmd(ch <-chan agentclient.ClaudeLoginMsg, attempts ...uint64) tea.Cmd {
	var attempt uint64
	if len(attempts) > 0 {
		attempt = attempts[0]
	}
	return func() tea.Msg {
		frame, ok := <-ch
		if !ok {
			return claudeLoginFrameMsg{attempt: attempt,
				frame: agentclient.ClaudeLoginMsg{Done: true, Ok: false, Error: "sign-in stream closed unexpectedly"},
			}
		}
		return claudeLoginFrameMsg{attempt: attempt, frame: frame, ch: ch}
	}
}

// claudeLoginCodeResultMsg reports a submitted code's outcome. loginID ties it
// to the modal that sent it; a result for any other login is stale.
type claudeLoginCodeResultMsg struct {
	loginID string
	err     error
}

// submitClaudeLoginCodeCmd submits a pasted code under the sign-in's login id.
func submitClaudeLoginCodeCmd(ag *agentclient.Client, loginID, code string) tea.Cmd {
	return func() tea.Msg {
		if ag == nil {
			return claudeLoginCodeResultMsg{loginID: loginID, err: errors.New("not connected to the agent")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return claudeLoginCodeResultMsg{loginID: loginID, err: ag.SubmitClaudeLoginCode(ctx, loginID, code)}
	}
}

// applyCodeResult folds a submit outcome into the modal. Acceptance keeps the
// modal busy until the stream's terminal frame; a rejection clears the code so
// the next paste starts fresh.
func (mo *claudeLoginModal) applyCodeResult(msg claudeLoginCodeResultMsg) {
	if mo.state != claudeLoginWaiting || msg.loginID != mo.loginID {
		return
	}
	if msg.err == nil {
		return
	}
	mo.submitting = false
	mo.code = ""
	if st, ok := status.FromError(msg.err); ok {
		mo.codeErr = st.Message()
	} else {
		mo.codeErr = msg.err.Error()
	}
}

// --- rendering ---

// modalDim keeps sign-in compact regardless of authorization URL length.
func (mo *claudeLoginModal) modalDim(frameW, frameH int) (int, int) {
	boxW := min(64, max(10, frameW-4))
	boxH := len(mo.lines(theme.Styles{}, boxW-4)) + 2
	return boxW, boxH
}

// View renders the modal as a bordered box sized to the frame.
func (mo *claudeLoginModal) View(styles theme.Styles, palette theme.Palette, frameW, frameH int) string {
	boxW, boxH := mo.modalDim(frameW, frameH)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(palette.Border).
		Padding(0, 1).
		Width(boxW - 2).
		Height(boxH - 2)
	inner := boxW - 4
	return box.Render(strings.Join(mo.lines(styles, inner), "\n"))
}

// lines lays out the modal content for an inner width of w.
func (mo *claudeLoginModal) lines(styles theme.Styles, w int) []string {
	out := []string{styles.Primary.Bold(true).Render(mo.title()), ""}
	out = append(out, mo.renderBody(styles, w)...)
	out = append(out, "")
	return append(out, strings.Split(ansi.Wrap(mo.renderActions(styles), w, ""), "\n")...)
}

func (mo *claudeLoginModal) title() string {
	switch mo.state {
	case claudeLoginDone:
		return "Claude sign-in complete"
	case claudeLoginFailed:
		return "Claude sign-in failed"
	}
	return "Sign in with Claude"
}

func (mo *claudeLoginModal) renderBody(styles theme.Styles, w int) []string {
	wrap := func(st lipgloss.Style, text string) []string {
		return strings.Split(st.Render(ansi.Wrap(text, w, "")), "\n")
	}
	switch mo.state {
	case claudeLoginWaiting:
		if mo.authorizeURL == "" && mo.manualURL == "" {
			return []string{styles.Muted.Render("Starting sign-in…")}
		}
		intro := "Approve the sign-in in your browser."
		if mo.browserOpened {
			intro = "Approve the sign-in in the browser that just opened."
		}
		out := wrap(styles.Muted, intro)
		buttonStyle := styles.Primary
		if mo.browserFocused {
			buttonStyle = buttonStyle.Reverse(true)
		}
		out = append(out, "", buttonStyle.Render("[ Open browser ]"), "")
		if mo.manualURL != "" {
			out = append(out, wrap(styles.Muted, "Or copy the link to another device, then paste the code here:")...)
			out = append(out, mo.renderCodeField(styles, w))
		}

		switch {
		case mo.codeErr != "":
			out = append(out, wrap(styles.Error, mo.codeErr)...)
		case mo.notice != "":
			out = append(out, wrap(styles.Muted, mo.notice)...)
		}
		return out
	case claudeLoginDone:
		msg := "Signed in. Profile " + strconv.Quote(mo.profile) + " is ready"
		if mo.model != "" {
			msg += " on " + mo.model
		}
		msg += "."
		return wrap(styles.Muted, msg)
	case claudeLoginFailed:
		reason := mo.errMsg
		if reason == "" {
			reason = "unknown error"
		}
		return wrap(styles.Error, reason)
	}
	return nil
}

// renderCodeField draws the one-line code input, keeping the tail of a long
// code in view.
func (mo *claudeLoginModal) renderCodeField(styles theme.Styles, w int) string {
	const prompt = "> "
	if mo.submitting {
		return styles.Muted.Render(prompt + "Checking code…")
	}
	code := mo.code
	if room := w - len(prompt) - 2; room > 1 && len(code) > room {
		code = "…" + code[len(code)-room+1:]
	}
	return styles.Primary.Render(prompt) + code + styles.Muted.Render("█")
}

func (mo *claudeLoginModal) renderActions(styles theme.Styles) string {
	switch mo.state {
	case claudeLoginWaiting:
		if mo.loginID != "" {
			return styles.Muted.Render("[Tab] switch focus  [Enter] activate\n[Ctrl+Y] copy URL  [Ctrl+O] open browser  [Esc] cancel")
		}
		return styles.Muted.Render("[o] open browser  [Esc] Cancel  (waiting for approval…)")
	case claudeLoginDone:
		return styles.Success.Render("✓ [any key] to close")
	case claudeLoginFailed:
		return styles.Muted.Render("[Esc] Close")
	}
	return ""
}

// handleClaudeLoginModalKey routes a keypress while the sign-in modal is open.
// In the waiting state, Esc cancels the in-flight stream and closes the modal,
// and typed text goes to the code field (so shortcuts use Ctrl); once settled
// (done/failed) any key dismisses it.
func (m Model) handleClaudeLoginModalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	mo := m.claudeLoginModal
	if mo.state == claudeLoginWaiting {
		switch msg.String() {
		case "esc":
			if mo.cancel != nil {
				mo.cancel()
			}
			m.claudeLoginModal = nil
			return m, nil
		case "tab", "shift+tab":
			mo.browserFocused = !mo.browserFocused
			return m, nil
		case "enter":
			if mo.browserFocused {
				return m, openBrowserCmd(mo.shownURL())
			}
		case "ctrl+o":
			if mo.authorizeURL != "" {
				return m, openBrowserCmd(mo.shownURL())
			}
			return m, nil
		case "ctrl+y":
			if url := mo.shownURL(); url != "" {
				mo.notice = "Copied the URL to the clipboard (needs a terminal with OSC 52 clipboard support)."
				return m, selectionClipboardCmd(url)
			}
			return m, nil
		}
		if mo.loginID == "" {
			// Older agent: no code field, keep the bare [o] shortcut.
			if msg.String() == "o" && mo.authorizeURL != "" {
				return m, openBrowserCmd(mo.shownURL())
			}
			return m, nil
		}
		if mo.browserFocused {
			return m, nil
		}
		if !mo.acceptsCode() {
			return m, nil
		}
		switch msg.String() {
		case "enter":
			code := strings.TrimSpace(mo.code)
			if code == "" {
				return m, nil
			}
			mo.submitting, mo.codeErr, mo.notice = true, "", ""
			return m, submitClaudeLoginCodeCmd(m.agent, mo.loginID, code)
		case "backspace":
			if r := []rune(mo.code); len(r) > 0 {
				mo.code = string(r[:len(r)-1])
			}
			return m, nil
		case "ctrl+u":
			mo.code = ""
			return m, nil
		}
		if t := msg.Text; t != "" && !strings.ContainsAny(t, " \t\r\n") {
			mo.code += t
			mo.codeErr = ""
		}
		return m, nil
	}
	m.claudeLoginModal = nil
	return m, nil
}

// handleClaudeLoginModalMouse uses the same layout as rendering and consumes
// clicks outside the action so they cannot reach the settings underneath.
func (m Model) handleClaudeLoginModalMouse(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	mo := m.claudeLoginModal
	if mo.state != claudeLoginWaiting || mo.shownURL() == "" || msg.Button != tea.MouseLeft {
		return m, nil
	}
	w, h := mo.modalDim(m.width, m.height)
	x, y := max(0, (m.width-w)/2)+2, max(0, (m.height-h)/2)+1
	for row, line := range mo.lines(theme.Styles{}, w-4) {
		text := ansi.Strip(line)
		if text == "[ Open browser ]" && msg.Y == y+row && msg.X >= x && msg.X < x+ansi.StringWidth(text) {
			mo.browserFocused = true
			return m, openBrowserCmd(mo.shownURL())
		}
	}
	return m, nil
}
