package slash

import "testing"

func TestRestartIsDistinctFromAgentRestart(t *testing.T) {
	if ResultRestartTUI == ResultRestartAgent {
		t.Fatal("TUI and agent restart conflated")
	}
}
