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

func TestCatalogSuccessFeedbackIsInButton(t *testing.T) {
	c := NewCatalog("Choose a model", "", false)
	c.State.Notification = "Models refreshed."
	view := ansi.Strip(c.View(100, 24, theme.Cracker()))
	if !strings.Contains(view, "[ Models refreshed ]") || strings.Count(view, "Models refreshed") != 1 {
		t.Fatalf("success should appear only in button: %s", view)
	}
	c.State.Notification = ""
	if view = ansi.Strip(c.View(100, 24, theme.Cracker())); !strings.Contains(view, "[ Refresh ]") || strings.Contains(view, "Models refreshed") {
		t.Fatal("button did not reset")
	}
}
