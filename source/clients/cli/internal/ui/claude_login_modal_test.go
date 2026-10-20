package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"cercano/source/clients/cli/internal/theme"
	"cercano/source/server/pkg/agentclient"
)

func TestClaudeLoginModalTransitions(t *testing.T) {
	mo := newClaudeLoginModal("claude", "claude-sonnet-5")
	if mo.state != claudeLoginWaiting {
		t.Fatalf("initial state = %v, want waiting", mo.state)
	}
	mo.setURL("https://claude.ai/oauth/authorize?x=1", "", "")
	if mo.authorizeURL == "" {
		t.Error("setURL did not record the authorize URL")
	}
	mo.setDone()
	if mo.state != claudeLoginDone {
		t.Errorf("after setDone state = %v, want done", mo.state)
	}
	if mo.cancel != nil {
		t.Error("setDone should clear cancel")
	}
}

func TestClaudeLoginModalSetFailed(t *testing.T) {
	mo := newClaudeLoginModal("claude", "")
	mo.setFailed("declined by user")
	if mo.state != claudeLoginFailed || mo.errMsg != "declined by user" {
		t.Errorf("setFailed → state=%v msg=%q, want failed/declined", mo.state, mo.errMsg)
	}
}

// A stream channel that closes without a terminal frame must synthesize a
// failure frame so the modal never hangs in the waiting state.
func TestDrainClaudeLoginClosedChannel(t *testing.T) {
	ch := make(chan agentclient.ClaudeLoginMsg)
	close(ch)
	msg := drainClaudeLoginCmd(ch)()
	frame, ok := msg.(claudeLoginFrameMsg)
	if !ok {
		t.Fatalf("want claudeLoginFrameMsg, got %T", msg)
	}
	if !frame.frame.Done || frame.frame.Ok {
		t.Errorf("closed channel should yield a terminal failure frame, got %+v", frame.frame)
	}
}

func TestDrainClaudeLoginDeliversFrame(t *testing.T) {
	ch := make(chan agentclient.ClaudeLoginMsg, 1)
	ch <- agentclient.ClaudeLoginMsg{AuthorizeURL: "https://claude.ai/oauth/authorize?y=2"}
	msg := drainClaudeLoginCmd(ch)()
	frame := msg.(claudeLoginFrameMsg)
	if frame.frame.AuthorizeURL == "" {
		t.Error("frame did not carry the authorize URL through")
	}
	if frame.ch == nil {
		t.Error("drain should re-arm with the channel for the next frame")
	}
}

const testManualURL = "https://claude.ai/oauth/authorize?code=true&client_id=9d1c250a-e61b-44d9-88ed-5944d1962f5e&response_type=code&redirect_uri=https%3A%2F%2Fplatform.claude.com%2Foauth%2Fcode%2Fcallback&scope=org%3Acreate_api_key+user%3Aprofile+user%3Ainference&code_challenge=abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG&code_challenge_method=S256&state=stateabcdefghijklmnopqrstuvwxyz0123456789"

func waitingModalWithCode() *claudeLoginModal {
	mo := newClaudeLoginModal("claude", "")
	mo.setURL("http://localhost:1234/callback-flow", testManualURL, "login-1")
	return mo
}

func keyText(text string) tea.KeyPressMsg {
	r := []rune(text)
	return tea.KeyPressMsg{Code: r[0], Text: text}
}

func TestClaudeLoginModalCompactBrowserAction(t *testing.T) {
	pal := theme.Cracker()
	mo := waitingModalWithCode()
	out := ansi.Strip(mo.View(theme.NewStyles(pal), pal, 100, 40))
	if strings.Contains(out, "code_challenge") || !strings.Contains(out, "[ Open browser ]") {
		t.Fatalf("expected compact action, got:\n%s", out)
	}
	if w, _ := mo.modalDim(100, 40); w > 64 {
		t.Fatalf("modal width = %d", w)
	}
	m := New(nil, false)
	m.claudeLoginModal = mo
	updated, _ := m.handleClaudeLoginModalKey(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(Model)
	_, cmd := m.handleClaudeLoginModalKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || mo.submitting {
		t.Fatal("focused browser action did not open browser")
	}
}

func TestClaudeLoginModalOlderAgentKeepsLoopbackOnlyCopy(t *testing.T) {
	pal := theme.Cracker()
	mo := newClaudeLoginModal("claude", "")
	mo.setURL("https://claude.ai/oauth/authorize?x=1", "", "")
	out := ansi.Strip(mo.View(theme.NewStyles(pal), pal, 80, 24))
	if strings.Contains(out, "paste the code") || !strings.Contains(out, "[o] open browser") {
		t.Errorf("older agent modal should not offer a code field:\n%s", out)
	}
}

func TestClaudeLoginModalPasteStripsWhitespace(t *testing.T) {
	mo := waitingModalWithCode()
	mo.codeErr = "previous"
	mo.pasteCode(" abc#def \r\n")
	if mo.code != "abc#def" || mo.codeErr != "" {
		t.Fatalf("code=%q err=%q", mo.code, mo.codeErr)
	}
	older := newClaudeLoginModal("claude", "")
	older.setURL("https://x", "", "")
	older.pasteCode("abc")
	if older.code != "" {
		t.Error("modal without a login id accepted a code")
	}
}

func TestClaudeLoginModalTypingAndSubmit(t *testing.T) {
	m := New(nil, false)
	m.claudeLoginModal = waitingModalWithCode()
	// Plain letters are code text, not shortcuts, once the field exists.
	for _, k := range []string{"o", "c", "x"} {
		updated, cmd := m.handleClaudeLoginModalKey(keyText(k))
		m = updated.(Model)
		if cmd != nil {
			t.Fatalf("typing %q produced a command", k)
		}
	}
	updated, _ := m.handleClaudeLoginModalKey(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = updated.(Model)
	if m.claudeLoginModal.code != "oc" {
		t.Fatalf("code=%q, want oc", m.claudeLoginModal.code)
	}
	updated, cmd := m.handleClaudeLoginModalKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil || !m.claudeLoginModal.submitting {
		t.Fatal("enter did not submit the code")
	}
	if _, cmd := m.handleClaudeLoginModalKey(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("enter resubmitted while a submit was in flight")
	}
}

func TestClaudeLoginModalRejectedCodeCanBeRetried(t *testing.T) {
	mo := waitingModalWithCode()
	mo.code, mo.submitting = "bad", true
	mo.applyCodeResult(claudeLoginCodeResultMsg{loginID: "other", err: errors.New("stale")})
	if !mo.submitting {
		t.Fatal("stale result applied")
	}
	mo.applyCodeResult(claudeLoginCodeResultMsg{loginID: "login-1", err: errors.New("That isn't a code for this sign-in.")})
	if mo.submitting || mo.code != "" || !strings.Contains(mo.codeErr, "isn't a code") || !mo.acceptsCode() {
		t.Fatalf("after rejection: %+v", mo)
	}
	mo.code, mo.submitting = "good", true
	mo.applyCodeResult(claudeLoginCodeResultMsg{loginID: "login-1"})
	if !mo.submitting {
		t.Fatal("accepted code should keep the modal busy until the terminal frame")
	}
}

// A paste while the modal is open goes to the code field even when a content
// page or a confirmation gate would otherwise claim it.
func TestPasteRoutesToClaudeLoginModal(t *testing.T) {
	m := New(nil, false)
	m.claudeLoginModal = waitingModalWithCode()
	m.pendingConfirm = &confirmRequest{}
	updated, _ := m.Update(tea.PasteMsg{Content: "pasted#state\n"})
	if got := updated.(Model).claudeLoginModal.code; got != "pasted#state" {
		t.Fatalf("modal code=%q", got)
	}
}

func TestBrowserLaunchable(t *testing.T) {
	env := func(kv ...string) func(string) string {
		m := map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return func(k string) string { return m[k] }
	}
	cases := []struct {
		name   string
		getenv func(string) string
		goos   string
		want   bool
	}{
		{"linux desktop", env("DISPLAY", ":0"), "linux", true},
		{"linux wayland", env("WAYLAND_DISPLAY", "wayland-0"), "linux", true},
		{"linux ssh headless", env("SSH_CONNECTION", "1 2 3 4"), "linux", false},
		{"linux ssh with X forwarding", env("SSH_CONNECTION", "1 2 3 4", "DISPLAY", "localhost:10.0"), "linux", true},
		{"linux console", env(), "linux", false},
		{"mac local", env(), "darwin", true},
		{"mac over ssh", env("SSH_TTY", "/dev/ttys001"), "darwin", false},
	}
	for _, c := range cases {
		if got := browserLaunchable(c.getenv, c.goos); got != c.want {
			t.Errorf("%s: browserLaunchable=%v, want %v", c.name, got, c.want)
		}
	}
}

func TestClaudeLoginAutoOpenSkippedWhenHeadless(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "1 2 3 4")
	t.Setenv("SSH_TTY", "/dev/pts/1")
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	mo := waitingModalWithCode()
	if cmd := mo.autoOpenBrowser(); cmd != nil || mo.browserOpened {
		t.Fatal("auto-open attempted over headless SSH")
	}
}

// Mouse reporting stays enabled for the compact sign-in action.
func TestClaudeLoginModalMouseAction(t *testing.T) {
	m := New(nil, false)
	m.width, m.height = 100, 40
	m.claudeLoginModal = waitingModalWithCode()
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("mouse reporting disabled")
	}
	mo := m.claudeLoginModal
	w, h := mo.modalDim(m.width, m.height)
	rendered := ansi.Strip(mo.View(m.styles, m.palette, m.width, m.height))
	if len(strings.Split(rendered, "\n")) != h {
		t.Fatalf("render height differs from geometry: %d vs %d", len(strings.Split(rendered, "\n")), h)
	}
	for row, line := range strings.Split(rendered, "\n") {
		if col := strings.Index(line, "[ Open browser ]"); col >= 0 {
			x := (m.width-w)/2 + ansi.StringWidth(line[:col])
			y := (m.height-h)/2 + row
			_, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			if cmd == nil || !mo.browserFocused {
				t.Fatal("mouse did not activate browser button")
			}
			return
		}
	}
	t.Fatal("missing browser action")
}
