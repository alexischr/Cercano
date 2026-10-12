package ui

import (
	tea "charm.land/bubbletea/v2"
	"testing"
)

func TestTUIRestartRunsResumeHydrationAtInit(t *testing.T) {
	m := New(nil, false)
	type resumed struct{}
	m.restartInit = func() tea.Msg { return resumed{} }
	batch, ok := m.Init()().(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatal("missing initialization batch")
	}
	if _, ok := batch[0]().(resumed); !ok {
		t.Fatal("restart hydration command was discarded")
	}
}
