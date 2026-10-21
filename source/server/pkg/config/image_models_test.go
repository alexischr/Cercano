package config

import (
	"reflect"
	"testing"
)

func TestCloudImageCandidatesPreservePriorityAndChoices(t *testing.T) {
	m := ModelProfiles{}
	m.Cloud.Providers = map[string]VendorCostTiers{"vendor": {Economy: CostTierModel{Model: "recommended-light"}, Standard: CostTierModel{Model: "recommended-chat"}, Premium: CostTierModel{Model: "recommended-deep"}}}
	p := CloudProfile{Provider: "vendor", TierOverrides: map[CostTier]string{CostPremium: "selected-deep"}}
	want := []string{"selected-deep", "recommended-chat", "recommended-deep", "recommended-light"}
	if got := m.CloudImageCandidates(p, TierMostCapable); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if p.TierOverrides[CostPremium] != "selected-deep" {
		t.Fatal("mutated profile")
	}
	p.ImageModel = "explicit"
	if got := m.CloudImageCandidates(p, TierMostCapable); !reflect.DeepEqual(got, []string{"explicit"}) {
		t.Fatalf("override not authoritative: %v", got)
	}
	p = CloudProfile{Provider: "unknown"}
	if got := m.CloudImageCandidates(p, TierEveryday); len(got) != 0 {
		t.Fatalf("invented provider default: %v", got)
	}
}
