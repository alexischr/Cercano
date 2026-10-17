package overlay

import (
	"cercano/source/clients/cli/internal/theme"
	tea "charm.land/bubbletea/v2"
	"os"
	"testing"
)

type catalogTerminalModel struct {
	catalog Catalog
	outcome CatalogOutcome
}

func (m catalogTerminalModel) Init() tea.Cmd { return nil }
func (m catalogTerminalModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd, out := m.catalog.Update(msg)
	if out.Action == CatalogSelect || out.Action == CatalogCancel {
		m.outcome = out
		return m, tea.Quit
	}
	return m, cmd
}
func (m catalogTerminalModel) View() tea.View {
	return tea.NewView(m.catalog.View(m.catalog.width, m.catalog.height, theme.Cracker()))
}

// Opt-in pseudo-terminal test. No agent connection, credentials or network calls.
func TestCatalogTerminal(t *testing.T) {
	if os.Getenv("CERCANO_CATALOG_TTY") != "1" {
		t.Skip("requires a terminal driver")
	}
	c := NewCatalog("Choose a model", "Fixture account", false)
	c.SetEntries([]CatalogEntry{{ID: "mistral-large-4", Name: "Large 4"}, {ID: "zai-glm-5-3", Name: "GLM 5.3", Details: []CatalogDetail{{Label: "Tools", Value: "Supported"}}}})
	result, err := tea.NewProgram(catalogTerminalModel{catalog: c}, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout), tea.WithoutSignalHandler()).Run()
	if err != nil {
		t.Fatal(err)
	}
	m := result.(catalogTerminalModel)
	if os.Getenv("CERCANO_CATALOG_TTY_ACTION") == "cancel" {
		if m.outcome.Action != CatalogCancel || m.catalog.Query() != "glm" {
			t.Fatal("resize/detail/cancel lost query or committed")
		}
	} else if m.outcome.Action != CatalogSelect || m.outcome.ID != "zai-glm-5-3" {
		t.Fatalf("terminal selection wrong: %+v", m.outcome)
	}
	t.Logf("terminal outcome=%d query=%q viewport=%dx%d", m.outcome.Action, m.catalog.Query(), m.catalog.width, m.catalog.height)
}
