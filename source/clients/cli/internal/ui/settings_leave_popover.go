package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The same rendered geometry drives composition and mouse hit testing.
func (sp *settingsPage) settingsLeavePopover() (view string, x, y int) {
	label := "Cloud"
	if sp.scope == scopeRouting {
		label = "Routing"
	}
	width := min(54, sp.width)
	content := "Unsaved " + label + " edits.\n\n[ Discard and leave ]\n[ Keep editing ]"
	view = lipgloss.NewStyle().Foreground(sp.palette.Primary).Background(sp.palette.Surface).
		Border(lipgloss.RoundedBorder()).BorderForeground(sp.palette.Accent).
		Padding(1, 2).Width(max(1, width-2)).Render(content)
	x = max(0, (sp.width-lipgloss.Width(view))/2)
	y = max(0, (sp.viewportHeight()-lipgloss.Height(view))/2)
	return
}

func (sp *settingsPage) settingsLeaveClick(x, y int) rune {
	view, left, top := sp.settingsLeavePopover()
	lines := strings.Split(ansi.Strip(view), "\n")
	y -= top
	if y < 0 || y >= len(lines) {
		return 0
	}
	for _, action := range []struct {
		label string
		key   rune
	}{{"[ Discard and leave ]", 'y'}, {"[ Keep editing ]", 'n'}} {
		if at := strings.Index(lines[y], action.label); at >= 0 {
			start := left + ansi.StringWidth(lines[y][:at])
			if x >= start && x < start+ansi.StringWidth(action.label) {
				return action.key
			}
		}
	}
	return 0
}
