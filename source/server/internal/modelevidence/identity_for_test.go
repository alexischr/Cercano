package modelevidence_test

import (
	"testing"

	"cercano/source/server/internal/modelevidence"
	"cercano/source/server/internal/modelmetadata"
	"cercano/source/server/pkg/config"
)

func TestIdentityFor(t *testing.T) {
	profile := config.CloudProfile{
		Name:     "test-profile",
		Provider: "openai",
		BaseURL:  "https://api.openai.com",
		Route:    "v1",
	}

	model := "gpt-4"

	identity := modelevidence.IdentityFor(profile, model)

	expected := modelmetadata.Identity{
		Profile:  "test-profile",
		Provider: "openai",
		BaseURL:  "https://api.openai.com",
		Route:    "v1",
		Model:    "gpt-4",
	}

	if identity != expected {
		t.Errorf("IdentityFor() = %+v, want %+v", identity, expected)
	}
}

func TestDifferingIdentities(t *testing.T) {
	profile1 := config.CloudProfile{
		Name:     "profile1",
		Provider: "openai",
		BaseURL:  "https://api.openai.com",
		Route:    "v1",
	}

	profile2 := config.CloudProfile{
		Name:     "profile2",
		Provider: "openai",
		BaseURL:  "https://api.openai.com",
		Route:    "v1",
	}

	profile3 := config.CloudProfile{
		Name:     "profile1",
		Provider: "anthropic",
		BaseURL:  "https://api.anthropic.com",
		Route:    "v1",
	}

	profile4 := config.CloudProfile{
		Name:     "profile1",
		Provider: "openai",
		BaseURL:  "https://api.openai.com",
		Route:    "v2",
	}

	model := "claude-3"

	identity1 := modelevidence.IdentityFor(profile1, model)
	identity2 := modelevidence.IdentityFor(profile2, model)
	identity3 := modelevidence.IdentityFor(profile3, model)
	identity4 := modelevidence.IdentityFor(profile4, model)

	// All identities should be different
	if identity1 == identity2 {
		t.Error("Identities with different profiles should not be equal")
	}

	if identity1 == identity3 {
		t.Error("Identities with different providers should not be equal")
	}

	if identity1 == identity4 {
		t.Error("Identities with different routes should not be equal")
	}

	// Same profile + provider + route + model should be equal
	identity1_again := modelevidence.IdentityFor(profile1, model)
	if identity1 != identity1_again {
		t.Error("Same profile + provider + route + model should be equal")
	}
}

func TestIdentityForSubscriptionInferenceBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, flavor, route, provider, endpoint, want string }{
		{"chatgpt", "responses", "chatgpt", "", "", "openai"},
		{"claude", "messages", "subscription", "", "", "anthropic"},
		{"explicit chatgpt provider", "responses", "chatgpt", "custom", "", "custom"},
		{"explicit claude provider", "messages", "subscription", "custom", "", "custom"},
		{"chatgpt gateway", "responses", "chatgpt", "", "https://gateway.example/v1", ""},
		{"claude gateway", "messages", "subscription", "", "https://gateway.example/v1", ""},
		{"responses subscription", "responses", "subscription", "", "", ""},
		{"messages chatgpt", "messages", "chatgpt", "", "", ""},
		{"missing flavor", "", "chatgpt", "", "", ""},
		{"missing route", "messages", "", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := config.CloudProfile{Name: "account-key", Flavor: tc.flavor, Route: tc.route, Provider: tc.provider, BaseURL: tc.endpoint}
			want := modelmetadata.Identity{Profile: p.Name, Provider: tc.want, BaseURL: p.BaseURL, Route: p.Route, Model: "model"}
			if got := modelevidence.IdentityFor(p, "model"); got != want {
				t.Fatalf("identity=%+v want %+v", got, want)
			}
		})
	}
}
