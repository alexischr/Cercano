package form

import (
	"cercano/source/clients/cli/internal/theme"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestCompactSectionsUseIndependentLabelWidths(t *testing.T) {
	f := New([]Section{
		{Title: "Connection", Fields: []Field{NewReadOnly("auth", "Authentication", "Subscription sign-in", "")}},
		{Title: "Model tiers", Fields: []Field{NewRow("economy", "Economy", "model-economy", true), NewRow("image", "Image model", "model-image", true)}},
	})
	f.CompactActions = true
	p := theme.Cracker()
	view := ansi.Strip(f.View(90, p, theme.NewStyles(p)))
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "model-economy") && !strings.Contains(line, "Economy      model-economy") {
			t.Fatalf("unrelated section widened tier indent: %q", line)
		}
	}
}
