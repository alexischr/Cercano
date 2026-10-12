package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cercano/source/clients/cli/internal/tuirestart"
)

func TestTUIRestartConfirmationPreservesDraft(t *testing.T) {
	m := New(nil, false)
	m.input.SetValue("unsent draft")
	m, cmd := m.requestTUIRestart()
	if cmd != nil || m.pendingConfirm == nil {
		t.Fatal("restart bypassed confirmation")
	}
	if !strings.Contains(m.pendingConfirm.hints, "d details") {
		t.Fatal("missing details choice")
	}
	details := m.pendingConfirm.extras["d"]
	m, _ = details(m)
	if m.pendingConfirm == nil || m.input.Value() != "unsent draft" {
		t.Fatal("details consumed prompt/draft")
	}
	m, _ = m.pendingConfirm.onNo(m)
	if m.pendingConfirm != nil || m.input.Value() != "unsent draft" {
		t.Fatal("defer lost state")
	}
}
func TestTUIRestartActiveWorkRefused(t *testing.T) {
	m := New(nil, false)
	m.streaming = true
	m, cmd := m.requestTUIRestart()
	if cmd != nil || m.pendingConfirm != nil {
		t.Fatal("active work restarted")
	}
	m.streaming = false
	m, _ = m.requestTUIRestart()
	m.streaming = true
	m, cmd = m.pendingConfirm.onYes(m)
	if cmd != nil {
		t.Fatal("work started during prompt still restarted")
	}
}
func TestTUIRestartYesIsLocal(t *testing.T) {
	// nil agent makes any accidental /restart-agent RPC fail. The command is
	// not executed by this unit test; a separate owned process tests Unix exec.
	m := New(nil, false)
	m, _ = m.requestTUIRestart()
	m, cmd := m.pendingConfirm.onYes(m)
	if m.pendingConfirm != nil {
		t.Fatal("confirmation not consumed")
	}
	if tuirestart.Supported() && cmd == nil {
		t.Fatal("missing terminal handoff command")
	}
	if !tuirestart.Supported() && cmd != nil {
		t.Fatal("manual platform tried replacement")
	}
}
func TestTUIUpdateNoticeDoesNotReplacePendingDecision(t *testing.T) {
	m := New(nil, false)
	pending := &confirmRequest{title: "existing decision"}
	m.pendingConfirm = pending
	updated, _ := m.Update(TUIUpdateAppliedMsg{Target: "/trusted/installed/client"})
	m = updated.(Model)
	if m.pendingConfirm != pending || !m.tuiUpdateAvailable {
		t.Fatal("update notification replaced decision")
	}
	m.pendingConfirm = nil
	m, _ = m.requestTUIRestart()
	if !strings.Contains(m.pendingConfirm.title, "update to the TUI has been applied") {
		t.Fatal("missing installed-update explanation")
	}
}
func TestTUIRestartRestoresDraftWithoutSubmission(t *testing.T) {
	m := New(nil, false)
	s := tuirestart.State{WorkingDirectory: t.TempDir(), Draft: "do not submit this"}
	m = m.RestoreTUI(s)
	if m.input.Value() != s.Draft || m.workDirOverride != s.WorkingDirectory || m.streaming {
		t.Fatal("restore changed draft semantics")
	}
}

func TestTUIRestartRetainsAttachmentsAndQueuedTurns(t *testing.T) {
	m := New(nil, false)
	img := filepath.Join(t.TempDir(), "draft.png")
	if e := os.WriteFile(img, onePxPNG, 0600); e != nil {
		t.Fatal(e)
	}
	if !m.handleImagePaste(img) {
		t.Fatal("image fixture not attached")
	}
	m, cmd := m.requestTUIRestart()
	if cmd != nil || m.pendingConfirm != nil || len(m.input.Attachments()) != 1 {
		t.Fatal("attachment could be lost during restart")
	}
	n := New(nil, false)
	n.mainChat().Enqueue("queued unsent", nil)
	n, cmd = n.requestTUIRestart()
	if cmd != nil || n.pendingConfirm != nil || len(n.mainChat().Queued()) != 1 {
		t.Fatal("queued turn could be lost during restart")
	}
}
