package ui

import (
	"cercano/source/clients/cli/internal/wizard"
	"cercano/source/server/pkg/agentclient"
	"cercano/source/server/pkg/config"
	"testing"
)

func TestWizardRecommendedEntryPreviewsExistingAccount(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.back() // leave customization to exercise the actual entry screen
	wp.hasCloudAccount = true
	wp.curActiveProfile = "saved"
	wp.curProfiles = []agentclient.CloudProfileInfo{{Name: "saved", Provider: "anthropic"}}
	rows := wp.rows()
	if len(rows) != 4 || rows[0].Key != "cloud_only" {
		t.Fatalf("entry should offer recommendation and customization, got %+v", rows)
	}
	applied := false
	wp.applyFn = func() error { applied = true; return nil }
	wp.selectRow()
	if wp.state.Step != wizard.StepDone || wp.state.LocusMode != "cloud_only" || wp.state.AuthMethod != "existing" {
		t.Fatalf("expected no-download account preview, got %+v", wp.state)
	}
	if applied {
		t.Fatal("recommendation applied before confirmation")
	}
}

func TestWizardRecommendedLocalPreviewAllowsCustomization(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.back()
	wp.curConfig = &agentclient.Config{LocusMode: "open_only", ModelTiers: map[string]string{"everyday": "explicit-model"}}
	wp.state.TierPicks = map[string]string{"llama_server.everyday": "explicit-model"}
	wp.cursor = wp.defaultCursor()
	wp.selectRow()
	if wp.state.Step != wizard.StepDone {
		t.Fatalf("want preview, got %s: %s", wp.state.Step, wp.status)
	}
	if wp.state.TierPicks[wizardOpenTierKey(config.TierEveryday)] != "explicit-model" {
		t.Fatal("explicit choice replaced")
	}
	if wp.back() || wp.state.Step != wizard.StepOpen {
		t.Fatal("back should open optional model customization")
	}
	if wp.quickSetup {
		t.Fatal("back must disable quick path")
	}
}

func TestWizardRecommendedFreshUserConnectsBeforePreview(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.back()
	wp.selectRow()
	if wp.state.Step != wizard.StepCloud {
		t.Fatalf("want account connection, got %s", wp.state.Step)
	}
	wp.state.CloudProvider = "anthropic"
	wp.state.AuthMethod = "api_key"
	wp.advance()
	if wp.state.Step != wizard.StepDone {
		t.Fatalf("want preview after connection, got %s: %s", wp.state.Step, wp.status)
	}
	saved, ok := wizard.Load()
	if !ok || saved.Step != wizard.StepDone {
		t.Fatalf("preview not resumable: %+v", saved)
	}
}

func TestWizardBackReturnsToTemplateSelection(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.back()
	if rows := wp.rows(); len(rows) != 4 {
		t.Fatalf("missing templates: %+v", rows)
	}
	wp.cursor = wp.defaultCursor()
	wp.selectRow()
	if wp.back() || wp.state.Step != wizard.StepLocus {
		t.Fatal("back should return to templates")
	}
}
