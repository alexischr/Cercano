package profilechain

import (
	"cercano/source/server/internal/inference"
	"cercano/source/server/internal/modelmetadata"
	"cercano/source/server/pkg/config"
)

// accountModelFor keeps target previews and actual requests on the same model,
// including independent backup resolution. Evidence belongs to this adapter's
// account/endpoint; transport-level SupportsVision alone is never sufficient.
func accountModelFor(c config.Config, p config.CloudProfile, chatTier config.Tier, provider inference.Provider) func(string) string {
	return func(tier string) string {
		if tier == "" {
			tier = string(chatTier)
		}
		if config.Tier(tier) != config.TierVision || p.ImageModel != "" {
			return c.ModelProfiles.ResolveCloudModelForTier(p, config.Tier(tier))
		}
		evidence, ok := provider.(interface {
			ModelEvidence(string) modelmetadata.Evidence
		})
		if !ok {
			return ""
		}
		for _, model := range c.ModelProfiles.CloudImageCandidates(p, chatTier) {
			if evidence.ModelEvidence(model).Vision == modelmetadata.VisionSupported {
				return model
			}
		}
		return ""
	}
}
