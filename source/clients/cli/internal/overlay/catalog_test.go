package overlay

import (
	"cercano/source/clients/cli/internal/theme"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func catKey(c *Catalog, code rune) CatalogOutcome {
	_, o := c.Update(tea.KeyPressMsg{Code: code})
	return o
}
func catType(c *Catalog, s string) {
	for _, r := range s {
		c.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}
func TestCatalogSearchAndCancel(t *testing.T) {
	c := NewCatalog("Choose", "account", false)
	c.SetEntries([]CatalogEntry{{ID: "z-glm-x", Name: "Substring"}, {ID: "glm-prefix", Name: "Prefix"}, {ID: "glm", Name: "Exact"}})
	catType(&c, "glm")
	v := c.VisibleEntries()
	if len(v) != 3 || v[0].ID != "glm" || v[1].ID != "glm-prefix" {
		t.Fatalf("ranking: %+v", v)
	}
	if o := catKey(&c, tea.KeyEscape); o.Action != CatalogCancel || c.Query() != "glm" {
		t.Fatal("Escape must cancel, not clear the query")
	}
	c = NewCatalog("Choose", "", false)
	catType(&c, "jkqr")
	if c.Query() != "jkqr" {
		t.Fatal("ordinary letters intercepted")
	}
}
func TestCatalogIdentityAndActions(t *testing.T) {
	c := NewCatalog("Choose", "", false)
	c.SetEntries([]CatalogEntry{{ID: "same", Account: "work", Name: "Work"}, {ID: "same", Account: "personal", Name: "Personal", Current: true}})
	o := catKey(&c, tea.KeyEnter)
	if o.ID != "same" || o.Account != "personal" {
		t.Fatalf("lost account identity: %+v", o)
	}
	c.SetEntries([]CatalogEntry{{ID: "new", Name: "New"}, {ID: "same", Account: "personal", Name: "Personal"}})
	if o = catKey(&c, tea.KeyEnter); o.Account != "personal" {
		t.Fatal("refresh lost highlight")
	}
	for _, a := range []CatalogAction{CatalogInherit, CatalogNone, CatalogCustom} {
		c = NewCatalog("Choose", "", false)
		c.SetEntries([]CatalogEntry{{Name: "Action", Action: a}})
		if catKey(&c, tea.KeyEnter).Action != a {
			t.Fatal("conflated special action")
		}
	}
}
func TestCatalogDisabledAndBrowse(t *testing.T) {
	c := NewCatalog("Choose", "", false)
	c.SetEntries([]CatalogEntry{{ID: "bad", Name: "Bad", DisabledReason: "Vision unknown"}})
	if len(c.VisibleEntries()) != 0 {
		t.Fatal("ineligible model visible by default")
	}
	catKey(&c, tea.KeyTab)
	catKey(&c, tea.KeyEnter)
	catKey(&c, tea.KeyTab)
	if catKey(&c, tea.KeyEnter).Action != CatalogIdle || c.notice != "Vision unknown" {
		t.Fatal("ineligible selection not blocked")
	}
	c.Browse = true
	c.Entries[0].DisabledReason = ""
	if catKey(&c, tea.KeyEnter).Action != CatalogIdle {
		t.Fatal("browse mutated selection")
	}
}
func TestCatalogCompactDetailsAndScroll(t *testing.T) {
	c := NewCatalog("Choose", "", false)
	c.Resize(60, 18)
	c.SetEntries([]CatalogEntry{{ID: "exact", Name: "Model", Details: []CatalogDetail{{"Long", strings.Repeat("readable ", 80)}}}})
	for i := 0; i < 3; i++ {
		catKey(&c, tea.KeyTab)
	}
	catKey(&c, tea.KeyEnter)
	if !c.compactDetail {
		t.Fatal("compact Details unreachable")
	}
	catKey(&c, tea.KeyPgDown)
	if c.detailOffset == 0 {
		t.Fatal("details not scrollable")
	}
	if catKey(&c, tea.KeyEscape).Action != CatalogIdle || c.compactDetail {
		t.Fatal("Escape must return from details")
	}
	if catKey(&c, tea.KeyEscape).Action != CatalogCancel {
		t.Fatal("Escape must close picker")
	}
}
func TestCatalogViewportBounds(t *testing.T) {
	for _, size := range [][2]int{{120, 36}, {100, 24}, {80, 24}, {60, 18}, {25, 8}} {
		c := NewCatalog("Choose a model", "Mistral / Work", false)
		c.SetEntries([]CatalogEntry{{ID: strings.Repeat("long-model-", 40), Name: "模型 — Long model", Details: []CatalogDetail{{"Capabilities", "Unknown"}, {"Aliases", strings.Repeat("alias ", 50)}}}})
		out := c.View(size[0], size[1], theme.Palette{})
		lines := strings.Split(out, "\n")
		if len(lines) > size[1] {
			t.Fatalf("%v: height %d", size, len(lines))
		}
		for _, l := range lines {
			if ansi.StringWidth(l) > size[0] {
				t.Fatalf("%v: width %d: %q", size, ansi.StringWidth(l), l)
			}
		}
	}
}
func TestCatalogStateAction(t *testing.T) {
	c := NewCatalog("Choose", "", false)
	c.State = CatalogState{Message: "Add an API key to load available models.", Action: CatalogCredentials, ActionLabel: "Add API key"}
	if catKey(&c, tea.KeyEnter).Action != CatalogCredentials {
		t.Fatal("credential action missing")
	}
	out := ansi.Strip(c.View(100, 24, theme.Palette{}))
	if !strings.Contains(out, "Add an API key") {
		t.Fatal("missing guidance")
	}
}
