package ui

import (
	"cercano/source/clients/cli/internal/wizard"
	"cercano/source/server/pkg/agentclient"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestWizardEntryOffersRoutingTemplates(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.customizingLocus = false
	rows := wp.rows()
	for _, key := range []string{"cloud_only", "cloud_primary", "open_primary", "open_only"} {
		found := false
		for _, row := range rows {
			if row.Key == key {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing template %s: %+v", key, rows)
		}
	}
	if len(rows) != 4 {
		t.Fatalf("want four templates, got %+v", rows)
	}
}

func TestWizardCompactReviewKeepsDiagnosticsInDetails(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.state.Step = wizard.StepDone
	wp.state.LocusMode = "open_only"
	text := wp.stepDesc()
	if strings.Contains(text, "locus:") || strings.Contains(text, "Context:") || strings.Contains(text, "Assignments remain editable") {
		t.Fatalf("technical wall remains: %s", text)
	}
	if !strings.Contains(text, "No cloud") {
		t.Fatalf("missing privacy boundary: %s", text)
	}
	found := false
	for i, row := range wp.rows() {
		if row.Key == "details" {
			wp.cursor = i
			wp.selectRow()
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing details control")
	}
	if !strings.Contains(wp.stepDesc(), "Context:") {
		t.Fatal("details lost full disclosure")
	}
}

func TestWizardTierBulletsAreIndented(t *testing.T) {
	wp := newTestWizardPage(t)
	if !strings.Contains(ansi.Strip(wp.View()), "  * Deep reasoning") {
		t.Fatalf("tier bullet is not indented: %s", wp.View())
	}
}

func TestWizardCompactReviewDeduplicatesLocalArtifactAliases(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.state.Step = wizard.StepDone
	wp.state.LocusMode = "open_only"
	wp.catalogOK = true
	wp.catalog.Models = []agentclient.RuntimeModel{{ID: "one-model", DisplayName: "One Model", DownloadState: "missing"}}
	wp.state.TierPicks = map[string]string{"llama_server.most_capable": "one-model", "llama_server.everyday": "One Model"}
	text := wp.compactSummary()
	if strings.Count(text, "One Model") != 1 || !strings.Contains(text, "1 model download(s)") {
		t.Fatalf("artifact repeated: %s", text)
	}
	if !strings.Contains(text, "Deep reasoning, Everyday development") {
		t.Fatalf("assignments not grouped: %s", text)
	}
}

func TestWizardCompactReviewReflectsClearedCloudOverride(t *testing.T) {
	_, client := newScenario(t)
	wp := scenarioWizard(t, client)
	pressRows(t, wp, "cloud_only")
	wp.state.TierPicks = map[string]string{"everyday.cloud": ""}
	wp.state.CloudPicksEdited = map[string]bool{"everyday.cloud": true}
	text := wp.compactSummary()
	if !strings.Contains(text, "Everyday development: automatic account default") || strings.Contains(text, "claude-sonnet-4") {
		t.Fatalf("stale cloud model in confirmation: %s", text)
	}
}

func TestWizardDetailsAndModelCustomizationDoNotApply(t *testing.T) {
	stub, client := newScenario(t)
	wp := scenarioWizard(t, client)
	pressRows(t, wp, "cloud_primary")
	pressRows(t, wp, "details")
	if !wp.showDetails {
		t.Fatal("details not opened")
	}
	pressRows(t, wp, "details")
	pressRows(t, wp, "edit_models")
	if wp.state.Step != wizard.StepOpen || stub.writeCount() != 0 {
		t.Fatal("review navigation applied configuration")
	}
}

func TestWizardSetupHeadingIsBold(t *testing.T) {
	wp := newTestWizardPage(t)
	header := strings.Split(wp.View(), "\n")[0]
	if !strings.Contains(header, "\x1b[1;") && !strings.Contains(header, "\x1b[1m") {
		t.Fatalf("heading lacks bold style: %q", header)
	}
}
