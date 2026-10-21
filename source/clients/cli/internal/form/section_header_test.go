package form

import (
	"strings"
	"testing"

	"cercano/source/clients/cli/internal/theme"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestSectionHeaderKeyboardMouseAndFocus(t *testing.T) {
	expanded := false
	sections := func() []Section {
		label := "▸ Model tiers"
		var fields []Field
		if expanded {
			label = "▾ Model tiers"
			fields = []Field{NewRow("economy", "Economy", "economy-model", true)}
		}
		return []Section{
			{Title: "Connection", Fields: []Field{NewReadOnly("account", "Account", "work", "")}},
			{Title: "Model tiers", Header: NewButton("models", label, true), Fields: fields},
			{Title: "Advanced settings", Header: NewButton("advanced", "▸ Advanced settings", true)},
		}
	}
	f := New(sections())
	f.OnReload = sections
	f.OnCommit = func(key, value string) (string, tea.Cmd, error) {
		if key == "models" {
			expanded = !expanded
		}
		return "", nil, nil
	}
	f.SetCursor(1)
	f.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !expanded || f.flat()[f.Cursor()].Key() != "models" {
		t.Fatal("header failed to expand or keep focus")
	}
	f.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if f.flat()[f.Cursor()].Key() != "economy" {
		t.Fatal("expanded fields missing from keyboard navigation")
	}
	p := theme.Cracker()
	view := ansi.Strip(f.View(90, p, theme.NewStyles(p)))
	if line := strings.Split(view, "\n")[f.FocusedLine()]; !strings.Contains(line, "Economy") {
		t.Fatalf("focus line mismatch: %s", line)
	}
	headerLine := -1
	for _, hit := range f.hits {
		if hit.index == 1 {
			headerLine = hit.from
			break
		}
	}
	if headerLine < 0 {
		t.Fatal("section header has no mouse hit")
	}
	f.Click(3, headerLine)
	if expanded || len(f.flat()) != 3 || f.flat()[f.Cursor()].Key() != "models" {
		t.Fatal("click did not collapse the whole section or preserve header focus")
	}
	view = ansi.Strip(f.View(90, p, theme.NewStyles(p)))
	if strings.Contains(view, "economy-model") || strings.Count(view, "Model tiers") != 1 {
		t.Fatal("collapsed section leaked body or duplicated heading")
	}
	// Collapsed headers still retain accurate hit/focus locations after the layout shrinks.
	for _, hit := range f.hits {
		if hit.index == 2 {
			f.Click(3, hit.from)
			break
		}
	}
	if f.flat()[f.Cursor()].Key() != "advanced" {
		t.Fatal("following header mouse coordinates shifted")
	}
}

func TestRelatedCompactSectionsShareColumns(t *testing.T) {
	f := New([]Section{
		{Title: "Connection", Fields: []Field{NewReadOnly("auth", "Authentication", "subscription-value", "")}},
		{Title: "Model tiers", Fields: []Field{NewRow("economy", "Economy", "economy-value", true)}},
	})
	f.CompactActions = true
	f.AlignSections = true
	p := theme.Cracker()
	view := ansi.Strip(f.View(90, p, theme.NewStyles(p)))
	cols := map[string]int{}
	for _, line := range strings.Split(view, "\n") {
		for _, value := range []string{"subscription-value", "economy-value"} {
			if i := strings.Index(line, value); i >= 0 {
				cols[value] = ansi.StringWidth(line[:i])
			}
		}
	}
	if len(cols) != 2 || cols["subscription-value"] != cols["economy-value"] {
		t.Fatalf("related sections use inconsistent value columns: %v\n%s", cols, view)
	}
}
