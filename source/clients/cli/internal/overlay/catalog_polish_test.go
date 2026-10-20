package overlay

import (
	"cercano/source/clients/cli/internal/theme"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestCatalogFooterKeyboardReachable(t *testing.T) {
	for _, action := range []CatalogAction{CatalogRefresh, CatalogCancel, CatalogCustom, CatalogSelect} {
		c := NewCatalog("Choose a model", "", false)
		c.SetEntries([]CatalogEntry{{ID: "model", Name: "Model"}, {Name: "Enter custom ID…", Action: CatalogCustom}})
		found := false
		for i := 0; i < 12; i++ {
			c.Update(tea.KeyPressMsg{Code: tea.KeyTab})
			copy := c
			_, out := copy.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if out.Action == action {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Tab cannot reach action %v", action)
		}
	}
}

func TestCatalogFooterReverseFocusAndMouse(t *testing.T) {
	for _, width := range []int{40, 60, 100, 120} {
		c := NewCatalog("Choose a model", "Personal", false)
		c.SetEntries([]CatalogEntry{{ID: "model", Name: "Model"}, {Action: CatalogCustom, Name: "Custom"}})
		c.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
		if c.focus != 7 {
			t.Fatalf("reverse Tab did not reach Use: %d", c.focus)
		}
		view := ansi.Strip(c.View(width, 24, theme.Cracker()))
		if width >= 64 && !strings.Contains(view, "[ Use ]") {
			t.Fatal("Use button clipped: " + view)
		}
		if strings.Contains(view, "Tab move") {
			t.Fatal("unexpected keyboard-help line")
		}
		for _, action := range []CatalogAction{CatalogCustom, CatalogRefresh, CatalogCancel, CatalogSelect} {
			found := false
			for _, hit := range c.hits {
				if hit.action != action {
					continue
				}
				if hit.to > width-2 {
					t.Fatalf("footer hit exceeds viewport at %d: %+v", width, hit)
				}
				copy := c
				_, out := copy.Update(tea.MouseClickMsg{X: hit.from, Y: hit.y, Button: tea.MouseLeft})
				if out.Action != action {
					t.Fatalf("mouse action %v returned %+v", action, out)
				}
				found = true
			}
			if !found {
				t.Fatalf("missing footer action %v", action)
			}
		}
	}
}
