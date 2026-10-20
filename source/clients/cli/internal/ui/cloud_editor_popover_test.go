package ui

import (
	"cercano/source/server/pkg/agentclient"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestConfiguredCloudEditorIsModal(t *testing.T) {
	p := layoutPage()
	p.selectCloudRow("profile:work-openai")
	p.form.Reload()
	for _, f := range p.buildCloudSection().Fields {
		if f.Key() == "cloud-add-provider" {
			t.Fatal("configured editor still shares the provider list form")
		}
	}
	_, closed := p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if closed || p.cloudSelected != "" {
		t.Fatal("Escape should close the editor, not Cloud settings")
	}
}

func TestCloudEditorPopoverMouseAndUnsavedClose(t *testing.T) {
	p := layoutPage()
	p.SetSize(140, 50)
	layoutClick(t, p, "OpenAI")
	if !p.cloudEditorActive() {
		t.Fatal("provider click did not open editor")
	}
	p.View()
	cursor := p.form.Cursor()
	p.handleClick(0, 4)
	if p.form.Cursor() != cursor {
		t.Fatal("outside click reached editor")
	}
	layoutClick(t, p, "API key")
	field, _ := cleanupFieldAt(p, "cloud-key")
	if !field.Editing() {
		t.Fatal("centered editor mouse coordinates incorrect")
	}
	p.handlePaste("pending-secret")
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !p.settingsNavigationPrompt || !p.cloudEditorActive() {
		t.Fatal("close discarded dirty credential")
	}
	p.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if p.cloudDraft.apiKey != "pending-secret" {
		t.Fatal("keep editing lost credential")
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	_, closed := p.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if closed || p.cloudEditorActive() || p.cloudDraft.apiKey != "" {
		t.Fatal("discard should return to provider list and clear credential")
	}
}

func TestCloudEditorPopoverBoundsAndNestedPicker(t *testing.T) {
	for _, size := range [][2]int{{60, 24}, {100, 36}, {140, 50}} {
		p := layoutPage()
		p.SetSize(size[0], size[1])
		p.selectCloudRow("profile:work-openai")
		p.form.Reload()
		view := p.View()
		if len(strings.Split(view, "\n")) > p.viewportHeight() {
			t.Fatal("popover exceeded viewport height")
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > p.width {
				t.Fatal("popover exceeded viewport width")
			}
		}
		p.openCatalog("cloud-image", false)
		p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if p.catalogPicker != nil || !p.cloudEditorActive() {
			t.Fatal("nested picker Escape closed editor")
		}
		m := Model{content: p, configSurface: &configSurface{active: configTabCloud, focused: false}}
		m, _, _ = m.handleConfigSurfaceKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		if p.cloudEditorActive() || m.configSurface.focused {
			t.Fatal("editor Escape escaped to config tabs")
		}
	}
}

func TestCloudEditorUsesSeparateSectionBlocks(t *testing.T) {
	p := layoutPage()
	p.selectCloudRow("profile:work-openai")
	p.form.Reload()
	want := []string{"Connection", "Model tiers", "Advanced settings", "Actions"}
	if len(p.form.Sections) != len(want) {
		t.Fatalf("editor sections = %d; want %d separate bordered blocks", len(p.form.Sections), len(want))
	}
	for i, title := range want {
		if p.form.Sections[i].Title != title {
			t.Fatalf("section %d = %q; want %q", i, p.form.Sections[i].Title, title)
		}
	}
}

func TestAddProviderUsesCloudEditorPopover(t *testing.T) {
	for _, query := range []string{"Mistral", "Custom endpoint"} {
		t.Run(query, func(t *testing.T) {
			p := layoutPage()
			p.cloudView.Providers = append(p.cloudView.Providers, agentclient.CloudProvider{ID: "mistral", Label: "Mistral", Flavor: "chat_completions", BaseURL: "https://api.mistral.ai/v1"})
			p.openProviderPicker()
			p.handlePaste(query)
			p.updateProviderPicker(tea.KeyPressMsg{Code: tea.KeyEnter})
			if p.providerPicker != nil || !p.cloudEditorActive() {
				t.Fatalf("provider selection stayed inline: selected=%q editorOpen=%v", p.cloudSelected, p.cloudEditorOpen)
			}
			if !p.cloudDraftNew || !p.cloudDirty {
				t.Fatal("new provider draft not preserved")
			}
			for _, section := range p.form.Sections {
				for _, f := range section.Fields {
					if f.Key() == "cloud-add-provider" {
						t.Fatal("editor shares provider list form")
					}
				}
			}
			p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			if !p.settingsNavigationPrompt {
				t.Fatal("Escape must protect unsaved setup")
			}
			p.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
			if p.cloudEditorActive() || p.cloudSelected != "" {
				t.Fatal("discard must return to provider list")
			}
		})
	}
}
