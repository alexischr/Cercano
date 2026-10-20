package form

import tea "charm.land/bubbletea/v2"

type fieldHit struct{ index, from, to, valueX, valueY int }

// Click uses the latest render, including wrapped fields and section borders.
func (f *Form) Click(x, y int) tea.Cmd {
	fields := f.flat()
	for _, hit := range f.hits {
		if y < hit.from || y >= hit.to || x < 1 {
			continue
		}
		if _, heading := fields[hit.index].(*HeadingField); heading {
			return nil
		}
		var pending tea.Cmd
		if f.cursor != hit.index && f.cursor < len(fields) && fields[f.cursor].Editing() {
			target := fields[hit.index].Key()
			if _, text := fields[f.cursor].(*TextField); text {
				pending, _ = f.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			} else {
				f.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			}
			fields = f.flat()
			for i, field := range fields {
				if field.Key() == target {
					f.SetCursor(i)
					cmd, _ := f.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
					return tea.Batch(pending, cmd)
				}
			}
			return pending
		}
		f.SetCursor(hit.index)
		if selectField, ok := fields[hit.index].(*SelectField); ok && selectField.open {
			for _, option := range selectField.hits {
				if y-hit.valueY == option.y && x-hit.valueX >= option.x && x-hit.valueX < option.end {
					selectField.cursor = option.index
					cmd, _ := f.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
					return cmd
				}
			}
			return nil
		}
		if fields[hit.index].Editing() {
			return nil
		}
		cmd, _ := f.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		return cmd
	}
	return nil
}
