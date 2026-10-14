package ui

import "testing"

func TestFormatTokensGrouping(t *testing.T) {
	for _, tt := range []struct {
		n    int
		want string
	}{
		{0, "0"}, {412, "412"}, {999, "999"}, {1247, "1.2k"},
		{21400, "21.4k"}, {999999, "1,000.0k"},
		{1050000, "1,050.0k"}, {5219062, "5,219.1k"},
		{1234567890, "1,234,567.9k"},
	} {
		if got := formatTokens(tt.n); got != tt.want {
			t.Errorf("formatTokens(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
