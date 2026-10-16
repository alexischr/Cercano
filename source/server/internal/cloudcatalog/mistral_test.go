package cloudcatalog

import "testing"

func TestMistralCatalogPresetAndHost(t *testing.T) {
	found := false
	for _, p := range Catalog() {
		if p.ID == "mistral" {
			found = true
			if p.Flavor != "chat_completions" || p.BaseURL != "https://api.mistral.ai/v1" || p.Tier != TierUntested {
				t.Fatalf("preset=%+v", p)
			}
		}
	}
	if !found {
		t.Fatal("missing Mistral preset")
	}
	for _, tc := range []struct{ url, want string }{
		{"https://api.mistral.ai/v1", "mistral"}, {"https://API.MISTRAL.AI/v1", "mistral"},
		{"https://api.mistral.ai.evil.test/v1", ""}, {"https://example.test/api.mistral.ai", ""}, {"https://api.mistral.ai@evil.test/v1", ""},
	} {
		if got := ProviderIDFor(ProfileRef{Flavor: "chat_completions", BaseURL: tc.url}); got != tc.want {
			t.Fatalf("%s: %q", tc.url, got)
		}
	}
}
