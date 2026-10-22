package ui

import (
	"testing"

	"cercano/source/clients/cli/internal/wizard"
	tea "charm.land/bubbletea/v2"
)

func TestSetupRerunDoesNotReplayWelcomeOverChatBanner(t *testing.T) {
	t.Setenv("CERCANO_WIZARD_STATE", t.TempDir()+"/wizard.yaml")
	m := New(nil, false)
	m = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.splashShown = false
	m.mainChat().PrependBanner(m.splash.Meta, m.splash.Started())
	m.mainChat().AppendEntry(&Entry{Role: RoleUser, Content: "keep this conversation"})
	m.refreshViewport()
	epoch, count := m.splash.Started(), len(m.mainChat().Entries())
	for _, finish := range []bool{false, true, false} {
		next, _ := m.runSlash("/setup")
		m = next.(Model)
		wp, ok := m.content.(*wizardPage)
		if !ok {
			t.Fatal("setup did not open")
		}
		key := tea.KeyEscape
		if finish {
			wp.state.Step = wizard.StepDone
			wp.state.LocusMode = "cloud_only"
			wp.cursor = 0
			wp.applyFn = func() error { return nil }
			key = tea.KeyEnter
		}
		m = send(t, m, tea.KeyPressMsg{Code: key})
		if m.content != nil {
			t.Fatal("setup did not close")
		}
		if m.splashShown {
			t.Fatal("rerunning setup adds a second welcome block above the chat banner")
		}
		if !m.mainChat().HasBanner() || len(m.mainChat().Entries()) != count {
			t.Fatal("existing banner or conversation was altered")
		}
		if !m.splash.Started().Equal(epoch) {
			t.Fatal("rerunning setup restarted the opening animation")
		}
	}
}

func TestStartupSetupStillRevealsOneWelcome(t *testing.T) {
	t.Setenv("CERCANO_WIZARD_STATE", t.TempDir()+"/wizard.yaml")
	m := New(nil, false).OpenWizardOnStart()
	m = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	if m.content == nil || m.splashShown {
		t.Fatal("startup setup did not suppress welcome")
	}
	m = send(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.content != nil || !m.splashShown || m.mainChat().HasBanner() {
		t.Fatal("startup setup must reveal exactly one welcome")
	}
}
