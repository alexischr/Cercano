package modelevidence

import (
	"cercano/source/server/internal/modelmetadata"
	"cercano/source/server/pkg/config"
)

// IdentityFor builds the evidence key for a model served by a cloud profile.
//
// Every field participates: two profiles pointing at different endpoints, or
// the same endpoint over different auth routes, are different identities even
// when the model id is identical. Credentials are deliberately absent — this
// value is cached and shipped to workers.
func IdentityFor(p config.CloudProfile, model string) modelmetadata.Identity {
	provider := p.Provider
	// Built-in subscription connections omit provider and endpoint metadata.
	// Infer only these exact pairs; explicit endpoints may be unrelated gateways.
	if provider == "" && p.BaseURL == "" {
		switch {
		case p.Flavor == "responses" && p.Route == "chatgpt":
			provider = "openai"
		case p.Flavor == "messages" && p.Route == "subscription":
			provider = "anthropic"
		}
	}
	return modelmetadata.Identity{
		Profile:  p.Name,
		Provider: provider,
		BaseURL:  p.BaseURL,
		Route:    p.Route,
		Model:    model,
	}
}
