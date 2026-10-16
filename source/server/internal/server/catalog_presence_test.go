package server

import (
	"cercano/source/server/internal/catalog"
	"cercano/source/server/pkg/proto"
	"testing"
)

func TestCatalogMetadataAbsentRetirement(t *testing.T) {
	if got := catalogMetadataToProto(catalog.Metadata{}).GetRetirementAt(); got != "" {
		t.Fatalf("absent retirement became %q", got)
	}
}

func TestCatalogCapabilityOnlyMetadata(t *testing.T) {
	got := catalogModelToProto(catalog.Model{ID: "x", Source: "mistral", Metadata: catalog.Metadata{Chat: catalog.CapabilityUnsupported}}, true)
	if got.Metadata == nil || got.Metadata.Chat != proto.CatalogCapability_UNSUPPORTED {
		t.Fatalf("lost explicit unsupported capability: %v", got)
	}
}
