package form

import (
	"cercano/source/clients/cli/internal/theme"
	tea "charm.land/bubbletea/v2"
)

type HeadingField struct{ key, title string }

func NewHeading(key, title string) *HeadingField                       { return &HeadingField{key, title} }
func (f *HeadingField) Key() string                                    { return f.key }
func (f *HeadingField) Label() string                                  { return "" }
func (f *HeadingField) Display() string                                { return f.title }
func (f *HeadingField) Editing() bool                                  { return false }
func (f *HeadingField) Update(tea.KeyPressMsg) (tea.Cmd, bool, string) { return nil, false, "" }
func (f *HeadingField) View(_ bool, _ int, s theme.Styles) string      { return s.Accent.Render(f.title) }
