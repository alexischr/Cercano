package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestSettingsLeavePromptOverlaysWithoutMovingPage(t *testing.T) {
	for _, scope := range []settingsScope{scopeCloud, scopeRouting} {
		p := layoutPage()
		p.scope = scope
		before := p.View()
		p.settingsPendingLeave = "__close"
		p.settingsNavigationPrompt = true
		after := p.View()
		if lipgloss.Height(after) != lipgloss.Height(before) {
			t.Fatalf("prompt moved page: height %d -> %d", lipgloss.Height(before), lipgloss.Height(after))
		}
		if strings.HasPrefix(ansi.Strip(after), "Unsaved ") {
			t.Fatal("prompt is inline rather than overlaid")
		}
		if !strings.Contains(ansi.Strip(after), "Keep editing") {
			t.Fatal("missing confirmation")
		}
	}
}

func TestSettingsLeavePromptMouseUsesRenderedPosition(t *testing.T) {
	p := layoutPage()
	p.cloudDirty = true
	p.settingsPendingLeave = "__close"
	p.settingsNavigationPrompt = true
	for y, line := range strings.Split(ansi.Strip(p.View()), "\n") {
		if x := strings.Index(line, "[ Keep editing ]"); x >= 0 {
			p.handleClick(ansi.StringWidth(line[:x])+2, y)
			if p.settingsNavigationPrompt || !p.cloudDirty {
				t.Fatal("keep editing failed or discarded input")
			}
			return
		}
	}
	t.Fatal("keep editing not rendered")
}

func TestSettingsLeavePromptEscapeKeepsInput(t *testing.T) {
	p := layoutPage()
	p.cloudDirty = true
	p.settingsPendingLeave = "__close"
	p.settingsNavigationPrompt = true
	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if p.settingsNavigationPrompt || !p.cloudDirty {
		t.Fatal("escape discarded input")
	}
}

func TestSettingsLeavePromptBlocksOutsideClicksAndDiscards(t *testing.T) {
	p := layoutPage()
	p.selectCloudRow("profile:work-openai")
	p.form.Reload()
	p.cloudDirty = true
	p.settingsPendingLeave = "__close"
	p.settingsNavigationPrompt = true
	p.handleClick(0, 0)
	if !p.settingsNavigationPrompt || !p.cloudDirty {
		t.Fatal("outside click affected editor")
	}
	if !p.handlePaste("must not reach editor") {
		t.Fatal("modal did not capture paste")
	}
	for y, line := range strings.Split(ansi.Strip(p.View()), "\n") {
		if x := strings.Index(line, "[ Discard and leave ]"); x >= 0 {
			p.handleClick(ansi.StringWidth(line[:x])+2, y)
			if p.settingsNavigationPrompt || p.cloudDirty || p.cloudEditorActive() {
				t.Fatal("discard did not close editor")
			}
			return
		}
	}
	t.Fatal("discard action missing")
}
