package ui

import (
	"strings"
	"testing"

	"cercano/source/clients/cli/internal/wizard"
)

func TestWizardPreviewDisclosesBeforeApply(t *testing.T) {
	for _, mode := range []string{"cloud_only", "open_only", "open_primary", "cloud_primary"} {
		t.Run(mode, func(t *testing.T) {
			wp := &wizardPage{state: wizard.State{Step: wizard.StepDone, LocusMode: mode, CloudProvider: "anthropic", TierPicks: map[string]string{}}}
			text := wp.summary()
			if strings.Contains(text, "Setup complete") {
				t.Fatalf("preview claims application already happened: %s", text)
			}
			if !strings.Contains(text, "Review") {
				t.Fatalf("missing review heading: %s", text)
			}
			if mode == "open_only" && (!strings.Contains(text, "no cloud routes or backups") || strings.Contains(text, "anthropic")) {
				t.Fatalf("local-only disclosure: %s", text)
			}
			if mode == "cloud_only" && strings.Contains(text, "download in the background") {
				t.Fatalf("cloud-only implies download: %s", text)
			}
			if wp.rows()[0].Label != "Use recommended setup" {
				t.Fatal("missing explicit apply action")
			}
		})
	}
}

func TestWizardOpenDescriptionDoesNotClaimMachineValidation(t *testing.T) {
	wp := &wizardPage{state: wizard.State{Step: wizard.StepOpen}}
	if strings.Contains(wp.stepDesc(), "verified to run on this machine") {
		t.Fatal("catalog recommendations are not machine validation")
	}
}
