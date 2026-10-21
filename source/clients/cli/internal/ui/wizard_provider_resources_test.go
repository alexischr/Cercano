package ui

import (
	"cercano/source/clients/cli/internal/wizard"
	"cercano/source/server/pkg/agentclient"
	"testing"
)

func TestResourceProvidersAreUnified(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.state.FlowVersion = 2
	wp.state.Step = wizard.StepResources
	wp.state.ResourcePage = 0
	wp.curProfiles = []agentclient.CloudProfileInfo{{Name: "claude", Provider: "anthropic-subscription", HasKey: true}, {Name: "deepinfra", Provider: "deepinfra", HasKey: true}}
	keys := map[string]bool{}
	for _, r := range wp.rows() {
		keys[r.Key] = true
	}
	for _, key := range []string{"connected:claude", "connected:deepinfra", "add_provider", "resource_next"} {
		if !keys[key] {
			t.Errorf("unified provider step missing %s", key)
		}
	}
	for _, key := range []string{"connect_claude", "connect_chatgpt"} {
		if keys[key] {
			t.Errorf("obsolete split provider action %s", key)
		}
	}
	pressRows(t, wp, "resource_next")
	if wp.state.ResourcePage != 2 {
		t.Fatalf("continue should show resource summary, page=%d", wp.state.ResourcePage)
	}
}
