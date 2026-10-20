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

// Keep this fixture aligned with the server provider catalog. The CLI cannot
// import the server internal package.
func TestAllCloudTemplateRecommendationsWithoutCredentials(t *testing.T) {
	providers := []agentclient.CloudProvider{
		{ID: "anthropic-subscription", Label: "anthropic (subscription)", Flavor: "messages", Route: "subscription", BaseURL: ""},
		{ID: "anthropic", Label: "anthropic (API key)", Flavor: "messages", BaseURL: ""},
		{ID: "openai-responses", Label: "openai (ChatGPT subscription)", Flavor: "responses", BaseURL: "https://api.openai.com/v1"},
		{ID: "openai", Label: "openai (API key)", Flavor: "chat_completions", Backend: "openai", BaseURL: "https://api.openai.com/v1"},
		{ID: "gemini", Label: "gemini", Flavor: "chat_completions", Backend: "gemini", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai"},
		{ID: "groq", Label: "groq", Flavor: "chat_completions", Backend: "groq", BaseURL: "https://api.groq.com/openai/v1"},
		{ID: "deepinfra", Label: "deepinfra", Flavor: "chat_completions", Backend: "", BaseURL: "https://api.deepinfra.com/v1/openai"},
		{ID: "together", Label: "together", Flavor: "chat_completions", Backend: "", BaseURL: "https://api.together.xyz/v1"},
		{ID: "openrouter", Label: "openrouter", Flavor: "chat_completions", Backend: "", BaseURL: "https://openrouter.ai/api/v1"},
		{ID: "deepseek", Label: "deepseek", Flavor: "chat_completions", Backend: "", BaseURL: "https://api.deepseek.com"},
		{ID: "mistral", Label: "mistral", Flavor: "chat_completions", BaseURL: "https://api.mistral.ai/v1"},
		{ID: "cerebras", Label: "cerebras", Flavor: "chat_completions", Backend: "", BaseURL: "https://api.cerebras.ai/v1"},
		{ID: "bedrock", Label: "bedrock", Flavor: "bedrock", BaseURL: ""},
	}
	expected := map[string]bool{"anthropic": true, "anthropic-subscription": true, "openai": true, "openai-responses": true, "mistral": true, "deepinfra": true, "bedrock": true}
	for _, provider := range providers {
		t.Run(provider.ID, func(t *testing.T) {
			p := cloudSamplePage()
			p.cloudView.Providers = providers
			p.selectCloudRow("template:" + provider.ID)
			profile := config.CloudProfile{Flavor: provider.Flavor, Backend: provider.Backend, Route: provider.Route, BaseURL: provider.BaseURL}
			fields := p.cloudChoiceFields(p.catalogRow())
			for _, q := range []config.CostTier{config.CostEconomy, config.CostStandard, config.CostPremium} {
				want := config.Defaults().ModelProfiles.ResolveCloudModelForTier(profile, q.CapabilityTier())
				if (want != "") != expected[provider.ID] {
					t.Fatalf("unexpected default coverage: %s = %q", q, want)
				}
				if got := p.cloudDraft.Effective[string(q)]; got != want {
					t.Fatalf("%s = %q, want %q", q, got, want)
				}
				if want != "" {
					found := false
					for _, f := range fields {
						if strings.Contains(f.Display(), want) {
							found = true
						}
					}
					if !found {
						t.Fatalf("recommendation %q missing from displayed fields", want)
					}
				}
			}
			if p.cloudDraft.apiKey != "" || p.cloudDirty || !p.cloudDraftNew {
				t.Fatal("recommendations modified credentials or draft state")
			}
		})
	}
}
