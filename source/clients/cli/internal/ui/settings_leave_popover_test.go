package ui

import (
	"strings"
	"testing"

	"cercano/source/clients/cli/internal/overlay"
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

func TestSettingsLeavePromptKeyboardSelection(t *testing.T) {
	p := layoutPage()
	p.cloudDirty = true
	p.settingsPendingLeave = "__close"
	p.settingsNavigationPrompt = true
	if !strings.Contains(ansi.Strip(p.View()), "› [ Keep editing ]") {
		t.Fatal("missing safe default selection caret")
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if !strings.Contains(ansi.Strip(p.View()), "› [ Discard and leave ]") {
		t.Fatal("up did not select discard")
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if !strings.Contains(ansi.Strip(p.View()), "› [ Keep editing ]") {
		t.Fatal("tab did not move selection")
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if p.settingsNavigationPrompt || !p.cloudDirty {
		t.Fatal("enter did not activate keep editing")
	}
}

func TestSettingsLeavePromptOwnsKeysOverPicker(t *testing.T) {
	p := layoutPage()
	p.cloudDirty = true
	picker := overlay.NewCatalog("Models", "test", false)
	p.catalogPicker = &picker
	p.settingsPendingLeave = "__close"
	p.settingsNavigationPrompt = true
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if p.settingsNavigationPrompt || !p.cloudDirty || p.catalogPicker == nil {
		t.Fatal("underlying picker stole confirmation input")
	}
}

func TestSettingsLeavePromptConfigNavigationKeyboard(t *testing.T) {
	p := layoutPage()
	p.cloudDirty = true
	p.settingsNavigationPrompt = true
	m := Model{content: p, configSurface: &configSurface{active: configTabCloud, pendingClose: true}}
	var handled bool
	m, _, handled = m.handleConfigSurfaceKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if !handled || !p.settingsLeaveDiscardSelected {
		t.Fatal("config navigation swallowed Tab")
	}
	m, _, _ = m.handleConfigSurfaceKey(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if p.settingsLeaveDiscardSelected {
		t.Fatal("shift-tab did not select keep")
	}
	m, _, _ = m.handleConfigSurfaceKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.configSurface == nil || m.configSurface.pendingClose || p.settingsNavigationPrompt || !p.cloudDirty {
		t.Fatal("Enter did not keep editing config")
	}
}
