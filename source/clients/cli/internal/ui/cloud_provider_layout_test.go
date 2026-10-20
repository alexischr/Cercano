package ui

import (
	"cercano/source/clients/cli/internal/form"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"os"
	"strings"
	"testing"
)

func layoutPage() *settingsPage {
	p := cloudSamplePage()
	p.scope = scopeCloud
	p.form = form.New(p.buildCloudSections())
	p.form.OnReload = p.buildCloudSections
	p.form.OnCommit = p.onCommit
	return p
}
func layoutClick(t *testing.T, p *settingsPage, label string) {
	t.Helper()
	lines := strings.Split(ansi.Strip(p.View()), "\n")
	for y, line := range lines {
		if strings.Contains(line, label) {
			p.handleClick(ansi.StringWidth(line[:strings.Index(line, label)])+1, y)
			return
		}
	}
	t.Fatalf("missing clickable %q in %s", label, p.View())
}
func TestCloudProviderLayoutMouse(t *testing.T) {
	p := layoutPage()
	view := ansi.Strip(p.View())
	if strings.Contains(view, "Gemini") || strings.Contains(view, "navigate") {
		t.Fatal(view)
	}
	layoutClick(t, p, "Add provider")
	if p.providerPicker == nil {
		t.Fatal("add provider not clickable")
	}
	p.handlePaste("gem")
	view = ansi.Strip(p.View())
	if !strings.Contains(view, "Gemini") || strings.Contains(view, "Navigate") {
		t.Fatal(view)
	}
	for y, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "Gemini") {
			p.handleClick((p.width-72)/2+5, y)
			break
		}
	}
	if p.providerPicker != nil || p.cloudSelected != "template:gemini" || !p.cloudDraftNew {
		t.Fatal("provider selection failed")
	}
	if p.cloudAdvanced {
		t.Fatal("advanced opened by default")
	}
	layoutClick(t, p, "Advanced settings")
	if !p.cloudAdvanced {
		t.Fatal("advanced not clickable")
	}
	layoutClick(t, p, "API key")
	field, _ := cleanupFieldAt(p, "cloud-key")
	if !field.Editing() {
		t.Fatal("key not clickable")
	}
	p.handlePaste("test-key")
	// Clicking another field commits the in-memory credential draft first.
	layoutClick(t, p, "Account name")
	if p.cloudDraft.apiKey != "test-key" {
		t.Fatal("click lost key draft")
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	p.onCommit("cloud-discard", "")
	p.form.Reload()
	if p.cloudSelected != "" || p.cloudDraft.apiKey != "" {
		t.Fatal("cancel retained draft")
	}
}
func TestCloudProviderLayoutSectionsAndRemoval(t *testing.T) {
	p := layoutPage()
	p.selectCloudRow("profile:work-openai")
	p.form.Reload()
	// Separate bordered sections can extend below the modal viewport.
	view := ansi.Strip(strings.Join(p.form.Lines(p.formWidth(), p.palette, p.styles), "\n"))
	for _, label := range []string{"Connection", "Model tiers", "Advanced settings"} {
		if !strings.Contains(view, label) {
			t.Fatal(view)
		}
	}
	if strings.Contains(view, "base-url") {
		t.Fatal("advanced exposed")
	}
	p.ScrollTo(10000)
	layoutClick(t, p, "delete account")
	if !p.cloudDeleteConfirm {
		t.Fatal("removal did not ask confirmation")
	}
	layoutClick(t, p, "Keep account")
	if p.cloudDeleteConfirm {
		t.Fatal("cancel removal failed")
	}
	if path := os.Getenv("CERCANO_CLOUD_LAYOUT_RENDER"); path != "" {
		if err := os.WriteFile(path, []byte(p.View()), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCloudProviderRootMouseAndScroll(t *testing.T) {
	p := layoutPage()
	p.selectCloudRow("profile:work-openai")
	p.form.Reload()
	p.height = 25
	p.offset = 8
	lines := strings.Split(ansi.Strip(p.View()), "\n")
	m := Model{content: p, width: p.width, height: 25, configSurface: &configSurface{active: configTabCloud, focused: true}}
	clicked := false
	for y, line := range lines {
		if strings.Contains(line, "Economy") || strings.Contains(line, "economy") {
			m.Update(tea.MouseClickMsg{X: 7, Y: m.contentTop() + y, Button: tea.MouseLeft})
			clicked = true
			break
		}
	}
	if !clicked || p.catalogPicker == nil {
		t.Fatalf("scrolled model button not activated: %s", p.View())
	}
	before := p.offset
	m.Update(tea.MouseWheelMsg{X: 7, Y: m.contentTop() + 4, Button: tea.MouseWheelDown})
	if p.offset != before {
		t.Fatal("modal wheel scrolled background")
	}
	p.catalogPicker = nil
	p.closeCloudEditor()
	p.offset = 0
	p.cloudDirty = false
	p.form.Reload()
	p.View()
	layoutClick(t, p, "Add provider")
	p.View()
	m.Update(tea.MouseClickMsg{X: 2, Y: m.configStripTop(), Button: tea.MouseLeft})
	if p.providerPicker == nil {
		t.Fatal("modal click escaped to config strip")
	}
}

func TestCloudProviderNewSubscriptionPrerequisite(t *testing.T) {
	p := layoutPage()
	p.selectCloudRow("template:openai-responses")
	_, field := p.catalogPrerequisite()
	if field != "cloud-signin" {
		t.Fatalf("new subscription directs to %q instead of sign-in", field)
	}
}

func TestCloudProviderAuthSwitchAndDuplicateAccounts(t *testing.T) {
	p := layoutPage()
	p.openProviderPicker()
	count := 0
	for _, entry := range p.providerPicker.Entries {
		if entry.Name == "OpenAI" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("auth methods should share one provider, got %d", count)
	}
	p.providerPicker = nil
	p.selectCloudRow("template:openai-responses")
	p.form.Reload()
	p.onCommit("cloud-auth-switch", "")
	if p.cloudDraft.Flavor != "chat_completions" {
		t.Fatal("did not switch to key auth")
	}
	p.cloudDraft.apiKeyEdited = true
	p.cloudDraft.apiKey = "pending"
	p.onCommit("cloud-auth-switch", "")
	if p.cloudDraft.apiKey != "pending" {
		t.Fatal("auth switch discarded pending key")
	}
}
