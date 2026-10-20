package ui

import (
	"cercano/source/clients/cli/internal/form"
	"cercano/source/clients/cli/internal/overlay"
	"cercano/source/server/pkg/agentclient"
	tea "charm.land/bubbletea/v2"
	"strings"
	"testing"
)

func TestCatalogReplyPreservesActiveEditor(t *testing.T) {
	p := cloudSamplePage()
	p.selectCloudRow("profile:work-openai")
	editor := form.NewMasked("cloud-key", "Key", true)
	p.form = form.New([]form.Section{{Fields: []form.Field{editor}}})
	reloads := 0
	p.form.OnReload = func() []form.Section {
		reloads++
		return []form.Section{{Fields: []form.Field{form.NewMasked("cloud-key", "Key", true)}}}
	}
	p.form.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	p.form.Paste("synthetic-token")
	p.applyCatalogResult(cloudCatalogResultMsg{page: p, generation: p.catalogGeneration, scope: p.catalogScope(), models: []agentclient.CloudModelInfo{{ID: "actual"}}})
	if reloads != 0 || p.form.Sections[0].Fields[0] != editor || !editor.Editing() {
		t.Fatal("async catalog reply replaced active credential editor")
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if reloads != 1 {
		t.Fatal("deferred refresh was not applied after editing")
	}
}
func TestCatalogFailedSelectionAndCancel(t *testing.T) {
	p := draftTestPage()
	p.cloudModelsFetched = true
	p.cloudModels = []agentclient.CloudModelInfo{{ID: "actual", DisplayName: "Readable", Metadata: &agentclient.CatalogMetadata{Chat: agentclient.CatalogCapabilitySupported, Tools: agentclient.CatalogCapabilitySupported}}}
	p.openCatalog("cloud-quality-standard", false)
	p.catalogPicker.State = overlay.CatalogState{}
	p.catalogPicker.Paste("actual")
	p.updateCatalogKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !p.cloudDirty || p.cloudDraft.Choices.TierOverrides["standard"] != "actual" {
		t.Fatal("failed autosave did not retain selection")
	}
	p.cloudDirty = false
	p.openCatalog("cloud-quality-standard", false)
	p.updateCatalogKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if p.cloudDirty || p.catalogPicker != nil {
		t.Fatal("cancel mutated draft or failed to close")
	}
}
func TestCatalogUnknownMetadataAndUnlistedSelection(t *testing.T) {
	p := draftTestPage()
	p.catalogField = "cloud-image"
	p.cloudModels = []agentclient.CloudModelInfo{{ID: "unknown"}}
	entries := p.pickerEntries()
	found := false
	for _, e := range entries {
		if e.ID == "unknown" && e.DisabledReason == "" {
			t.Fatal("unknown vision became selectable")
		}
		if e.ID == "image" {
			found = e.Current && e.Unconfirmed
		}
	}
	if !found {
		t.Fatal("configured absent ID was dropped")
	}
	e := catalogEntry(agentclient.CloudModelInfo{ID: "x", Metadata: &agentclient.CatalogMetadata{}}, "work")
	for _, d := range e.Details {
		if strings.Contains(d.Label, "tokens") && d.Value != "Unknown" {
			t.Fatal("unknown metadata invented", d)
		}
	}
}
func TestCatalogPopoverOwnsConfigFocus(t *testing.T) {
	p := draftTestPage()
	p.openCatalog("cloud-image", false)
	m := Model{content: p, configSurface: &configSurface{focused: false}}
	var handled bool
	m, _, handled = m.handleConfigSurfaceKey(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if !handled || m.configSurface.focused {
		t.Fatal("Shift+Tab escaped modal")
	}
	m, _, handled = m.handleConfigSurfaceKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !handled || m.configSurface.focused || p.catalogPicker != nil {
		t.Fatal("Escape escaped config before closing modal")
	}
}

func TestCatalogCredentialFocusDoesNotSave(t *testing.T) {
	p := cloudSamplePage()
	p.selectCloudRow("template:gemini")
	p.form = form.New([]form.Section{p.buildCloudSection()})
	p.openCatalog("cloud-quality-standard", false)
	p.updateCatalogKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if p.catalogPicker != nil || p.cloudDirty || p.cloudDraft.apiKeyEdited {
		t.Fatal("credential action saved or edited draft")
	}
	fields := p.form.Sections[0].Fields
	if fields[p.form.Cursor()].Key() != "cloud-key" {
		t.Fatal("credential action did not focus masked editor")
	}
}

func TestCatalogRefreshCompletionNotification(t *testing.T) {
	for _, problem := range []string{"", "Discovery unavailable"} {
		p := cloudSamplePage()
		p.selectCloudRow("profile:work-openai")
		c := overlay.NewCatalog("Choose a model", "", false)
		p.catalogPicker = &c
		p.catalogBusy = true
		p.applyCatalogResult(cloudCatalogResultMsg{page: p, generation: p.catalogGeneration, scope: p.catalogScope(), err: problem, models: []agentclient.CloudModelInfo{{ID: "actual"}}})
		want := "Models refreshed."
		if problem != "" {
			want = "Refresh failed. See the discovery error above."
		}
		if p.catalogBusy || p.catalogPicker.State.Notification != want {
			t.Fatalf("refresh result not visible: %+v", p.catalogPicker.State)
		}
	}
}

func TestCatalogSuccessFeedbackExpires(t *testing.T) {
	p := cloudSamplePage()
	p.selectCloudRow("profile:work-openai")
	c := overlay.NewCatalog("Choose a model", "", false)
	p.catalogPicker = &c
	result := cloudCatalogResultMsg{page: p, generation: p.catalogGeneration, scope: p.catalogScope()}
	p.applyCatalogResult(result)
	cmd := p.catalogFeedbackTimer(result)
	if cmd == nil {
		t.Fatal("missing reset timer")
	}
	msg, ok := cmd().(cloudCatalogFeedbackExpiredMsg)
	if !ok {
		t.Fatal("wrong timer message")
	}
	p.catalogGeneration++
	p.expireCatalogFeedback(msg)
	if c.State.Notification == "" {
		t.Fatal("old timer cleared newer feedback")
	}
	p.catalogGeneration--
	p.expireCatalogFeedback(msg)
	if c.State.Notification != "" {
		t.Fatal("success feedback did not expire")
	}
}

func TestCatalogTitleIncludesProviderAndTier(t *testing.T) {
	p := cloudSamplePage()
	p.cloudDraft.Provider = "deepinfra"
	p.cloudDraft.Name = "deepinfra"
	p.openCatalog("cloud-quality-economy", false)
	if got := p.catalogPicker.Title; got != "DeepInfra - Choose a Model > Economy" {
		t.Fatalf("title = %q", got)
	}
	if p.catalogPicker.Scope != "" {
		t.Fatal("duplicate provider subtitle")
	}
	p.cloudDraft.Name = "Work"
	p.openCatalog("cloud-image", false)
	if p.catalogPicker.Title != "DeepInfra - Choose a Model > Image" || p.catalogPicker.Scope != "Work" {
		t.Fatal("image title or account subtitle incorrect")
	}
}
