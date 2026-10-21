package config

// CloudImageCandidates returns account-local image-input candidates in priority
// order. An explicit image override is authoritative. Otherwise use the selected
// chat quality first, followed by provider recommendations, never another
// account's model. Callers MUST confirm image capability before automatic use.
// This is derived configuration: it never writes an ImageModel override.
func (m ModelProfiles) CloudImageCandidates(p CloudProfile, chatTier Tier) []string {
	if p.ImageModel != "" {
		return []string{p.ImageModel}
	}
	var out []string
	add := func(model string) {
		if model == "" {
			return
		}
		for _, v := range out {
			if v == model {
				return
			}
		}
		out = append(out, model)
	}
	add(m.ResolveCloudModelForTier(p, chatTier))
	add(m.ResolveCloudModelForTier(p, TierVision))
	p.TierOverrides = nil
	for _, tier := range []Tier{TierEveryday, TierMostCapable, TierFastLight} {
		add(m.ResolveCloudModelForTier(p, tier))
	}
	return out
}
