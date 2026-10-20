package overlay

import (
	"cercano/source/clients/cli/internal/theme"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestCatalogMouseModelAndCancel(t *testing.T) {
	for _, size := range [][2]int{{60, 18}, {80, 24}, {120, 36}} {
		c := NewCatalog("Choose a model", "work", false)
		c.Entries = []CatalogEntry{{ID: "one", Name: "First model", Account: "work"}, {ID: "two", Name: "Second model", Account: "work"}}
		view := ansi.Strip(c.View(size[0], size[1], theme.Cracker()))
		selected := false
		for y, line := range strings.Split(view, "\n") {
			if strings.Contains(line, "Second model") {
				_, out := c.Update(tea.MouseClickMsg{X: 5, Y: y, Button: tea.MouseLeft})
				if out.ID != "two" {
					t.Fatalf("size %v: %#v", size, out)
				}
				selected = true
				break
			}
		}
		if !selected {
			t.Fatal(view)
		}
		for y, line := range strings.Split(view, "\n") {
			if strings.Contains(line, "[ Cancel ]") {
				_, out := c.Update(tea.MouseClickMsg{X: ansi.StringWidth(line[:strings.Index(line, "[ Cancel ]")]) + 2, Y: y, Button: tea.MouseLeft})
				if out.Action != CatalogCancel {
					t.Fatal(out)
				}
			}
		}
		if strings.Contains(view, "Navigate") || strings.Contains(view, "Tab Focus") {
			t.Fatal(view)
		}
	}
}
