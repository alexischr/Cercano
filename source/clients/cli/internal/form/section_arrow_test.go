package form

import (
	tea "charm.land/bubbletea/v2"
	"testing"
)

func TestSectionHeaderRightExpandsAndEnterStillToggles(t *testing.T) {
	expanded := false
	sections := func() []Section {
		s := Section{Title: "Optional", Header: NewButton("optional", "Optional", true), HeaderExpanded: expanded}
		if expanded {
			s.Fields = []Field{NewReadOnly("detail", "Detail", "value", "")}
		}
		return []Section{s}
	}
	f := New(sections())
	f.OnReload = sections
	commits := 0
	f.OnCommit = func(key, value string) (string, tea.Cmd, error) { commits++; expanded = !expanded; return "", nil, nil }
	f.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if !expanded || commits != 1 {
		t.Fatal("right arrow did not expand collapsed section")
	}
	f.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if !expanded || commits != 1 {
		t.Fatal("right arrow toggled already expanded section")
	}
	f.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if expanded || commits != 2 {
		t.Fatal("left arrow did not collapse expanded section")
	}
	f.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if expanded || commits != 2 {
		t.Fatal("left arrow toggled a collapsed section")
	}
	f.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !expanded || commits != 3 {
		t.Fatal("Enter no longer expands")
	}
	f.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if expanded || commits != 4 {
		t.Fatal("Enter no longer collapses")
	}
}
