package ui

import (
	"cercano/source/server/pkg/agentclient"
	"cercano/source/server/pkg/config"
	"strings"
	"testing"
)

func TestCloudTemplateRecommendationsWithoutKey(t *testing.T) {
	p := cloudSamplePage()
	p.cloudView.Providers = []agentclient.CloudProvider{{ID: "mistral", Flavor: "chat_completions", Backend: "mistral", BaseURL: "https://api.mistral.ai/v1"}}
	p.selectCloudRow("template:mistral")
	for _, q := range []config.CostTier{config.CostEconomy, config.CostStandard, config.CostPremium} {
		want := config.Defaults().ModelProfiles.ResolveCloudModelForTier(config.CloudProfile{Flavor: "chat_completions", Backend: "mistral"}, q.CapabilityTier())
		if want == "" || p.cloudDraft.Effective[string(q)] != want {
			t.Fatalf("%s recommendation = %q, want %q before saving credentials", q, p.cloudDraft.Effective[string(q)], want)
		}
	}
	for _, f := range p.cloudChoiceFields(p.catalogRow()) {
		if strings.Contains(f.Display(), "No recommendation") {
			t.Fatalf("missing recommendation: %s", f.Display())
		}
	}
	if p.cloudDraft.apiKey != "" || p.cloudDirty || !p.cloudDraftNew {
		t.Fatal("recommendations changed draft credentials or save state")
	}
}
