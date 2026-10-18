package ui

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"cercano/source/clients/cli/internal/wizard"
	"cercano/source/server/pkg/agentclient"
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

func TestWizardOpenAutofillPreservesCustomLocalPick(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.state.LocusMode = "open_only"
	wp.state.TierPicks = map[string]string{
		"llama_server.most_capable": "custom-model-123",
	}
	wp.state.Step = wizard.StepOpen

	// Set up catalog with recommendations
	wp.catalog = agentclient.RuntimeModelCatalog{
		Models: []agentclient.RuntimeModel{
			{ID: "llama_server:catalog:recommended-model", DisplayName: "Recommended Model", Runtime: "llama_server", Source: "catalog"},
		},
		RecommendedOpenModels: map[string]string{
			"most_capable": "llama_server:catalog:recommended-model",
		},
	}
	wp.catalogOK = true
	wp.recsOK = false // Disable shipped recs to force catalog usage

	wp.autofillTiers()

	// Custom pick should survive - only EMPTY slots should be filled
	if got := wp.state.TierPicks["llama_server.most_capable"]; got != "custom-model-123" {
		t.Fatalf("most_capable: want custom pick preserved, got %q", got)
	}
}

func TestWizardCatalogRecommendationsWorkWhenRecsOKFalse(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.state.LocusMode = "open_only"
	wp.state.TierPicks = nil // Start empty
	wp.state.Step = wizard.StepOpen

	// Set up catalog with recommendations but disable shipped recs
	wp.catalog = agentclient.RuntimeModelCatalog{
		Models: []agentclient.RuntimeModel{
			{ID: "llama_server:catalog:recommended-model", DisplayName: "Recommended Model", Runtime: "llama_server", Source: "catalog"},
		},
		RecommendedOpenModels: map[string]string{
			"most_capable": "llama_server:catalog:recommended-model",
		},
	}
	wp.catalogOK = true
	wp.recsOK = false // This should still work

	wp.autofillTiers()

	// Catalog recommendations should still be applied even when recsOK is false
	if got := wp.state.TierPicks["llama_server.most_capable"]; got != "Recommended Model" {
		t.Fatalf("most_capable: want catalog recommendation, got %q", got)
	}
}

func TestWizardPreviewDisclosesDistinctRequiredDownloads(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.state.LocusMode = "open_only"
	wp.state.Step = wizard.StepDone
	wp.state.TierPicks = map[string]string{
		"llama_server.most_capable": "Recommended Model",
		"llama_server.everyday":     "Recommended Model", // Same model, different tiers
		"llama_server.fast_light":   "Different Model",
	}

	// Set up catalog with models and download states
	wp.catalog = agentclient.RuntimeModelCatalog{
		Models: []agentclient.RuntimeModel{
			{
				ID:            "llama_server:catalog:recommended-model",
				DisplayName:   "Recommended Model",
				Runtime:       "llama_server",
				Source:        "catalog",
				DownloadState: "not_downloaded",
			},
			{
				ID:            "llama_server:catalog:different-model",
				DisplayName:   "Different Model",
				Runtime:       "llama_server",
				Source:        "catalog",
				DownloadState: "not_downloaded",
			},
		},
		RecommendedOpenModels: map[string]string{
			"most_capable": "llama_server:catalog:recommended-model",
			"everyday":     "llama_server:catalog:recommended-model",
			"fast_light":   "llama_server:catalog:different-model",
		},
	}
	wp.catalogOK = true

	summary := wp.summary()

	// Should list distinct models only once
	if strings.Count(summary, "Download after confirmation: llama_server:catalog:recommended-model") != 1 {
		t.Fatalf("duplicate model listing: %s", summary)
	}
	if !strings.Contains(summary, "Different Model") {
		t.Fatalf("missing different model: %s", summary)
	}
	// Should indicate downloads needed
	if !strings.Contains(summary, "download in the background") {
		t.Fatalf("missing download indication: %s", summary)
	}
}

func TestWizardPreviewDisclosesDownloadedModelsReuse(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.state.LocusMode = "open_only"
	wp.state.Step = wizard.StepDone
	wp.state.TierPicks = map[string]string{
		"llama_server.most_capable": "Recommended Model",
		"llama_server.everyday":     "Recommended Model",
	}

	// Set up catalog with model already downloaded
	wp.catalog = agentclient.RuntimeModelCatalog{
		Models: []agentclient.RuntimeModel{
			{
				ID:            "llama_server:catalog:recommended-model",
				DisplayName:   "Recommended Model",
				Runtime:       "llama_server",
				Source:        "catalog",
				DownloadState: "downloaded",
			},
		},
		RecommendedOpenModels: map[string]string{
			"most_capable": "llama_server:catalog:recommended-model",
			"everyday":     "llama_server:catalog:recommended-model",
		},
	}
	wp.catalogOK = true

	summary := wp.summary()

	// Should not indicate downloads needed for already downloaded models
	if strings.Contains(summary, "download in the background") {
		t.Fatalf("should not indicate downloads for already downloaded models: %s", summary)
	}
}

func TestWizardPreviewDisclosesCustomSelectedInsteadOfDefault(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.state.LocusMode = "open_only"
	wp.state.Step = wizard.StepDone
	wp.state.TierPicks = map[string]string{
		"llama_server.most_capable": "Custom Model", // Custom selection
	}

	// Set up catalog with different recommendation
	wp.catalog = agentclient.RuntimeModelCatalog{
		Models: []agentclient.RuntimeModel{
			{
				ID:            "llama_server:catalog:recommended-model",
				DisplayName:   "Recommended Model",
				Runtime:       "llama_server",
				Source:        "catalog",
				DownloadState: "not_downloaded",
			},
			{
				ID:            "llama_server:catalog:custom-model",
				DisplayName:   "Custom Model",
				Runtime:       "llama_server",
				Source:        "catalog",
				DownloadState: "not_downloaded",
			},
		},
		RecommendedOpenModels: map[string]string{
			"most_capable": "llama_server:catalog:recommended-model",
		},
	}
	wp.catalogOK = true

	summary := wp.summary()

	// Should show custom selection, not default
	if !strings.Contains(summary, "Custom Model") {
		t.Fatalf("missing custom model: %s", summary)
	}
	if strings.Contains(summary, "Recommended Model") {
		t.Fatalf("should not show default when custom selected: %s", summary)
	}
}

func TestWizardPreviewDisclosesUnknownCatalog(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.state.LocusMode = "open_only"
	wp.state.Step = wizard.StepDone
	wp.state.TierPicks = map[string]string{
		"llama_server.most_capable": "Some Model",
	}
	wp.catalogOK = false // Catalog unavailable

	summary := wp.summary()

	// Should say cannot determine requirements
	if !strings.Contains(summary, "cannot determine requirements") {
		t.Fatalf("missing catalog unavailable message: %s", summary)
	}
	// Should not claim zero downloads
	if strings.Contains(summary, "no downloads") {
		t.Fatalf("should not claim zero downloads when catalog unavailable: %s", summary)
	}
}

func TestWizardPreviewDisclosesUnknownSystemRAM(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.state.LocusMode = "open_only"
	wp.state.Step = wizard.StepDone
	wp.state.TierPicks = map[string]string{
		"llama_server.most_capable": "Recommended Model",
	}

	// Set up catalog with unknown RAM (0)
	wp.catalog = agentclient.RuntimeModelCatalog{
		Models: []agentclient.RuntimeModel{
			{
				ID:            "llama_server:catalog:recommended-model",
				DisplayName:   "Recommended Model",
				Runtime:       "llama_server",
				Source:        "catalog",
				DownloadState: "not_downloaded",
			},
		},
		RecommendedOpenModels: map[string]string{
			"most_capable": "llama_server:catalog:recommended-model",
		},
		SystemRAMBytes: 0, // Unknown RAM
	}
	wp.catalogOK = true

	summary := wp.summary()

	// Should warn about hardware fit unknown
	if !strings.Contains(summary, "hardware fit unknown") {
		t.Fatalf("missing hardware fit warning: %s", summary)
	}
}

func TestWizardPreviewDisclosesUnresolvedCustomModel(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.state.LocusMode = "open_only"
	wp.state.Step = wizard.StepDone
	wp.state.TierPicks = map[string]string{
		"llama_server.most_capable": "Unknown Custom Model", // Not in catalog
	}

	// Set up catalog without this model
	wp.catalog = agentclient.RuntimeModelCatalog{
		Models: []agentclient.RuntimeModel{
			{
				ID:            "llama_server:catalog:known-model",
				DisplayName:   "Known Model",
				Runtime:       "llama_server",
				Source:        "catalog",
				DownloadState: "not_downloaded",
			},
		},
		RecommendedOpenModels: map[string]string{
			"most_capable": "llama_server:catalog:known-model",
		},
	}
	wp.catalogOK = true

	summary := wp.summary()

	// Should mark unknown model status
	if !strings.Contains(summary, "unknown") {
		t.Fatalf("missing unknown model status: %s", summary)
	}
	// Should not claim zero downloads
	if strings.Contains(summary, "no downloads") {
		t.Fatalf("should not claim zero downloads when unknown models present: %s", summary)
	}
}

func TestWizardPreviewIsSideEffectFree(t *testing.T) {
	// Verify that summary() doesn't modify state or make RPC calls
	wp := newTestWizardPage(t)
	wp.state.LocusMode = "open_only"
	wp.state.Step = wizard.StepDone
	wp.state.TierPicks = map[string]string{
		"llama_server.most_capable": "Test Model",
	}

	// Set up catalog
	wp.catalog = agentclient.RuntimeModelCatalog{
		Models: []agentclient.RuntimeModel{
			{
				ID:            "llama_server:catalog:test-model",
				DisplayName:   "Test Model",
				Runtime:       "llama_server",
				Source:        "catalog",
				DownloadState: "not_downloaded",
			},
		},
		RecommendedOpenModels: map[string]string{
			"most_capable": "llama_server:catalog:test-model",
		},
	}
	wp.catalogOK = true

	// Capture initial state
	initialTierPicks, _ := json.Marshal(wp.state.TierPicks)
	initialCatalog, _ := json.Marshal(wp.catalog)
	initialCatalogOK := wp.catalogOK

	// Call summary multiple times
	summary1 := wp.summary()
	summary2 := wp.summary()
	summary3 := wp.summary()

	// Verify state unchanged
	currentTierPicks, _ := json.Marshal(wp.state.TierPicks)
	if string(currentTierPicks) != string(initialTierPicks) {
		t.Fatalf("summary() modified TierPicks")
	}
	currentCatalog, _ := json.Marshal(wp.catalog)
	if string(currentCatalog) != string(initialCatalog) {
		t.Fatalf("summary() modified catalog")
	}
	if wp.catalogOK != initialCatalogOK {
		t.Fatalf("summary() modified catalogOK")
	}

	// Verify consistent output
	if summary1 != summary2 || summary2 != summary3 {
		t.Fatalf("summary() returned inconsistent results")
	}
}

func TestWizardPreviewListsSelectedDownloadsOnce(t *testing.T) {
	wp := &wizardPage{state: wizard.State{LocusMode: "open_only", TierPicks: map[string]string{"llama_server.most_capable": "Selected", "llama_server.everyday": "Selected", "llama_server.lightweight": "Installed"}}, catalogOK: true}
	wp.catalog.Models = []agentclient.RuntimeModel{
		{ID: "selected-id", DisplayName: "Selected", Runtime: "llama_server", Source: "catalog"},
		{ID: "installed-id", DisplayName: "Installed", Runtime: "llama_server", Source: "catalog", DownloadState: "downloaded"},
	}
	text := wp.summary()
	if strings.Count(text, "Download after confirmation: selected-id") != 1 {
		t.Fatalf("missing or duplicate selected download: %s", text)
	}
	if strings.Contains(text, "Download after confirmation: installed-id") {
		t.Fatalf("installed model marked for download: %s", text)
	}
	if !strings.Contains(text, "Machine memory is unknown") {
		t.Fatalf("missing unknown hardware warning: %s", text)
	}
}
func TestWizardPreviewUnknownSelectionDoesNotPromiseDownload(t *testing.T) {
	wp := &wizardPage{state: wizard.State{LocusMode: "open_only", TierPicks: map[string]string{"llama_server.everyday": "custom-model"}}, catalogOK: true}
	text := wp.summary()
	if !strings.Contains(text, "Readiness not verified: custom-model") {
		t.Fatalf("unknown model not disclosed: %s", text)
	}
}

func TestWizardPreviewDisclosesUnknownTopology(t *testing.T) {
	// RAM known but topology not probed (or an older server without the
	// fields): the preview must still refuse a fit claim.
	wp := &wizardPage{state: wizard.State{Step: wizard.StepDone, LocusMode: "open_only", TierPicks: map[string]string{"llama_server.most_capable": "Recommended Model"}}}
	wp.catalog = agentclient.RuntimeModelCatalog{
		Models: []agentclient.RuntimeModel{{
			ID: "llama_server:catalog:recommended-model", DisplayName: "Recommended Model",
			Runtime: "llama_server", Source: "catalog", DownloadState: "downloaded",
		}},
		RecommendedOpenModels: map[string]string{"most_capable": "llama_server:catalog:recommended-model"},
		SystemRAMBytes:        64 << 30,
		Hardware:              agentclient.HardwareTopology{Kind: agentclient.HardwareTopologyUnknown},
		HardwareOK:            false, // old-server default
	}
	wp.catalogOK = true
	summary := wp.summary()
	if !strings.Contains(summary, "Memory topology is unknown; hardware fit unknown") {
		t.Fatalf("missing unknown-topology disclosure: %s", summary)
	}
}

func TestWizardPreviewDiscreteScreensSelectedPicks(t *testing.T) {
	// The preview must screen the user's actual selected local picks — not
	// the server's RecommendedOpenModels, which may differ from the
	// selections being applied — and must not mutate the picks while
	// doing so.
	wp := &wizardPage{state: wizard.State{
		Step:      wizard.StepDone,
		LocusMode: "open_only",
		TierPicks: map[string]string{"llama_server.most_capable": "Custom Big Model"},
	}}
	wp.catalog = agentclient.RuntimeModelCatalog{
		Models: []agentclient.RuntimeModel{
			{
				ID: "llama_server:catalog:custom-big-model", DisplayName: "Custom Big Model",
				Runtime: "llama_server", Source: "catalog", DownloadState: "downloaded", SizeBytes: 40 << 30,
			},
			{
				ID: "llama_server:catalog:recommended-model", DisplayName: "Recommended Model",
				Runtime: "llama_server", Source: "catalog", DownloadState: "downloaded", SizeBytes: 4 << 30,
			},
		},
		// The server's band pick fits the GPUs; the user's explicit
		// selection does not. Screening the band instead of the
		// selection would silently pass an unfit pick.
		RecommendedOpenModels: map[string]string{"most_capable": "llama_server:catalog:recommended-model"},
		SystemRAMBytes:        64 << 30,
		Hardware: agentclient.HardwareTopology{Kind: agentclient.HardwareTopologyDiscrete, GPUs: []agentclient.HardwareGPU{
			{Name: "GTX 1080", MemoryBytes: 8 << 30},
			{Name: "GTX 1080", MemoryBytes: 8 << 30},
		}},
		HardwareOK: true,
	}
	wp.catalogOK = true
	before := make(map[string]string, len(wp.state.TierPicks))
	for k, v := range wp.state.TierPicks {
		before[k] = v
	}
	summary := wp.summary()
	if !strings.Contains(summary, "larger than any single GPU") || !strings.Contains(summary, "Custom Big Model") {
		t.Fatalf("the selected (unfit) pick must be disclosed, got:\n%s", summary)
	}
	if !strings.Contains(summary, "preliminary weight-budget screen") || strings.Contains(summary, "fit checked") {
		t.Fatalf("the screen must be named a preliminary weight-budget screen, never a fit check, got:\n%s", summary)
	}
	if !reflect.DeepEqual(wp.state.TierPicks, before) {
		t.Fatalf("screening must not mutate picks: before %v, after %v", before, wp.state.TierPicks)
	}
}

func TestWizardPreviewDisclosesDiscreteFitPerGPU(t *testing.T) {
	// Same RAM as a unified machine, but discrete GPUs: the preview must
	// check fit against the largest GPU pool — never system RAM, never a
	// sum across devices — and name picks that don't fit any single GPU.
	wp := &wizardPage{state: wizard.State{Step: wizard.StepDone, LocusMode: "open_only", TierPicks: map[string]string{"llama_server.most_capable": "Big Model"}}}
	wp.catalog = agentclient.RuntimeModelCatalog{
		Models: []agentclient.RuntimeModel{{
			ID: "llama_server:catalog:big-model", DisplayName: "Big Model",
			Runtime: "llama_server", Source: "catalog", DownloadState: "downloaded", SizeBytes: 40 << 30,
		}},
		RecommendedOpenModels: map[string]string{"most_capable": "llama_server:catalog:big-model"},
		SystemRAMBytes:        64 << 30, // plenty — but not GPU memory here
		Hardware: agentclient.HardwareTopology{Kind: agentclient.HardwareTopologyDiscrete, GPUs: []agentclient.HardwareGPU{
			{Name: "GTX 1080", MemoryBytes: 8 << 30},
			{Name: "GTX 1080", MemoryBytes: 8 << 30},
		}},
		HardwareOK: true,
	}
	wp.catalogOK = true
	summary := wp.summary()
	if !strings.Contains(summary, "preliminary weight-budget screen against the largest GPU's") {
		t.Fatalf("missing per-GPU budget disclosure: %s", summary)
	}
	if !strings.Contains(summary, "larger than any single GPU") || !strings.Contains(summary, "most_capable") {
		t.Fatalf("missing unfit pick disclosure: %s", summary)
	}
}
