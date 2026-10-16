package ui

import (
	"cercano/source/server/pkg/agentclient"
	"strings"
	"testing"
)

func TestCloudModelOptionsShowAccountMetadata(t *testing.T) {
	models := []agentclient.CloudModelInfo{{ID: "exact-id", DisplayName: "Friendly", Metadata: &agentclient.CatalogMetadata{ContextLength: 8192, Vision: agentclient.CatalogCapabilitySupported}, AccountObservations: []agentclient.CatalogAccountObservation{{Profile: "work"}}}}
	options := modelOptionsFromCatalog(models, "exact-id")
	if len(options) != 1 || options[0].Value != "exact-id" {
		t.Fatalf("changed selection identity: %+v", options)
	}
	for _, want := range []string{"Friendly", "8192", "vision", "pricing unknown", "work"} {
		if !strings.Contains(options[0].Label, want) {
			t.Fatalf("missing %q: %q", want, options[0].Label)
		}
	}
}

func TestCloudModelOptionsKeepUnknownPinnedID(t *testing.T) {
	options := modelOptionsFromCatalog(nil, "pinned-id")
	if len(options) != 1 || options[0].Value != "pinned-id" {
		t.Fatalf("lost pinned model: %+v", options)
	}
}
