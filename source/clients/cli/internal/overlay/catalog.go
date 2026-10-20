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
type CatalogDetail struct{ Label, Value, Account string }
type CatalogEntry struct {
	ID, Account, Name string
	Aliases           []string
	Accounts          []string
	Details           []CatalogDetail
	Current           bool
	DisabledReason    string
	Unconfirmed       bool
	Action            CatalogAction
}
type CatalogState struct {
	Message      string
	Notification string // Persistent result of the most recent discovery request.
	ActionLabel  string
	Action       CatalogAction
	Busy         bool
}

// Catalog is presentation-only. Hosts own discovery, credentials and mutations.
type Catalog struct {
	Title, Scope                string
	Browse                      bool
	Providers                   bool
	hits                        []catalogHit
	Entries                     []CatalogEntry
	State                       CatalogState
	input                       textinput.Model
	cursor, focus, detailOffset int
	all, compactDetail          bool
	width, height               int
	notice                      string
	account                     string
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
		if e.Action == CatalogCustom {
			continue
		}
		if c.account != "" {
			found := e.Account == c.account
			for _, a := range e.Accounts {
				found = found || a == c.account
			}
			if !found {
				continue
			}
		}
		if !c.all && e.DisabledReason != "" && !e.Current {
			continue
		}
		rank := 3
		if e.Action != CatalogIdle {
			rank = -1
			if q != "" || e.Action == CatalogCustom {
				rank = 4
			}
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
		if rank < 3 || e.Action != CatalogIdle {
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
func (c Catalog) hasCustom() bool {
	for _, e := range c.Entries {
		if e.Action == CatalogCustom {
			return true
		}
	}
	return false
}

func (c *Catalog) setFocus(f int) {
	targets := []int{0, 1, 2, 3, 5, 6}
	if c.hasCustom() {
		targets = []int{0, 1, 2, 3, 4, 5, 6}
	}
	if !c.Browse {
		targets = append(targets, 7)
	}
	if c.Providers {
		targets = []int{0, 2, 6}
	}
	if f > c.focus {
		c.focus = targets[0]
		for _, target := range targets {
			if target >= f {
				c.focus = target
				break
			}
		}
	} else if f < c.focus {
		c.focus = targets[len(targets)-1]
		for i := len(targets) - 1; i >= 0; i-- {
			if targets[i] <= f {
				c.focus = targets[i]
				break
			}
		}
	} else {
		c.focus = f
	}
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
	if mouse, ok := msg.(tea.MouseClickMsg); ok {
		return c.click(mouse.X, mouse.Y)
	}
	if wheel, ok := msg.(tea.MouseWheelMsg); ok {
		code := tea.KeyDown
		if wheel.Button == tea.MouseWheelUp {
			code = tea.KeyUp
		}
		return c.Update(tea.KeyPressMsg{Code: code})
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil, CatalogOutcome{}
	}
	entries := c.filtered()
	move := func(n int) { c.cursor = max(0, min(len(entries)-1, c.cursor+n)); c.detailOffset = 0; c.notice = "" }
	switch key.String() {
	case "esc":

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
	case "left", "right":
		if c.focus == 1 && c.Browse {
			accounts := c.accounts()
			i := 0
			for j, a := range accounts {
				if a == c.account {
					i = j
				}
			}
			n := 1
			if key.String() == "left" {
				n = -1
			}
			c.account = accounts[(i+n+len(accounts))%len(accounts)]
			c.cursor = 0
			c.detailOffset = 0
			return nil, CatalogOutcome{}
		}
	case "enter":
		switch c.focus {
		case 4:
			return nil, CatalogOutcome{Action: CatalogCustom}
		case 5:
			if !c.State.Busy {
				return nil, CatalogOutcome{Action: CatalogRefresh}
			}
			return nil, CatalogOutcome{}
		case 6:
			return nil, CatalogOutcome{Action: CatalogCancel}
		}
		if c.State.Action == CatalogCredentials {
			return nil, CatalogOutcome{Action: CatalogCredentials}
		}
		if c.focus == 1 {
			c.all = !c.all
			c.cursor = 0
			return nil, CatalogOutcome{}
		}
		if c.State.Action != CatalogIdle && (c.State.Action == CatalogCredentials || len(entries) == 0) {
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
	lines := []string{catalogText(e.Name)}
	if e.Action == CatalogIdle {
		lines = append(lines, "", catalogText(e.ID), "")
	}
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
		if c.account != "" && d.Account != "" && d.Account != c.account {
			continue
		}
		v := d.Value
		if v == "" {
			v = "Unknown"
		}
		label := d.Label
		if d.Account != "" {
			label = d.Account + " · " + label
		}
		lines = append(lines, catalogText(label)+": "+catalogText(v))
	}
	var wrapped []string
	for _, s := range lines {
		wrapped = append(wrapped, strings.Split(ansi.Hardwrap(s, max(1, w), true), "\n")...)
	}
	return wrapped
}
func (c *Catalog) View(width, height int, p theme.Palette) string {
	c.hits = nil
	if c.Providers {
		return c.providerView(width, height, p)
	}
	if width < 30 || height < 10 {
		return catalogClip("Enlarge terminal", width)
	}
	// Width includes border and padding. Every internal line is bounded explicitly.
	w := width - 4
	h := height - 2
	c.width = width
	c.height = height
	accent := lipgloss.NewStyle().Foreground(p.Accent).Background(p.Surface)
	muted := lipgloss.NewStyle().Foreground(p.Primary).Background(p.Surface)
	lines := []string{lipgloss.NewStyle().Foreground(p.Bright).Bold(true).Render(catalogClip(catalogText(c.Title), w)), muted.Render(catalogClip(catalogText(c.Scope), w))}
	lines = append(lines, "", "Search models")
	c.input.SetWidth(max(1, w-4))
	lines = append(lines, catalogClip(c.input.View(), w), "")
	c.hits = append(c.hits, catalogHit{y: 5, from: 2, to: width - 2, search: true})
	filter := "Compatible models"
	if c.all {
		filter = "All models"
	}
	if c.Browse {
		account := c.account
		if account == "" {
			account = "All accounts"
		}
		filter = account
	}
	if c.focus == 1 {
		filter = "› " + filter
	}
	lines = append(lines, muted.Render(catalogClip(filter, w)), "")
	c.hits = append(c.hits, catalogHit{y: 7, from: 2, to: width - 2, filter: true})
	status := catalogText(c.State.Message)
	if c.State.Busy && status == "" {
		status = "Loading available models…"
	}
	if status != "" {
		wrapped := strings.Split(ansi.Hardwrap(status, w, true), "\n")
		lines = append(lines, wrapped[:min(len(wrapped), max(1, h/4))]...)
	}
	if c.State.Action == CatalogCredentials {
		content := []string{lines[0], lines[1], "", accent.Render("Available models")}
		content = append(content, strings.Split(ansi.Hardwrap(status, w, true), "\n")...)
		c.hits = append(c.hits, catalogHit{y: len(content) + 2, from: 2, to: width - 2, action: CatalogCredentials})
		content = append(content, "", accent.Render("[ "+catalogText(c.State.ActionLabel)+" ]"), "", "Your current selection stays unchanged.")
		for len(content) < h-1 {
			content = append(content, "")
		}
		if len(content) > h-1 {
			content = content[:h-1]
		}
		content = append(content, "[ Cancel ]")
		c.hits = append(c.hits, catalogHit{y: len(content), from: 2, to: width - 2, action: CatalogCancel})
		for i, line := range content {
			content[i] = catalogClip(line, w)
		}
		return lipgloss.NewStyle().Foreground(p.Primary).Background(p.Surface).Border(lipgloss.RoundedBorder()).BorderForeground(p.BorderDim).Padding(0, 1).Width(width).Render(strings.Join(content, "\n"))
	}
	entries := c.filtered()
	count := 0
	for _, e := range entries {
		if e.Action == CatalogIdle {
			count++
		}
	}
	if strings.TrimSpace(c.Query()) != "" && count == 0 {
		lines = append(lines, catalogClip("No models match your search.", w))
	}
	footerRows := 4
	if w < 48 && c.hasCustom() {
		footerRows = 5
	}
	bodyH := max(1, h-len(lines)-footerRows)
	wide := width >= 100 && height >= 24
	compact := c.IsCompact()
	listW, detailW := w, w
	if wide {
		listW = (w - 3) / 2
		detailW = w - listW - 3
	}
	heading := "MODELS"
	if c.focus == 2 {
		heading = "› MODELS"
	}
	if compact && c.compactDetail {
		heading = "DETAILS"
	}
	if wide {
		right := "DETAILS"
		if c.focus == 3 {
			right = "› DETAILS"
		}
		heading = catalogPad(heading, listW) + " │ " + right
	}
	lines = append(lines, muted.Render(catalogClip(heading, w)))
	bodyH = max(1, bodyH-1)
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
			row = lipgloss.NewStyle().Foreground(p.Bright).Background(p.SelectionBg).Render(row)
		}
		if !c.compactDetail {
			c.hits = append(c.hits, catalogHit{y: len(lines) + len(list) + 1, from: 2, to: 2 + listW, index: i, entry: true})
		}
		list = append(list, row)
	}
	if len(list) == 0 {
		s := "No models available"
		if c.Query() != "" {
			s = "No matches"
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
	// Fixed footer: each action is reachable by Tab and uses the same hit box
	// for pointer activation. No keyboard-help text competes with the content.
	if len(lines) > h-footerRows {
		lines = lines[:h-footerRows]
	}
	for len(lines) < h-footerRows {
		lines = append(lines, "")
	}
	notification := c.State.Notification
	if c.notice != "" {
		notification = c.notice
	}
	if c.State.Busy {
		notification = "Refreshing available models…"
	}
	lines = append(lines, muted.Render(strings.Repeat("─", w)), catalogClip(catalogText(notification), w))
	type button struct {
		label  string
		focus  int
		action CatalogAction
	}
	buttons := []button{}
	if c.hasCustom() {
		buttons = append(buttons, button{"Custom ID…", 4, CatalogCustom})
	}
	refreshLabel := "Refresh"
	if c.State.Busy {
		refreshLabel = "Refreshing…"
	}
	buttons = append(buttons, button{refreshLabel, 5, CatalogRefresh}, button{"Cancel", 6, CatalogCancel})
	if !c.Browse {
		buttons = append(buttons, button{"Use", 7, CatalogSelect})
	}
	footer := ""
	footerY := h - 1
	if footerRows == 5 {
		footerY--
	}
	for _, b := range buttons {
		label := "[ " + b.label + " ]"
		if w < 60 {
			label = "[" + b.label + "]"
		}
		if ansi.StringWidth(footer)+ansi.StringWidth(label) > w {
			lines = append(lines, footer)
			footer = ""
			footerY++
		}
		x := ansi.StringWidth(footer)
		style := muted
		if c.focus == b.focus {
			style = accent.Bold(true).Underline(true)
		}
		footer += style.Render(label) + " "
		if b.focus == 4 && w >= 60 {
			remaining := ansi.StringWidth("[ " + refreshLabel + " ] [ Cancel ]")
			if !c.Browse {
				remaining += len(" [ Use ]")
			}
			footer += strings.Repeat(" ", max(0, w-ansi.StringWidth(footer)-remaining))
		}
		c.hits = append(c.hits, catalogHit{y: footerY, from: 2 + x, to: 2 + x + ansi.StringWidth(label), action: b.action})
	}
	lines = append(lines, catalogClip(strings.TrimRight(footer, " "), w), "")
	for i, s := range lines {
		lines[i] = catalogClip(s, w)
	}
	return lipgloss.NewStyle().Foreground(p.Primary).Background(p.Surface).Border(lipgloss.RoundedBorder()).BorderForeground(p.BorderDim).Padding(0, 1).Width(width).Render(strings.Join(lines, "\n"))
}

// Paste consumes pasted input only in search; it never forwards it to a form.
func (c *Catalog) Paste(s string) {
	if c.focus != 0 {
		return
	}
	v := []rune(c.input.Value() + catalogText(s))
	if len(v) > 256 {
		v = v[:256]
	}
	c.input.SetValue(string(v))
	c.input.CursorEnd()
	c.cursor = 0
	c.detailOffset = 0
}

func (c Catalog) accounts() []string {
	seen := map[string]bool{"": true}
	out := []string{""}
	for _, e := range c.Entries {
		for _, a := range append([]string{e.Account}, e.Accounts...) {
			if !seen[a] {
				out = append(out, a)
				seen[a] = true
			}
		}
	}
	sort.Strings(out[1:])
	return out
}
