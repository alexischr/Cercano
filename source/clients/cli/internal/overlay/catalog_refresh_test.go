package overlay

import (
	"cercano/source/clients/cli/internal/theme"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestCatalogRefreshFeedback(t *testing.T) {
	c := NewCatalog("Choose a model", "Personal", false)
	c.SetEntries([]CatalogEntry{{ID: "model", Name: "Model"}})
	c.State = CatalogState{Busy: true}
	view := ansi.Strip(c.View(100, 24, theme.Cracker()))
	if !strings.Contains(view, "[ Refreshing… ]") {
		t.Fatal("refresh button gives no busy feedback")
	}
	c.focus = 5
	if _, out := c.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); out.Action != CatalogIdle {
		t.Fatal("busy refresh was activated twice")
	}
}
