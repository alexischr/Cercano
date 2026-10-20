package ui

import tea "charm.land/bubbletea/v2"

// Use the real configuration page while its provider picker/editor is open.
// Keeping it as the active content page reuses paste, mouse, async-save, catalog,
// and OAuth result handling rather than introducing a second authentication UI.
type wizardAddProviderMsg struct{ page *wizardPage }

func (m Model) openWizardProvider(msg wizardAddProviderMsg) (tea.Model, tea.Cmd) {
	if msg.page == nil || m.content != msg.page || m.wizardProviderReturn != nil {
		return m, nil
	}
	sp, cmd := newScopedSettingsPage(m.agent, m.palette, m.styles, m.promptColorToken, m.width, m.contentPageHeight(), m.themes, m.theme, scopeCloud)
	m.wizardProviderReturn = msg.page
	m.content = sp
	sp.openProviderPicker()
	return m, cmd
}

func (m *Model) restoreWizardProvider() {
	wp := m.wizardProviderReturn
	if wp == nil {
		return
	}
	m.wizardProviderReturn = nil
	wp.discoverResources()
	wp.authPick = false
	wp.cursor = wp.defaultCursor()
	wp.SetSize(m.width, m.contentPageHeight())
	m.content = wp
}
