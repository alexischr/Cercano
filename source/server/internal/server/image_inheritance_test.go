package server

import (
	"cercano/source/server/internal/modelevidence"
	"cercano/source/server/internal/modelmetadata"
	"cercano/source/server/pkg/config"
	"testing"
)

func TestImageInheritanceSnapshotIncludesProviderRecommendations(t *testing.T) {
	s, _ := newTestServer()
	c := config.Config{ActiveCloudProfile: "p", ModelProfiles: config.Defaults().ModelProfiles, CloudProfiles: []config.CloudProfile{{Name: "p", Provider: "anthropic", Flavor: "messages", TierOverrides: map[config.CostTier]string{config.CostEconomy: "custom-text", config.CostStandard: "custom-text", config.CostPremium: "custom-text"}}}}
	snapshot := s.turnModelEvidence(t.Context(), c)
	p := c.CloudProfiles[0]
	candidates := c.ModelProfiles.CloudImageCandidates(p, config.TierEveryday)
	if len(candidates) < 2 {
		t.Fatalf("no provider fallback: %v", candidates)
	}
	for _, model := range candidates[1:] {
		evidence, ok := snapshot.Lookup(modelevidence.IdentityFor(p, model))
		if !ok || evidence.Vision != modelmetadata.VisionSupported {
			t.Fatalf("worker missing image fallback evidence for %q: %+v", model, evidence)
		}
	}
	if p.ImageModel != "" || p.TierOverrides[config.CostStandard] != "custom-text" {
		t.Fatal("resolution changed explicit choices")
	}
}
