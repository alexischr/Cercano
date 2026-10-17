package ui

import (
	"cercano/source/clients/cli/internal/wizard"
	"strings"
	"testing"
)

func TestWizardAccountSelectionStaysPendingUntilConfirmation(t *testing.T) {
	stub, client := newScenario(t)
	wp := scenarioWizard(t, client)
	pressRows(t, wp, "recommended")
	pressRows(t, wp, "change_account")
	pressRows(t, wp, "account:team-external")
	if wp.state.Step != wizard.StepDone {
		t.Fatalf("expected preview, got %s", wp.state.Step)
	}
	if !strings.Contains(wp.summary(), "team-external") {
		t.Fatal("selected account missing from preview")
	}
	if !strings.Contains(wp.summary(), "was: personal") {
		t.Fatal("preview must disclose active-account change")
	}
	if stub.writeCount() != 0 {
		t.Fatal("selection wrote before confirmation")
	}
	saved, ok := wizard.Load()
	if !ok || saved.CloudProfile != "team-external" {
		t.Fatal("selection not saved")
	}
	wp = scenarioWizard(t, client)
	if wp.state.CloudProfile != "team-external" {
		t.Fatal("resume lost pending account")
	}
	if !pressRows(t, wp, "finish") {
		t.Fatalf("apply failed: %s", wp.status)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.active != "team-external" {
		t.Fatalf("account not activated: %s", stub.active)
	}
}

func TestWizardPendingAccountCancelDoesNotActivate(t *testing.T) {
	stub, client := newScenario(t)
	wp := scenarioWizard(t, client)
	for _, key := range []string{"recommended", "change_account", "account:team-external"} {
		pressRows(t, wp, key)
	}
	wp.abandon()
	if !wp.abandon() {
		t.Fatal("cancel did not close")
	}
	if stub.writeCount() != 0 {
		t.Fatal("cancel wrote configuration")
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.active != "personal" {
		t.Fatal("cancel switched account")
	}
}

func TestWizardRemovedPendingAccountFailsBeforeWrites(t *testing.T) {
	stub, client := newScenario(t)
	wp := scenarioWizard(t, client)
	for _, key := range []string{"recommended", "change_account", "account:team-external"} {
		pressRows(t, wp, key)
	}
	stub.mu.Lock()
	stub.profiles = stub.profiles[:1]
	stub.mu.Unlock()
	if pressRows(t, wp, "finish") {
		t.Fatal("missing account must not be accepted")
	}
	if !strings.Contains(wp.status, "no longer available") {
		t.Fatalf("wrong error: %s", wp.status)
	}
	if stub.writeCount() != 0 {
		t.Fatal("missing-account rejection wrote configuration")
	}
}

func TestWizardAccountSwitchDropsOnlyPreviousCloudPicks(t *testing.T) {
	_, client := newScenario(t)
	wp := scenarioWizard(t, client)
	pressRows(t, wp, "recommended")
	wp.state.TierPicks = map[string]string{"everyday.cloud": "old-account-model", "llama_server.everyday": "keep-local"}
	wp.state.CloudPicksEdited = map[string]bool{"everyday.cloud": true}
	pressRows(t, wp, "change_account")
	pressRows(t, wp, "account:team-external")
	if wp.state.TierPicks["everyday.cloud"] == "old-account-model" || wp.state.CloudPicksEdited["everyday.cloud"] {
		t.Fatal("old account model edits carried over")
	}
	if wp.state.TierPicks["llama_server.everyday"] != "keep-local" {
		t.Fatal("account change lost local pick")
	}
}
