package ui

import (
	"strings"
	"testing"

	"cercano/source/clients/cli/internal/theme"
)

func TestImageAttachmentDoesNotShowCapabilityNotice(t *testing.T) {
	m := newTestModelWithPrompt()
	m.styles = theme.NewStyles(theme.Cracker())
	m.input.AddImage([]byte{1}, "image/png", "")
	parts, _ := m.composeFrame()
	rendered := strings.Join(parts, "\n")
	for _, unwanted := range []string{"image inspector", "can't see images"} {
		if strings.Contains(rendered, unwanted) {
			t.Fatalf("attached image shows misleading capability notice %q", unwanted)
		}
	}
	if len(m.input.Attachments()) != 1 {
		t.Fatal("image attachment must be retained")
	}
}
