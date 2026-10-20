package ui

import (
	"cercano/source/clients/cli/internal/form"
	"charm.land/lipgloss/v2"
)

// The active form belongs exclusively to the editor while it is open. The
// provider list behind it is rendered separately and cannot receive input.
func (sp *settingsPage) formWidth() int {
	if sp.cloudEditorActive() {
		return max(4, min(sp.width, 100)-2)
	}
	return sp.width
}

func (sp *settingsPage) formHeight() int {
	if sp.cloudEditorActive() {
		return max(1, min(sp.viewportHeight(), 32)-2)
	}
	return sp.viewportHeight()
}

func (sp *settingsPage) cloudEditorView(body string) string {
	background := form.New([]form.Section{sp.cloudProviderListSection()})
	background.HideHelp = true
	background.CompactActions = true
	base := renderScrollable(background.Lines(sp.width, sp.palette, sp.styles), sp.viewportHeight(), sp.width-2, 0, sp.styles)
	panel := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Width(sp.formWidth() + 2).Height(sp.formHeight() + 2).Render(body)
	return composeOverlay(base, panel, (sp.width-sp.formWidth()-2)/2, 0)
}

func (sp *settingsPage) closeCloudEditor() {
	sp.selectCloudRow("")
	sp.form.Reload()
	sp.form.SetCursor(0)
}

func (sp *settingsPage) cloudEditorActive() bool {
	return sp.cloudEditorOpen && sp.cloudSelected != "" && sp.scope == scopeCloud
}

// Use the same bordered Section renderer as configuration pages. Keep one
// form so keyboard navigation, scrolling and mouse hit-testing stay shared.
func (sp *settingsPage) buildCloudSections() []form.Section {
	section := sp.buildCloudSection()
	if !sp.cloudEditorActive() {
		return []form.Section{section}
	}
	sections := []form.Section{{Title: section.Title}}
	for _, field := range section.Fields {
		title := ""
		heading := true
		switch field.Key() {
		case "cloud-connection-heading":
			title = "Connection"
		case "cloud-model-heading":
			title = "Model tiers"
		case "cloud-actions-heading":
			title = "Actions"
		case "cloud-advanced":
			title = "Advanced settings"
			heading = false
		}
		if title != "" {
			sections = append(sections, form.Section{Title: title})
			if heading {
				continue
			}
		}
		last := len(sections) - 1
		sections[last].Fields = append(sections[last].Fields, field)
	}
	if len(sections[0].Fields) == 0 {
		sections = sections[1:]
	}
	return sections
}
