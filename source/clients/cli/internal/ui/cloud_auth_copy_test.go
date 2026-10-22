package ui

import (
	"strings"
	"testing"

	"cercano/source/server/pkg/agentclient"
)

func TestSubscriptionButtonsReplaceRedundantAuthLabel(t *testing.T) {
	sp := cloudSamplePage()
	sp.cloudView.Providers = append(sp.cloudView.Providers, agentclient.CloudProvider{ID: "anthropic-subscription", Label: "Claude", Flavor: "messages", Route: "subscription"})
	sp.selectCloudRow("template:anthropic-subscription")
	fields := map[string]bool{}
	for _, field := range sp.cloudDetailFields(sp.catalogRow()) {
		fields[field.Key()] = true
	}
	if !fields["cloud-signin-claude"] || !fields["cloud-auth-switch"] {
		t.Fatal("fixture lacks subscription sign-in/switch actions")
	}
	if fields["cloud-route"] {
		t.Fatal("Authentication / Subscription sign-in redundantly repeats the explicit buttons")
	}
}

func TestCapabilityBulletsHaveDistinctLabels(t *testing.T) {
	_, client := newScenario(t)
	wp := resourceWizard(t, client)
	pressRows(t, wp, "resource_next")
	text := wp.stepDesc()
	for _, name := range []string{"Cloud", "Local models"} {
		if !strings.Contains(text, "* **"+name+"** - ") {
			t.Fatalf("missing formatted %s bullet: %s", name, text)
		}
	}
	if strings.Contains(wp.View(), "**") {
		t.Fatal("raw Markdown leaked into screen")
	}
}

func TestDirectAPIProviderKeepsItsCredentialControls(t *testing.T) {
	sp := cloudSamplePage()
	sp.selectCloudRow("template:anthropic")
	fields := map[string]bool{}
	for _, field := range sp.cloudDetailFields(sp.catalogRow()) {
		fields[field.Key()] = true
	}
	if !fields["cloud-key"] || !fields["cloud-auth-switch"] || fields["cloud-signin-claude"] {
		t.Fatalf("API-key authentication controls changed: %v", fields)
	}
}
