package ui

import (
	"cercano/source/clients/cli/internal/form"
	"cercano/source/clients/cli/internal/wizard"
	"cercano/source/server/pkg/agentclient"
	tea "charm.land/bubbletea/v2"
	"fmt"
	"strings"
	"testing"
)

func TestWizardTierIntroductionIsBulleted(t *testing.T) {
	wp := newTestWizardPage(t)
	text := wp.stepDesc()
	for _, name := range []string{"Deep reasoning", "Everyday development", "Lightweight tasks"} {
		if !strings.Contains(text, "* **"+name+"** - ") {
			t.Fatalf("missing tier bullet %q: %s", name, text)
		}
	}
}

func TestWizardCloudUsesSharedAddProviderFlow(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.state.Step = wizard.StepCloud
	if !strings.Contains(wp.stepDesc(), "Cercano works with nearly any cloud provider") {
		t.Fatal("missing provider explanation")
	}
	for i, row := range wp.rows() {
		if row.Key == "add_provider" {
			wp.cursor = i
			cmd, closed := wp.selectRow()
			if closed || cmd == nil {
				t.Fatal("add provider must open shared provider flow")
			}
			if fmt.Sprintf("%T", cmd()) != "ui.wizardAddProviderMsg" {
				t.Fatal("wrong provider flow")
			}
			return
		}
	}
	t.Fatal("missing Add cloud provider button")
}

func TestWizardSubscriptionCatalogAliasesUseOAuth(t *testing.T) {
	for id, auth := range map[string]string{"openai-responses": "chatgpt", "anthropic-subscription": "claude"} {
		rows := wizardAuthRows(id)
		if rows[0].Key != auth {
			t.Fatalf("%s selected %s instead of OAuth", id, rows[0].Key)
		}
	}
}

func TestWizardProviderPickerCancelReturnsToSetup(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.state.Step = wizard.StepCloud
	m := Model{content: wp, width: 100, height: 40, palette: wp.palette, styles: wp.styles}
	next, _ := m.openWizardProvider(wizardAddProviderMsg{page: wp})
	m = next.(Model)
	sp, ok := m.content.(*settingsPage)
	if !ok || sp.providerPicker == nil {
		t.Fatal("shared config picker not opened")
	}
	if query := sp.providerPicker.Query(); query != "" {
		t.Fatalf("provider filter pre-populated: %q", query)
	}
	sp.handlePaste("Anthropic")
	sp.openProviderPicker()
	if query := sp.providerPicker.Query(); query != "" {
		t.Fatalf("new picker retained previous filter: %q", query)
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if m.content != wp || m.wizardProviderReturn != nil {
		t.Fatal("picker cancellation did not return to setup")
	}
	if wp.state.Step != wizard.StepCloud {
		t.Fatal("picker cancellation advanced setup")
	}
}

func TestWizardSharedProviderEditorDispatchesSubscriptionOAuth(t *testing.T) {
	for _, tc := range []struct{ id, label, flavor, route, query, action string }{
		{"anthropic-subscription", "Anthropic", "messages", "subscription", "Anthropic", "cloud-signin-claude"},
		{"openai-responses", "ChatGPT", "responses", "", "OpenAI", "cloud-signin"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			wp := newTestWizardPage(t)
			wp.state.Step = wizard.StepCloud
			m := Model{content: wp, width: 100, height: 40, palette: wp.palette, styles: wp.styles}
			next, _ := m.openWizardProvider(wizardAddProviderMsg{page: wp})
			m = next.(Model)
			sp := m.content.(*settingsPage)
			sp.cloudView.Providers = []agentclient.CloudProvider{{ID: tc.id, Label: tc.label, Flavor: tc.flavor, Route: tc.route}}
			sp.openProviderPicker()
			sp.handlePaste(tc.query)
			sp.updateProviderPicker(tea.KeyPressMsg{Code: tea.KeyEnter})
			if !sp.cloudEditorActive() {
				t.Fatal("provider editor not opened")
			}
			for _, section := range sp.form.Sections {
				for _, field := range section.Fields {
					if field.Key() == "cloud-key" {
						t.Fatal("subscription asks for an API key")
					}
				}
			}
			_, cmd, err := sp.onCommit(tc.action, form.ButtonActivate)
			if err != nil || cmd == nil {
				t.Fatalf("OAuth action not available: %v", err)
			}
			switch msg := cmd().(type) {
			case openClaudeLoginModalMsg:
				if tc.id != "anthropic-subscription" || !msg.createOnly {
					t.Fatal("wrong Claude action")
				}
			case openChatGPTLoginModalMsg:
				if tc.id != "openai-responses" || !msg.createOnly {
					t.Fatal("wrong ChatGPT action")
				}
			default:
				t.Fatalf("expected OAuth modal, got %T", msg)
			}
			if wp.state.Step != wizard.StepCloud {
				t.Fatal("setup advanced before successful connection")
			}
		})
	}
}

func TestWizardIntroductionRendersBoldMarkersWithoutLiteralMarkdown(t *testing.T) {
	wp := newTestWizardPage(t)
	if strings.Contains(wp.View(), "**") {
		t.Fatal("literal Markdown emphasis leaked into setup screen")
	}
}

func TestWizardProviderEditorReturnsWithRefreshedAccounts(t *testing.T) {
	stub, client := newScenario(t)
	wp := scenarioWizard(t, client)
	wp.state.Step = wizard.StepCloud
	wp.curProfiles = nil // prove the return path performs discovery
	m := Model{agent: client, content: wp, width: 100, height: 40, palette: wp.palette, styles: wp.styles}
	next, _ := m.openWizardProvider(wizardAddProviderMsg{page: wp})
	m = next.(Model)
	sp := m.content.(*settingsPage)
	sp.providerPicker = nil
	sp.selectCloudRow("profile:personal")
	sp.form.Reload()
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if m.content != wp || m.wizardProviderReturn != nil {
		t.Fatal("editor did not return to setup")
	}
	if wp.existingActiveProfile() == nil || len(wp.curProfiles) != 2 {
		t.Fatal("connected accounts not refreshed")
	}
	if wp.state.Step != wizard.StepCloud {
		t.Fatal("return advanced setup without account selection")
	}
	if stub.writeCount() != 0 {
		t.Fatal("editor navigation wrote configuration")
	}
}
