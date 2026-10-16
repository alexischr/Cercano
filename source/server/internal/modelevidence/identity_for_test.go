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
