package ui

import (
	"cercano/source/clients/cli/internal/wizard"
	"cercano/source/server/pkg/agentclient"
	"strings"
	"testing"
)

func TestWizardReuseActiveAccountWithoutAuthOrWrites(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.state.Step = wizard.StepCloud
	wp.state.LocusMode = "cloud_only"
	wp.curActiveProfile = "work"
	wp.curProfiles = []agentclient.CloudProfileInfo{{Name: "work", Provider: "anthropic", EffectiveQualityModels: map[string]string{"premium": "custom-deep", "standard": "custom-main", "economy": "custom-small"}}}
	wp.hasCloudAccount = true
	wp.cursor = 0
	if wp.rows()[0].Key != "use_existing" {
		t.Fatalf("active account not offered: %+v", wp.rows())
	}
	cmd, closed := wp.selectRow()
	if cmd != nil || closed || wp.authPick || wp.state.Step != wizard.StepDone {
		t.Fatalf("reuse requested authentication or failed to advance: %+v", wp.state)
	}
	if wp.state.AuthMethod != "existing" {
		t.Fatalf("reuse not persisted: %+v", wp.state)
	}
	if choices, err := wizardCloudChoices(wp.state, nil); err != nil || choices != nil {
		t.Fatalf("reuse changed model overrides: %+v %v", choices, err)
	}
	for _, want := range []string{"work", "custom-deep", "custom-main", "custom-small"} {
		if !strings.Contains(wp.summary(), want) {
			t.Errorf("preview omits %q: %s", want, wp.summary())
		}
	}
}

func TestWizardMissingActiveAccountNotOffered(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.state.Step = wizard.StepCloud
	wp.curActiveProfile = "missing"
	wp.hasCloudAccount = true
	if wp.rows()[0].Key == "use_existing" {
		t.Fatal("offered missing account")
	}
}
