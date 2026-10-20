package ui

import (
	"cercano/source/clients/cli/internal/overlay"
	tea "charm.land/bubbletea/v2"
	"fmt"
	"strings"
)

func (sp *settingsPage) openProviderPicker() {
	if sp.hasUnsavedSettings() {
		sp.form.SetStatus("Save or discard your changes before adding a provider.")
		return
	}
	c := overlay.NewCatalog("Add provider", "", false)
	c.Providers = true
	seen := map[string]bool{}
	for _, p := range sp.cloudView.Providers {
		if p.Tier == "coming_soon" {
			continue
		}
		family := cloudProviderFamily(p.ID)
		if seen[family] {
			continue
		}
		seen[family] = true
		c.Entries = append(c.Entries, overlay.CatalogEntry{ID: "template:" + p.ID, Name: cloudProviderLabel(p.ID, p.Label)})
	}
	c.Entries = append(c.Entries, overlay.CatalogEntry{ID: "other", Name: "Custom endpoint"})
	sp.providerPicker = &c
}
func (sp *settingsPage) updateProviderPicker(msg tea.Msg) tea.Cmd {
	cmd, out := sp.providerPicker.Update(msg)
	switch out.Action {
	case overlay.CatalogCancel:
		sp.providerPicker = nil
	case overlay.CatalogSelect:
		sp.providerPicker = nil
		sp.selectCloudRow(out.ID)
		sp.cloudEditorOpen = true
		sp.uniqueCloudDraftName()
		sp.cloudDirty = true
		sp.form.Reload()
		sp.focusCloudField("cloud-name")
	}
	return cmd
}
func (sp *settingsPage) handleClick(x, y int) tea.Cmd {
	if sp.settingsNavigationPrompt {
		if y == 1 {
			code := rune(0)
			if x >= 0 && x < 21 {
				code = 'y'
			}
			if x >= 23 && x < 40 {
				code = 'n'
			}
			if code != 0 {
				cmd, _ := sp.Update(tea.KeyPressMsg{Code: code, Text: string(code)})
				return cmd
			}
		}
		return nil
	}
	if sp.pendingCommit {
		return nil
	}
	if sp.providerPicker != nil {
		w := min(sp.width, 72)
		return sp.updateProviderPicker(tea.MouseClickMsg{X: x - (sp.width-w)/2, Y: y, Button: tea.MouseLeft})
	}
	if sp.catalogPicker != nil {
		w := min(sp.width, 120)
		return sp.updateCatalogKey(tea.MouseClickMsg{X: x - (sp.width-w)/2, Y: y, Button: tea.MouseLeft})
	}
	if sp.cloudEditorActive() {
		x -= (sp.width-sp.formWidth()-2)/2 + 1
		y--
	}
	if x < 0 || x >= sp.formWidth()-2 || y < 0 || y >= sp.formHeight() {
		return nil
	}
	sp.form.Lines(sp.formWidth(), sp.palette, sp.styles)
	return sp.form.Click(x, y+sp.offset)
}

func cloudProviderFamily(id string) string {
	switch id {
	case "anthropic-subscription":
		return "anthropic"
	case "openai-responses":
		return "openai"
	}
	return id
}
func cloudProviderLabel(id, label string) string {
	switch cloudProviderFamily(id) {
	case "anthropic":
		return "Anthropic"
	case "openai":
		return "OpenAI"
	}
	return strings.ToUpper(label[:min(1, len(label))]) + label[min(1, len(label)):]
}

func (sp *settingsPage) uniqueCloudDraftName() {
	base := sp.cloudDraft.Name
	for i := 2; sp.cloudAccountNameExists(sp.cloudDraft.Name); i++ {
		sp.cloudDraft.Name = fmt.Sprintf("%s-%d", base, i)
	}
}
