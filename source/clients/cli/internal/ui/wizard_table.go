package ui

import (
	"strings"

	"cercano/source/clients/cli/internal/wizard"
	"github.com/charmbracelet/x/ansi"
)

func wizardCellLines(text string, width int) []string {
	return strings.Split(ansi.Hardwrap(ansi.Wrap(text, max(1, width), ""), max(1, width), false), "\n")
}

func (wp *wizardPage) renderWizardBullet(name, description string) string {
	width := wp.width
	if width <= 0 {
		width = 80
	}
	prefix := wp.styles.Accent.Render("  * ") + wp.styles.Accent.Bold(true).Render(name) + wp.styles.Primary.Render(" - ")
	indent := ansi.StringWidth(prefix)
	if indent+8 >= width {
		lines := wizardCellLines("  * "+name, width)
		var b strings.Builder
		for i, line := range lines {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(wp.styles.Accent.Bold(true).Render(line))
		}
		padding := min(4, max(0, width-1))
		for _, line := range wizardCellLines(description, width-padding) {
			b.WriteByte('\n')
			b.WriteString(strings.Repeat(" ", padding) + wp.styles.Primary.Render(line))
		}
		return b.String()
	}
	lines := wizardCellLines(description, width-indent)
	var b strings.Builder
	b.WriteString(prefix + wp.styles.Primary.Render(lines[0]))
	for _, line := range lines[1:] {
		b.WriteString("\n" + strings.Repeat(" ", indent) + wp.styles.Primary.Render(line))
	}
	return b.String()
}

// Independently wrap both columns; a long action cannot move its description
// into the label or shift subsequent continuation lines. Narrow terminals use
// stacked rows instead of squeezing either column into unreadable fragments.
func (wp *wizardPage) renderWizardRows() string {
	rows := wp.rows()
	width := wp.width
	if width <= 0 {
		width = 80
	}
	var b strings.Builder
	labelWidth := 16
	for _, row := range rows {
		labelWidth = max(labelWidth, ansi.StringWidth(row.Label))
	}
	labelWidth = min(labelWidth, min(42, (width-5)/2))
	detailWidth := width - labelWidth - 5
	table := width >= 60
	if table {
		title := "Action"
		if wp.state.Step == wizard.StepPreference {
			title = "Configuration"
		}
		b.WriteString("  " + wp.styles.Accent.Bold(true).Render(title) + strings.Repeat(" ", labelWidth-ansi.StringWidth(title)) + wp.styles.Muted.Render(" │ ") + wp.styles.Accent.Bold(true).Render("Description") + "\n")
		b.WriteString(wp.styles.Muted.Render("  "+strings.Repeat("─", labelWidth)+"─┼─"+strings.Repeat("─", detailWidth)) + "\n")
	}
	for i, row := range rows {
		caret := "  "
		style := wp.styles.Primary
		if row.Disabled {
			style = wp.styles.Muted
		} else if i == wp.cursor {
			style = wp.styles.Bright
			caret = wp.styles.SelectionCaret.Render("▶ ")
		}
		if !table {
			for n, line := range wizardCellLines(row.Label, width-2) {
				marker := "  "
				if n == 0 {
					marker = caret
				}
				b.WriteString(marker + style.Render(line) + "\n")
			}
			if row.Annotation != "" {
				padding := min(4, max(0, width-1))
				for _, line := range wizardCellLines(row.Annotation, width-padding) {
					b.WriteString(strings.Repeat(" ", padding) + wp.styles.Info.Render(line) + "\n")
				}
			}
			if i < len(rows)-1 {
				b.WriteByte('\n')
			}
			continue
		}
		names, details := wizardCellLines(row.Label, labelWidth), wizardCellLines(row.Annotation, detailWidth)
		for n := 0; n < max(len(names), len(details)); n++ {
			label, detail := "", ""
			if n < len(names) {
				label = names[n]
			}
			if n < len(details) {
				detail = details[n]
			}
			marker := "  "
			if n == 0 {
				marker = caret
			}
			b.WriteString(marker + style.Render(label) + strings.Repeat(" ", labelWidth-ansi.StringWidth(label)) + wp.styles.Muted.Render(" │ ") + wp.styles.Info.Render(detail) + "\n")
		}
	}
	return b.String()
}
