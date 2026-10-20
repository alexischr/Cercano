package form

import (
	"cercano/source/clients/cli/internal/theme"
	tea "charm.land/bubbletea/v2"
	"testing"
)

func TestMouseSelectAndWrappedField(t *testing.T) {
	for _, width := range []int{30, 60, 100} {
		p := theme.Cracker()
		s := theme.NewStyles(p)
		selectField := NewSelect("account", "Account", []Option{{"Personal", "personal"}, {"Work", "work"}}, "personal")
		f := New([]Section{{Title: "Connection", Fields: []Field{selectField, NewText("key", "API key", "", "Paste API key")}}})
		selected := ""
		f.OnCommit = func(k, v string) (string, tea.Cmd, error) { selected = v; return "", nil, nil }
		f.View(width, p, s)
		f.Click(4, f.hits[0].from)
		if !selectField.open {
			t.Fatal("select did not open")
		}
		f.View(width, p, s)
		option := selectField.hits[1]
		hit := f.hits[0]
		if width == 30 && option.y != 1 {
			t.Fatalf("stacked Work option has wrong hit line: %#v", option)
		}
		f.Click(hit.valueX+option.x, hit.valueY+option.y)
		if selected != "work" {
			t.Fatalf("width %d: selected %q", width, selected)
		}
		f.View(width, p, s)
		f.Click(5, f.hits[1].from)
		if !f.flat()[1].Editing() {
			t.Fatal("text editor did not open")
		}

	}
}
