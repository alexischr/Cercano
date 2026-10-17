package ui

import (
	"cercano/source/server/pkg/agentclient"
	"strings"
	"testing"
)

func TestCloudCatalogKeepsMetadataOutOfLabel(t *testing.T) {
	m := agentclient.CloudModelInfo{ID: "exact-id", DisplayName: "Friendly", Metadata: &agentclient.CatalogMetadata{ContextLength: 8192, Vision: agentclient.CatalogCapabilitySupported}}
	entry := catalogEntry(m, "work")
	if entry.ID != "exact-id" || entry.Name != "Friendly" || entry.Account != "work" {
		t.Fatalf("changed identity or inflated label: %+v", entry)
	}
	var text string
	for _, d := range entry.Details {
		text += d.Label + ": " + d.Value + "\n"
	}
	for _, want := range []string{"8,192", "Vision: Supported", "Input / 1M tokens: Unknown", "Output / 1M tokens: Unknown"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
}
func TestCloudCatalogKeepsUnknownPinnedID(t *testing.T) {
	p := draftTestPage()
	p.catalogField = "cloud-quality-standard"
	p.cloudDraft.Choices.TierOverrides["standard"] = "pinned-id"
	for _, e := range p.pickerEntries() {
		if e.ID == "pinned-id" {
			if !e.Current || !e.Unconfirmed || e.Account != p.cloudDraft.Name {
				t.Fatal("pinned identity/access status lost")
			}
			return
		}
	}
	t.Fatal("configured ID disappeared")
}
