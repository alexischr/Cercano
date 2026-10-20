package overlay

import (
	"cercano/source/clients/cli/internal/theme"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"strings"
)

type catalogHit struct {
	y, from, to, index    int
	entry, search, filter bool
	action                CatalogAction
}

func (c *Catalog) click(x, y int) (tea.Cmd, CatalogOutcome) {
	for _, hit := range c.hits {
		if y != hit.y || x < hit.from || x >= hit.to {
			continue
		}
		if hit.search {
			c.setFocus(0)
			return nil, CatalogOutcome{}
		}
		if hit.filter {
			c.setFocus(1)
			if c.Browse {
				return c.Update(tea.KeyPressMsg{Code: tea.KeyRight})
			}
			return c.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		}
		if hit.entry {
			c.cursor = hit.index
			c.setFocus(2)
			return c.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		}
		return nil, CatalogOutcome{Action: hit.action}
	}
	return nil, CatalogOutcome{}
}

// Provider mode uses the same search/selection model without model-specific filters.
func (c *Catalog) providerView(width, height int, p theme.Palette) string {
	if width < 24 || height < 8 {
		return "Enlarge terminal"
	}
	c.width, c.height = width, height
	w, h := width-4, height-2
	c.input.Placeholder = "Search providers…"
	c.input.SetWidth(max(1, w-2))
	lines := []string{"Add provider", c.input.View(), ""}
	c.hits = append(c.hits, catalogHit{y: 2, from: 2, to: width - 2, search: true})
	entries := c.filtered()
	count := max(1, h-5)
	start := max(0, min(c.cursor-count/2, len(entries)-count))
	for i := start; i < len(entries) && len(lines) < h-2; i++ {
		label := "  " + catalogText(entries[i].Name)
		if i == c.cursor {
			label = "› " + catalogText(entries[i].Name)
		}
		c.hits = append(c.hits, catalogHit{y: len(lines) + 1, from: 2, to: width - 2, index: i, entry: true})
		lines = append(lines, catalogClip(label, w))
	}
	if len(entries) == 0 {
		lines = append(lines, "No providers match your search.")
	}
	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	lines = append(lines, "[ Cancel ]")
	c.hits = append(c.hits, catalogHit{y: len(lines), from: 2, to: width - 2, action: CatalogCancel})
	for i := range lines {
		lines[i] = catalogClip(lines[i], w)
	}
	return lipgloss.NewStyle().Foreground(p.Primary).Background(p.Surface).Border(lipgloss.RoundedBorder()).BorderForeground(p.Border).Padding(0, 1).Width(width).Render(strings.Join(lines, "\n"))
}
