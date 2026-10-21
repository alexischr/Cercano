package ui

import (
	"strings"
	"testing"

	"cercano/source/clients/cli/internal/theme"
)

func TestVisionNoticeDoesNotClaimUnknownModelCannotSeeImages(t *testing.T) {
	m := newTestModelWithPrompt()
	m.styles = theme.NewStyles(theme.Cracker())
	m.supportsVision = false
	m.input.AddImage([]byte{1}, "image/png", "")
	if n := m.visionNotice(); !strings.Contains(n, "image inspector") || strings.Contains(n, "can't see") {
		t.Fatal("expected an inspector notice without claiming the model cannot see images")
	}
}

func TestVisionNoticeHiddenWhenSupported(t *testing.T) {
	m := newTestModelWithPrompt()
	m.styles = theme.NewStyles(theme.Cracker())
	m.supportsVision = true
	m.input.AddImage([]byte{1}, "image/png", "")
	if m.visionNotice() != "" {
		t.Fatal("no notice when the model supports vision")
	}
}

func TestVisionNoticeHiddenWhenNoImage(t *testing.T) {
	m := newTestModelWithPrompt()
	m.styles = theme.NewStyles(theme.Cracker())
	m.supportsVision = false
	if n := m.visionNotice(); n != "" {
		t.Fatalf("no notice without an attached image, got %q", n)
	}
}
