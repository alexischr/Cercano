package ui

import (
	tea "charm.land/bubbletea/v2"
	"testing"
)

func TestTUIUpdateNoticePromptsEachTerminalLocally(t *testing.T) {
	first := New(nil, false).WithTUIVersion("1.0")
	second := New(nil, false).WithTUIVersion("1.0")
	first.input.SetValue("draft one")
	second.input.SetValue("draft two")
	first, _ = first.applyTUIUpdateNotice("1.1", 2)
	second, _ = second.applyTUIUpdateNotice("1.1", 2)
	if first.pendingConfirm == nil || second.pendingConfirm == nil {
		t.Fatal("missing local confirmations")
	}
	first, _ = first.pendingConfirm.onNo(first)
	if first.pendingConfirm != nil || second.pendingConfirm == nil || first.input.Value() != "draft one" || second.input.Value() != "draft two" {
		t.Fatal("terminal state was coupled")
	}
	first, _ = first.applyTUIUpdateNotice("1.1", 2)
	if first.pendingConfirm != nil {
		t.Fatal("replay prompted after deferral")
	}
	first, _ = first.applyTUIUpdateNotice("1.2", 3)
	if first.pendingConfirm == nil {
		t.Fatal("new update suppressed")
	}
}
func TestTUIUpdateNoticeValidatesAndKeepsStreamDraining(t *testing.T) {
	m := New(nil, false).WithTUIVersion("1.1")
	for _, v := range []string{"1.1", "/tmp/evil", "bad\nvalue", ""} {
		m, _ = m.applyTUIUpdateNotice(v, 1)
		if m.pendingConfirm != nil {
			t.Fatal("unneeded/invalid prompt", v)
		}
	}
	m = New(nil, false).WithTUIVersion("1.0")
	m.streaming = true
	m.input.SetValue("draft")
	type nextEvent struct{}
	updated, cmd := m.Update(tuiUpdateNoticeMsg{version: "1.1", operationID: 2, next: func() tea.Msg { return nextEvent{} }})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("stream draining stopped")
	}
	if _, ok := cmd().(nextEvent); !ok {
		t.Fatal("next event lost")
	}
	if m.pendingConfirm != nil || !m.tuiUpdateAvailable || m.input.Value() != "draft" {
		t.Fatal("busy terminal interrupted")
	}
}
