package ui

import (
	"cercano/source/server/pkg/agentclient"
	"strings"
	"testing"
)

func TestCloudProviderCatalogExplicitAndProviderScoped(t *testing.T) {
	sp := &settingsPage{}
	if fields := sp.mistralProviderCatalogFields(); len(fields) != 1 {
		t.Fatalf("combined view was not opt-in: %+v", fields)
	}
	sp.cloudProviderCatalog = &agentclient.RuntimeModelCatalog{
		Models: []agentclient.RuntimeModel{
			{CatalogSource: "other", CatalogID: "other-private"},
			{CatalogSource: "mistral", CatalogID: "shared", AccountObservations: []agentclient.CatalogAccountObservation{
				{Profile: "personal", Metadata: &agentclient.CatalogMetadata{ContextLength: 4096}},
				{Profile: "work", Metadata: &agentclient.CatalogMetadata{ContextLength: 8192}},
			}},
		}, CatalogAccountStatus: []agentclient.CatalogAccountStatus{{Profile: "unavailable", Error: "discovery failed"}},
	}
	fields := sp.mistralProviderCatalogFields()
	text := ""
	for _, f := range fields {
		text += f.Label() + " " + f.Display() + "\n"
	}
	for _, want := range []string{"shared", "personal", "work", "4096", "8192", "pricing unknown", "discovery failed"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s: %s", want, text)
		}
	}
	if strings.Contains(text, "other-private") {
		t.Fatal("cross-provider catalog shown")
	}
	if strings.Count(text, "shared") != 1 {
		t.Fatal("shared model shown more than once")
	}
	if !cloudCommitNeedsAgent(classifyCloudCommit("cloud-provider-catalog", ""), false) {
		t.Fatal("catalog action not recognized")
	}
	if _, _, err := sp.commitCloud(classifyCloudCommit("cloud-provider-catalog", "")); err != nil || sp.cloudProviderCatalog != nil {
		t.Fatal("failed to hide explicit combined view")
	}
}
