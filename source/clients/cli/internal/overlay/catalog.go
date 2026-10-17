package overlay

import (
	"sort"
	"strings"

	"cercano/source/clients/cli/internal/theme"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// CatalogAction distinguishes configuration actions from actual model IDs.
type CatalogAction int

const (
	CatalogIdle CatalogAction = iota
	CatalogSelect
	CatalogInherit
	CatalogNone
	CatalogCustom
	CatalogCancel
	CatalogRefresh
	CatalogCredentials
)

type CatalogOutcome struct {
	Action      CatalogAction
	ID, Account string
}
type CatalogDetail struct{ Label, Value string }
type CatalogEntry struct {
	ID, Account, Name string
	Aliases           []string
	Details           []CatalogDetail
	Current           bool
	DisabledReason    string
	Unconfirmed       bool
	Action            CatalogAction
}
type CatalogState struct {
	Message     string
	ActionLabel string
	Action      CatalogAction
	Busy        bool
}

// Catalog is presentation-only. Hosts own discovery, credentials and mutations.
type Catalog struct {
	Title, Scope                string
	Browse                      bool
	Entries                     []CatalogEntry
	State                       CatalogState
	input                       textinput.Model
	cursor, focus, detailOffset int
	all, compactDetail          bool
	width, height               int
	notice                      string
}

func NewCatalog(title, scope string, browse bool) Catalog {
	in := textinput.New()
	in.Placeholder = "Search models…"
	in.Focus()
	in.CharLimit = 256
	return Catalog{Title: title, Scope: scope, Browse: browse, input: in, width: 100, height: 24}
}
func (c *Catalog) Resize(w, h int) { c.width = w; c.height = h }
func (c Catalog) Query() string    { return c.input.Value() }
func (c Catalog) IsCompact() bool  { return c.width < 70 || c.height < 20 }
func (c Catalog) filtered() []CatalogEntry {
	q := strings.ToLower(strings.TrimSpace(c.input.Value()))
	type match struct {
		entry CatalogEntry
		rank  int
	}
	var found []match
	for _, e := range c.Entries {
		if !c.all && e.DisabledReason != "" && !e.Current {
			continue
		}
		rank := 3
		if e.Action != CatalogIdle {
			rank = -1
		} else if q == "" {
			rank = 0
		} else {
			for _, s := range append([]string{e.ID, e.Name}, e.Aliases...) {
				s = strings.ToLower(s)
				if s == q {
					rank = min(rank, 0)
				} else if strings.HasPrefix(s, q) {
					rank = min(rank, 1)
				} else if strings.Contains(s, q) {
					rank = min(rank, 2)
				}
			}
		}
		if rank < 3 {
			found = append(found, match{e, rank})
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].rank < found[j].rank })
	out := make([]CatalogEntry, len(found))
	for i, m := range found {
		out[i] = m.entry
	}
	return out
}
func (c Catalog) VisibleEntries() []CatalogEntry { return c.filtered() }
func (c Catalog) highlighted() (CatalogEntry, bool) {
	e := c.filtered()
	if len(e) == 0 {
		return CatalogEntry{}, false
	}
	return e[min(c.cursor, len(e)-1)], true
}

// SetEntries preserves the highlighted identity across asynchronous refreshes.
func (c *Catalog) SetEntries(entries []CatalogEntry) {
	old, had := c.highlighted()
	c.Entries = entries
	c.cursor = 0
	for i, e := range c.filtered() {
		if had && e.ID == old.ID && e.Account == old.Account && e.Action == old.Action {
			c.cursor = i
			return
		}
		if !had && e.Current {
			c.cursor = i
		}
	}
}
func (c *Catalog) setFocus(f int) {
	c.focus = (f + 4) % 4
	if c.focus == 0 {
		c.input.Focus()
	} else {
		c.input.Blur()
	}
}

// Update returns a typed intent; selection is never applied by the component.
func (c *Catalog) Update(msg tea.Msg) (tea.Cmd, CatalogOutcome) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		c.Resize(size.Width, size.Height)
		return nil, CatalogOutcome{}
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil, CatalogOutcome{}
	}
	entries := c.filtered()
	move := func(n int) { c.cursor = max(0, min(len(entries)-1, c.cursor+n)); c.detailOffset = 0; c.notice = "" }
	switch key.String() {
	case "esc":
		if c.compactDetail {
			c.compactDetail = false
			c.detailOffset = 0
			return nil, CatalogOutcome{}
		}
		return nil, CatalogOutcome{Action: CatalogCancel}
	case "ctrl+r":
		if !c.State.Busy {
			return nil, CatalogOutcome{Action: CatalogRefresh}
		}
		return nil, CatalogOutcome{}
	case "ctrl+f":
		c.setFocus(0)
		return nil, CatalogOutcome{}
	case "tab":
		c.setFocus(c.focus + 1)
		return nil, CatalogOutcome{}
	case "shift+tab":
		c.setFocus(c.focus - 1)
		return nil, CatalogOutcome{}
	case "up":
		if c.focus == 3 {
			c.detailOffset = max(0, c.detailOffset-1)
		} else {
			move(-1)
		}
		return nil, CatalogOutcome{}
	case "down":
		if c.focus == 3 {
			c.detailOffset++
		} else {
			move(1)
		}
		return nil, CatalogOutcome{}
	case "pgup", "pgdown":
		n := max(1, c.height/3)
		if key.String() == "pgup" {
			n = -n
		}
		if c.focus == 3 || c.compactDetail {
			c.detailOffset = max(0, c.detailOffset+n)
		} else {
			move(n)
		}
		return nil, CatalogOutcome{}
	case "home", "end":
		if c.focus != 0 {
			if c.focus == 3 {
				c.detailOffset = 0
				if key.String() == "end" {
					c.detailOffset = 1 << 20
				}
			} else {
				c.cursor = 0
				if key.String() == "end" {
					c.cursor = max(0, len(entries)-1)
				}
			}
			return nil, CatalogOutcome{}
		}
	case "enter":
		if c.focus == 1 {
			c.all = !c.all
			c.cursor = 0
			return nil, CatalogOutcome{}
		}
		if c.State.Action != CatalogIdle {
			return nil, CatalogOutcome{Action: c.State.Action}
		}
		e, exists := c.highlighted()
		if !exists {
			return nil, CatalogOutcome{}
		}
		if c.focus == 3 || c.Browse {
			if c.IsCompact() {
				c.compactDetail = !c.compactDetail
			}
			return nil, CatalogOutcome{}
		}
		if e.DisabledReason != "" {
			c.notice = e.DisabledReason
			return nil, CatalogOutcome{}
		}
		action := e.Action
		if action == CatalogIdle {
			action = CatalogSelect
		}
		return nil, CatalogOutcome{Action: action, ID: e.ID, Account: e.Account}
	}
	if c.focus == 0 {
		old := c.input.Value()
		var cmd tea.Cmd
		c.input, cmd = c.input.Update(msg)
		if old != c.input.Value() {
			c.cursor = 0
			c.detailOffset = 0
			c.notice = ""
		}
		return cmd, CatalogOutcome{}
	}
	return nil, CatalogOutcome{}
}
func catalogText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, ansi.Strip(s))
}
func catalogClip(s string, w int) string { return ansi.Truncate(s, max(0, w), "…") }
func catalogPad(s string, w int) string {
	s = catalogClip(s, w)
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}
func (c Catalog) details(w int) []string {
	e, ok := c.highlighted()
	if !ok {
		return []string{"No model highlighted"}
	}
	lines := []string{catalogText(e.Name), "ID: " + catalogText(e.ID)}
	if e.Account != "" {
		lines = append(lines, "Account: "+catalogText(e.Account))
	}
	if e.Current {
		lines = append(lines, "Current selection")
	}
	if e.Unconfirmed {
		lines = append(lines, "Access unconfirmed")
	}
	if e.DisabledReason != "" {
		lines = append(lines, "Unavailable: "+catalogText(e.DisabledReason))
	}
	for _, d := range e.Details {
		v := d.Value
		if v == "" {
			v = "Unknown"
		}
		lines = append(lines, catalogText(d.Label)+": "+catalogText(v))
	}
	var wrapped []string
	for _, s := range lines {
		wrapped = append(wrapped, strings.Split(ansi.Hardwrap(s, max(1, w), true), "\n")...)
	}
	return wrapped
}
func (c Catalog) View(width, height int, p theme.Palette) string {
	if width < 30 || height < 10 {
		return catalogClip("Enlarge terminal · Esc Cancel", width)
	}
	// Width includes border and padding. Every internal line is bounded explicitly.
	w := width - 4
	h := height - 2
	c.width = width
	c.height = height
	accent := lipgloss.NewStyle().Foreground(p.Accent)
	muted := lipgloss.NewStyle().Foreground(p.Muted)
	lines := []string{lipgloss.NewStyle().Foreground(p.Bright).Bold(true).Render(catalogClip(c.Title, w)), muted.Render(catalogClip(c.Scope, w))}
	c.input.SetWidth(max(1, w-2))
	lines = append(lines, catalogClip(c.input.View(), w))
	filter := "Compatible models"
	if c.all {
		filter = "All models"
	}
	if c.focus == 1 {
		filter = "› " + filter
	}
	lines = append(lines, muted.Render(catalogClip(filter+" · Tab to filters, Enter to toggle", w)))
	status := c.State.Message
	if c.State.Busy {
		status = "Loading… " + status
	}
	if status != "" {
		lines = append(lines, catalogClip(status, w))
	}
	entries := c.filtered()
	bodyH := max(1, h-len(lines)-2)
	wide := width >= 100 && height >= 24
	compact := c.IsCompact()
	listW, detailW := w, w
	if wide {
		listW = (w - 3) / 2
		detailW = w - listW - 3
	}
	detailH := 0
	listH := bodyH
	if !wide && !compact {
		detailH = max(2, bodyH/3)
		listH = bodyH - detailH - 1
	}
	if wide || c.compactDetail {
		detailH = bodyH
	}
	var list []string
	start := max(0, min(c.cursor-listH/2, len(entries)-listH))
	for i := start; i < len(entries) && len(list) < listH; i++ {
		e := entries[i]
		mark := "  "
		if e.Current {
			mark = "● "
		}
		if i == c.cursor {
			mark = "› "
		}
		name := catalogText(e.Name)
		if name == "" {
			name = catalogText(e.ID)
		}
		if e.Current {
			name += " (current)"
		}
		if e.Unconfirmed {
			name += " (?)"
		}
		if e.DisabledReason != "" {
			name += " (unavailable)"
		}
		row := catalogPad(mark+name, listW)
		if i == c.cursor {
			row = accent.Render(row)
		}
		list = append(list, row)
	}
	if len(list) == 0 {
		s := "No models available"
		if c.Query() != "" {
			s = "No matches — clear search with Ctrl+F, Ctrl+U"
		}
		list = []string{catalogClip(s, listW)}
	}
	details := c.details(detailW)
	offset := min(c.detailOffset, max(0, len(details)-detailH))
	details = details[offset:]
	if detailH > 0 && len(details) > detailH {
		details = details[:detailH]
	}
	switch {
	case compact && c.compactDetail:
		lines = append(lines, details...)
	case wide:
		for i := 0; i < bodyH; i++ {
			left, right := "", ""
			if i < len(list) {
				left = list[i]
			}
			if i < len(details) {
				right = details[i]
			}
			lines = append(lines, catalogPad(left, listW)+muted.Render(" │ ")+catalogPad(right, detailW))
		}
	default:
		lines = append(lines, list...)
		if !compact {
			lines = append(lines, muted.Render(strings.Repeat("─", w)))
			lines = append(lines, details...)
		}
	}
	footer := "↑↓ Navigate · Tab Focus · Enter Select · Esc Cancel"
	if c.Browse {
		footer = "↑↓ Navigate · Tab Focus · Enter Details · Esc Close"
	}
	if compact {
		footer = "Tab Details · Enter · Esc Back/Cancel"
	}
	if c.compactDetail {
		footer = "PgUp/PgDn Scroll · Esc Back"
	}
	if c.State.ActionLabel != "" {
		footer = "Enter " + catalogText(c.State.ActionLabel) + " · Esc Cancel"
	}
	note := c.notice
	if note == "" {
		note = "Ctrl+F Search · Ctrl+R Refresh"
	}
	if len(lines) > h-2 {
		lines = lines[:h-2]
	}
	for len(lines) < h-2 {
		lines = append(lines, "")
	}
	lines = append(lines, muted.Render(catalogClip(note, w)), muted.Render(catalogClip(footer, w)))
	for i, s := range lines {
		lines[i] = catalogClip(s, w)
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.BorderDim).Padding(0, 1).Width(width).Render(strings.Join(lines, "\n"))
}
