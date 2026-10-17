package ui

import (
	"cercano/source/clients/cli/internal/overlay"
	"cercano/source/server/pkg/agentclient"
	"strings"
	"testing"
)

func TestCloudProviderCatalogExplicitAndProviderScoped(t *testing.T) {
	sp := &settingsPage{}
	if fields := sp.mistralProviderCatalogFields(); len(fields) != 1 {
		t.Fatal("browser must be opt-in")
	}
	sp.cloudProviderCatalog = &agentclient.RuntimeModelCatalog{Models: []agentclient.RuntimeModel{
		{CatalogSource: "other", CatalogID: "other-private"},
		{CatalogSource: "mistral", CatalogID: "shared", AccountObservations: []agentclient.CatalogAccountObservation{{Profile: "personal", Metadata: &agentclient.CatalogMetadata{ContextLength: 4096}}, {Profile: "work", Metadata: &agentclient.CatalogMetadata{ContextLength: 8192}}}},
	}, CatalogAccountStatus: []agentclient.CatalogAccountStatus{{Profile: "unavailable", Error: "discovery failed"}}}
	c := overlay.NewCatalog("Models", "Mistral", true)
	sp.catalogPicker = &c
	sp.updateCatalogPicker()
	entries := sp.catalogPicker.VisibleEntries()
	if len(entries) != 1 || entries[0].ID != "shared" {
		t.Fatalf("model union not provider-scoped/deduplicated: %+v", entries)
	}
	var text string
	for _, d := range entries[0].Details {
		text += d.Account + " " + d.Label + " " + d.Value + "\n"
	}
	for _, want := range []string{"personal", "work", "4,096", "8,192", "Unknown"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if !strings.Contains(sp.catalogPicker.State.Message, "discovery failed") {
		t.Fatal("partial failure hidden")
	}
	if sp.cloudDirty {
		t.Fatal("browsing changed draft")
	}
}
