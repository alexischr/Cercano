package ui

import (
	"reflect"
	"strings"
	"testing"

	"cercano/source/clients/cli/internal/wizard"
	"cercano/source/server/pkg/agentclient"
	"cercano/source/server/pkg/config"
)

// testRecs is the shipped-recommendation fixture: two cloud providers and
// one open set. Struct literals skip validate(); these tests exercise
// selection, not table validity.
func testRecs() config.TierRecommendations {
	c := func(most, every, fast string) config.TierCandidates {
		return config.TierCandidates{
			config.TierMostCapable:   {most},
			config.TierEveryday:      {every},
			config.TierFastLight:     {fast},
			config.TierFastLightText: {fast},
		}
	}
	return config.TierRecommendations{
		Version: 1,
		Cloud: map[string]config.TierCandidates{
			"anthropic": c("claude-opus-4", "claude-sonnet-4", "claude-haiku-4"),
		},
		Open: c("glm-4.5-air-q4km", "qwen3-8b-instruct", "qwen3-4b-instruct"),
	}
}

// testCatalog is the runtime-catalog fixture. rec maps tier → recommended
// model id (what the server computes from the machine's RAM band, including
// the embedding tier); downloaded marks ids already on disk (everything else
// is not downloaded).
func testCatalog(rec map[string]string, downloaded map[string]bool) agentclient.RuntimeModelCatalog {
	byID := map[string]agentclient.RuntimeModel{}
	for _, id := range rec {
		byID[id] = agentclient.RuntimeModel{
			ID:          id,
			DisplayName: "display:" + id,
			Runtime:     "llama_server",
		}
	}
	for id, dl := range downloaded {
		if dl {
			m := byID[id]
			m.DownloadState = "downloaded"
			byID[id] = m
		}
	}
	models := make([]agentclient.RuntimeModel, 0, len(byID))
	for _, m := range byID {
		models = append(models, m)
	}
	return agentclient.RuntimeModelCatalog{
		Models: models,
		// Default fixture: a fully-probed unified-memory machine (Apple
		// Silicon style), so tests asserting a clean, confident
		// recommendation keep their meaning. Topology-specific tests
		// override Hardware/HardwareOK below.
		SystemRAMBytes:        128 << 30,
		Hardware:              agentclient.HardwareTopology{Kind: agentclient.HardwareTopologyUnified, UnifiedMemoryBytes: 128 << 30},
		HardwareOK:            true,
		RecommendedOpenModels: rec,
	}
}

func TestRecommendCloudOnlyOneAccount(t *testing.T) {
	recs := testRecs()
	got := recommendSetup(wizardRecommendInputs{
		LocusMode:     "cloud_only",
		CloudProvider: "anthropic",
		Recs:          recs,
		RecsOK:        true,
	})
	if !got.CloudUsed || got.CloudBackup {
		t.Fatalf("cloud flags: used=%v backup=%v, want used=true backup=false", got.CloudUsed, got.CloudBackup)
	}
	want := map[config.Tier]string{
		config.TierMostCapable:   "claude-opus-4",
		config.TierEveryday:      "claude-sonnet-4",
		config.TierFastLight:     "claude-haiku-4",
		config.TierFastLightText: "claude-haiku-4",
	}
	if !reflect.DeepEqual(got.CloudPicks, want) {
		t.Errorf("cloud picks: want %v, got %v", want, got.CloudPicks)
	}
	if got.OpenPicks != nil {
		t.Errorf("cloud-only: want no open picks, got %v", got.OpenPicks)
	}
	if got.Downloads != nil || len(got.Notes) != 0 {
		t.Errorf("cloud-only: want no downloads/notes, got %v / %v", got.Downloads, got.Notes)
	}
}

func TestRecommendMixedDisclosesCloudUse(t *testing.T) {
	recs := testRecs()
	cat := testCatalog(map[string]string{
		"most_capable":    "llama_server:catalog:glm-4.5-air-q4km",
		"everyday":        "llama_server:catalog:glm-4.5-air-q4km",
		"fast_light":      "llama_server:catalog:qwen3-4b-instruct",
		"fast_light_text": "llama_server:catalog:qwen3-4b-instruct",
		"embedding":       "llama_server:catalog:embed-bge-m3",
	}, map[string]bool{
		"llama_server:catalog:embed-bge-m3": true, // on disk
	})
	got := recommendSetup(wizardRecommendInputs{
		LocusMode:     "cloud_primary",
		CloudProvider: "anthropic",
		Recs:          recs,
		RecsOK:        true,
		Catalog:       cat,
		CatalogOK:     true,
	})
	if !got.CloudUsed || !got.CloudBackup {
		t.Fatalf("mixed flags: used=%v backup=%v, want both true", got.CloudUsed, got.CloudBackup)
	}
	if len(got.CloudPicks) != len(wizardTierOrder) {
		t.Errorf("mixed: cloud picks not all filled: %v", got.CloudPicks)
	}
	if got.OpenPicks[config.TierMostCapable] != "llama_server:catalog:glm-4.5-air-q4km" {
		t.Errorf("mixed open most_capable: got %v", got.OpenPicks[config.TierMostCapable])
	}
	if got.OpenPicks[config.TierEmbedding] != "llama_server:catalog:embed-bge-m3" {
		t.Errorf("mixed open embedding: got %v", got.OpenPicks[config.TierEmbedding])
	}
	// Shared model downloads once; the already-downloaded embedding
	// artifact is reused, not re-downloaded.
	wantDownloads := []string{"llama_server:catalog:glm-4.5-air-q4km", "llama_server:catalog:qwen3-4b-instruct"}
	if !reflect.DeepEqual(got.Downloads, wantDownloads) {
		t.Errorf("mixed downloads: want %v, got %v", wantDownloads, got.Downloads)
	}
	// The probed unified machine carries the honest preliminary-screen
	// limitation note (context deferral); nothing else is unexpected.
	joined := strings.Join(got.Notes, "\n")
	if !strings.Contains(joined, "unified memory: preliminary weight-budget screen") {
		t.Errorf("mixed: want the unified preliminary-screen limitation note, got %v", got.Notes)
	}
	if strings.Contains(joined, "fit checked") {
		t.Errorf("mixed: must not claim a runtime fit check, got %v", got.Notes)
	}
}

func TestRecommendLocalOnlyHasNoCloudRoutes(t *testing.T) {
	recs := testRecs()
	cat := testCatalog(map[string]string{
		"most_capable":    "llama_server:catalog:glm-4.5-air-q4km",
		"everyday":        "llama_server:catalog:glm-4.5-air-q4km",
		"fast_light":      "llama_server:catalog:glm-4.5-air-q4km",
		"fast_light_text": "llama_server:catalog:glm-4.5-air-q4km",
		"embedding":       "llama_server:catalog:embed-bge-m3",
	}, nil)
	got := recommendSetup(wizardRecommendInputs{
		LocusMode:     "open_only",
		CloudProvider: "anthropic", // account exists; local preference must win
		Recs:          recs,
		RecsOK:        true,
		Catalog:       cat,
		CatalogOK:     true,
	})
	if got.CloudUsed || got.CloudBackup || len(got.CloudPicks) != 0 {
		t.Fatalf("local-only must not use cloud: used=%v backup=%v picks=%v", got.CloudUsed, got.CloudBackup, got.CloudPicks)
	}
	if got.CloudProvider != "" {
		t.Errorf("local-only provider should be empty, got %q", got.CloudProvider)
	}
	// One capable local model serves all capability tiers: one download.
	wantDownloads := []string{"llama_server:catalog:glm-4.5-air-q4km", "llama_server:catalog:embed-bge-m3"}
	if !reflect.DeepEqual(got.Downloads, wantDownloads) {
		t.Errorf("local-only downloads: want %v, got %v", wantDownloads, got.Downloads)
	}
	// The state-slot representation carries no cloud key at all.
	picks := got.tierPicks(cat, true)
	for k := range picks {
		if strings.HasSuffix(k, "."+wizard.SideCloud) {
			t.Errorf("local-only tierPicks has cloud key %q", k)
		}
	}
	for _, t2 := range openTierOrder() {
		key := wizardOpenTierKey(t2)
		if picks[key] == "" {
			t.Errorf("local-only tierPicks missing %q", key)
		}
	}
}

func TestRecommendLocalPreferenceHonored(t *testing.T) {
	recs := testRecs()
	for _, mode := range []string{"open_only", "open_primary"} {
		got := recommendSetup(wizardRecommendInputs{LocusMode: mode, Recs: recs, RecsOK: true})
		if got.LocusMode != mode {
			t.Fatalf("preference not preserved: %s", got.LocusMode)
		}
		if !wizard.ModeUsesOpen(got.LocusMode) || len(got.OpenPicks) == 0 {
			t.Errorf("%s: want open picks, got %v", mode, got.OpenPicks)
		}
	}
	// cloud_only is honored too: no open side.
	got := recommendSetup(wizardRecommendInputs{LocusMode: "cloud_only", CloudProvider: "anthropic", Recs: recs, RecsOK: true})
	if got.OpenPicks != nil {
		t.Errorf("cloud_only: want no open picks, got %v", got.OpenPicks)
	}
}

func TestRecommendNoUsableResources(t *testing.T) {
	// Cloud side: no account selected.
	got := recommendSetup(wizardRecommendInputs{LocusMode: "cloud_only", Recs: testRecs(), RecsOK: true})
	if len(got.CloudPicks) != 0 || len(got.Notes) == 0 {
		t.Errorf("no account: want empty picks + note, got %v / %v", got.CloudPicks, got.Notes)
	}
	// Open side: neither catalog nor shipped recs available.
	got = recommendSetup(wizardRecommendInputs{LocusMode: "open_only"})
	if len(got.OpenPicks) != 0 {
		t.Errorf("no resources: want empty open picks, got %v", got.OpenPicks)
	}
	if len(got.Notes) == 0 {
		t.Error("no resources: want a note explaining the empty recommendation")
	}
}

func TestRecommendUnknownProvider(t *testing.T) {
	got := recommendSetup(wizardRecommendInputs{
		LocusMode:     "cloud_only",
		CloudProvider: "bedrock", // no shipped recommendations
		Recs:          testRecs(),
		RecsOK:        true,
	})
	if len(got.CloudPicks) != 0 || len(got.Notes) == 0 {
		t.Errorf("unknown provider: want empty picks + note, got %v / %v", got.CloudPicks, got.Notes)
	}
}

func TestRecommendUnknownHardwareNotesHonestly(t *testing.T) {
	cat := testCatalog(map[string]string{"most_capable": "llama_server:catalog:qwen3-4b-instruct"}, nil)
	cat.SystemRAMBytes = 0 // probe failed server-side
	got := recommendSetup(wizardRecommendInputs{
		LocusMode: "open_only", Recs: testRecs(), RecsOK: true, Catalog: cat, CatalogOK: true,
	})
	found := false
	for _, n := range got.Notes {
		if len(n) > 0 && n == "machine memory unknown: the server recommended its smallest curated profile" {
			found = true
		}
	}
	if !found {
		t.Errorf("unknown hardware: want honest note, got %v", got.Notes)
	}
	// The recommendation still uses what the server computed — the client
	// never invents a fit verdict for hardware it cannot see.
	if got.OpenPicks[config.TierMostCapable] != "llama_server:catalog:qwen3-4b-instruct" {
		t.Errorf("unknown hardware: picks should defer to server, got %v", got.OpenPicks)
	}
}

func TestRecommendDefersToServerMemoryBand(t *testing.T) {
	recs := testRecs()
	// Two catalogs = two server-computed RAM bands (e.g. 48 GB vs 128 GB).
	// The client must take the server's band pick unchanged, not recompute.
	small := testCatalog(map[string]string{
		"most_capable":    "llama_server:catalog:qwen3-4b-instruct",
		"everyday":        "llama_server:catalog:qwen3-4b-instruct",
		"fast_light":      "llama_server:catalog:qwen3-4b-instruct",
		"fast_light_text": "llama_server:catalog:qwen3-4b-instruct",
		"embedding":       "llama_server:catalog:embed-bge-m3",
	}, nil)
	big := testCatalog(map[string]string{
		"most_capable":    "llama_server:catalog:glm-4.5-air-q4km",
		"everyday":        "llama_server:catalog:glm-4.5-air-q4km",
		"fast_light":      "llama_server:catalog:qwen3-4b-instruct",
		"fast_light_text": "llama_server:catalog:qwen3-4b-instruct",
		"embedding":       "llama_server:catalog:embed-bge-m3",
	}, nil)
	for _, c := range []struct {
		name string
		cat  agentclient.RuntimeModelCatalog
		most string
	}{{"small band", small, "llama_server:catalog:qwen3-4b-instruct"}, {"big band", big, "llama_server:catalog:glm-4.5-air-q4km"}} {
		got := recommendSetup(wizardRecommendInputs{
			LocusMode: "open_only", Recs: recs, RecsOK: true, Catalog: c.cat, CatalogOK: true,
		})
		if got.OpenPicks[config.TierMostCapable] != c.most {
			t.Errorf("%s: most_capable want %s, got %v", c.name, c.most, got.OpenPicks[config.TierMostCapable])
		}
	}
}

func TestRecommendFallsBackToShippedRecsWithNote(t *testing.T) {
	// Catalog unavailable (agent offline / old server): shipped recs fill
	// the open side, honestly labeled as not machine-fit-verified, and
	// nothing is scheduled for download (no artifacts to resolve).
	got := recommendSetup(wizardRecommendInputs{
		LocusMode: "open_only", Recs: testRecs(), RecsOK: true,
	})
	if got.OpenPicks[config.TierMostCapable] != "glm-4.5-air-q4km" {
		t.Errorf("fallback: most_capable want glm-4.5-air-q4km, got %v", got.OpenPicks[config.TierMostCapable])
	}
	if got.OpenPicks[config.TierEmbedding] != "" {
		t.Errorf("fallback: shipped recs carry no embedding candidate, want empty, got %v", got.OpenPicks[config.TierEmbedding])
	}
	if got.Downloads != nil {
		t.Errorf("fallback: want no downloads without a catalog, got %v", got.Downloads)
	}
	found := false
	for _, n := range got.Notes {
		if n == "runtime catalog unavailable: open picks use shipped recommendations, not machine-fit-verified models" {
			found = true
		}
	}
	if !found {
		t.Errorf("fallback: want unverified-picks note, got %v", got.Notes)
	}
}

func TestRecommendReusesInstalledArtifacts(t *testing.T) {
	cat := testCatalog(map[string]string{
		"most_capable":    "llama_server:catalog:glm-4.5-air-q4km",
		"everyday":        "llama_server:catalog:glm-4.5-air-q4km",
		"fast_light":      "llama_server:catalog:qwen3-4b-instruct",
		"fast_light_text": "llama_server:catalog:qwen3-4b-instruct",
		"embedding":       "llama_server:catalog:embed-bge-m3",
	}, map[string]bool{
		// Everything already on disk: reuse, no downloads.
		"llama_server:catalog:glm-4.5-air-q4km":  true,
		"llama_server:catalog:qwen3-4b-instruct": true,
		"llama_server:catalog:embed-bge-m3":      true,
	})
	got := recommendSetup(wizardRecommendInputs{
		LocusMode: "open_only", Recs: testRecs(), RecsOK: true, Catalog: cat, CatalogOK: true,
	})
	if got.Downloads != nil {
		t.Errorf("installed artifacts: want no downloads, got %v", got.Downloads)
	}
}

func TestRecommendTierPicksUsesSlotConventions(t *testing.T) {
	cat := testCatalog(map[string]string{
		"most_capable":    "llama_server:catalog:glm-4.5-air-q4km",
		"everyday":        "llama_server:catalog:glm-4.5-air-q4km",
		"fast_light":      "llama_server:catalog:qwen3-4b-instruct",
		"fast_light_text": "llama_server:catalog:qwen3-4b-instruct",
		"embedding":       "llama_server:catalog:embed-bge-m3",
	}, nil)
	got := recommendSetup(wizardRecommendInputs{
		LocusMode:     "cloud_primary",
		CloudProvider: "anthropic",
		Recs:          testRecs(),
		RecsOK:        true,
		Catalog:       cat,
		CatalogOK:     true,
	})
	picks := got.tierPicks(cat, true)
	want := map[string]string{
		"most_capable.cloud":           "claude-opus-4",
		"everyday.cloud":               "claude-sonnet-4",
		"fast_light.cloud":             "claude-haiku-4",
		"fast_light_text.cloud":        "claude-haiku-4",
		"llama_server.most_capable":    "display:llama_server:catalog:glm-4.5-air-q4km",
		"llama_server.everyday":        "display:llama_server:catalog:glm-4.5-air-q4km",
		"llama_server.fast_light":      "display:llama_server:catalog:qwen3-4b-instruct",
		"llama_server.fast_light_text": "display:llama_server:catalog:qwen3-4b-instruct",
		"llama_server.embedding":       "display:llama_server:catalog:embed-bge-m3",
	}
	if !reflect.DeepEqual(picks, want) {
		t.Errorf("tierPicks:\nwant %v\ngot  %v", want, picks)
	}
	// The keys are exactly the keys applyConfig's taxonomy patches accept,
	// so applying a recommendation is the existing wizard finish path.
	for _, t2 := range openTierOrder() {
		if _, ok := picks[wizardOpenTierKey(t2)]; !ok {
			t.Errorf("tierPicks missing open key %q", wizardOpenTierKey(t2))
		}
	}
}

func TestRecommendSetupPerformsNoWrites(t *testing.T) {
	// recommendSetup is a pure function over inputs; the test fixes that
	// contract by construction: the inputs are plain data and the result
	// carries no patch. Applying happens elsewhere (wizardApplyPatches →
	// UpdateConfig), only on the explicit finish action. This test pins
	// the type signature so a future writeable field can't slip in
	// unnoticed.
	got := recommendSetup(wizardRecommendInputs{LocusMode: "cloud_primary", CloudProvider: "anthropic", Recs: testRecs(), RecsOK: true})
	if len(got.CloudPicks) == 0 {
		t.Fatal("fixture regression: cloud picks empty")
	}
	var any interface{} = got
	if _, ok := any.(interface{ Write() error }); ok {
		t.Error("recommendation must not expose a Write method")
	}
}

// hardwareTestCatalog builds a catalog with explicit model sizes so fit
// checks have real numbers to reason about, and a caller-chosen topology
// over the same total system RAM — the equal-RAM/different-topology case.
func hardwareTestCatalog(topology agentclient.HardwareTopology, hardwareOK bool) agentclient.RuntimeModelCatalog {
	rec := map[string]string{
		"most_capable":    "llama_server:catalog:big-model",
		"everyday":        "llama_server:catalog:big-model",
		"fast_light":      "llama_server:catalog:small-model",
		"fast_light_text": "llama_server:catalog:small-model",
		"embedding":       "llama_server:catalog:embed-model",
	}
	sizes := map[string]int64{
		"llama_server:catalog:big-model":   32 << 30, // 32 GiB weights (fits 80% of a 48 GiB GPU, not 80% of 8 GiB)
		"llama_server:catalog:small-model": 4 << 30,
		"llama_server:catalog:embed-model": 1 << 30,
	}
	var models []agentclient.RuntimeModel
	for id, size := range sizes {
		models = append(models, agentclient.RuntimeModel{
			ID: id, DisplayName: "display:" + id, Runtime: "llama_server", SizeBytes: size,
		})
	}
	return agentclient.RuntimeModelCatalog{
		Models:                models,
		SystemRAMBytes:        64 << 30, // identical RAM in every scenario
		Hardware:              topology,
		HardwareOK:            hardwareOK,
		RecommendedOpenModels: rec,
	}
}

// hasNote reports whether the exact note text is present.
func hasNote(notes []string, want string) bool {
	for _, n := range notes {
		if n == want {
			return true
		}
	}
	return false
}

func TestRecommendEqualRAMDifferentTopology(t *testing.T) {
	recs := testRecs()
	scenarios := []struct {
		name       string
		topology   agentclient.HardwareTopology
		hardwareOK bool
		wantNote   string // must be present
		wantNoNote string // must be absent
		wantUnfit  bool   // expect the larger-than-any-single-GPU note
	}{
		{
			name:       "unified",
			topology:   agentclient.HardwareTopology{Kind: agentclient.HardwareTopologyUnified, UnifiedMemoryBytes: 64 << 30},
			hardwareOK: true,
			// Probed unified memory: no topology caveat — but the
			// preliminary-screen limitation note still applies (see
			// TestRecommendUnifiedDisclosesContextDeferral).
			wantNoNote: "memory topology unknown",
		},
		{
			name: "discrete-fits",
			topology: agentclient.HardwareTopology{Kind: agentclient.HardwareTopologyDiscrete, GPUs: []agentclient.HardwareGPU{
				{Name: "RTX A6000", MemoryBytes: 48 << 30},
			}},
			hardwareOK: true,
			wantNote:   "discrete GPUs: preliminary weight-budget screen against the largest GPU's",
			wantNoNote: "larger than any single GPU",
		},
		{
			name: "discrete-too-small",
			topology: agentclient.HardwareTopology{Kind: agentclient.HardwareTopologyDiscrete, GPUs: []agentclient.HardwareGPU{
				{Name: "GTX 1080", MemoryBytes: 8 << 30},
				{Name: "GTX 1080", MemoryBytes: 8 << 30},
			}},
			hardwareOK: true,
			wantNote:   "discrete GPUs: preliminary weight-budget screen against the largest GPU's",
			wantUnfit:  true,
		},
		{
			name: "discrete-unknown-pool",
			topology: agentclient.HardwareTopology{Kind: agentclient.HardwareTopologyDiscrete, GPUs: []agentclient.HardwareGPU{
				{Name: "Unknown GPU", MemoryBytes: 0}, // detected, size unknown
			}},
			hardwareOK: true,
			wantNote:   "discrete GPUs detected but per-GPU memory unknown: hardware fit not verified",
		},
		{
			name:       "topology-unknown",
			topology:   agentclient.HardwareTopology{Kind: agentclient.HardwareTopologyUnknown},
			hardwareOK: true,
			wantNote:   "memory topology unknown: hardware fit not verified for this machine",
			wantNoNote: "machine memory unknown",
		},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			cat := hardwareTestCatalog(sc.topology, sc.hardwareOK)
			got := recommendSetup(wizardRecommendInputs{
				LocusMode: "open_only", Recs: recs, RecsOK: true, Catalog: cat, CatalogOK: true,
			})
			// Every scenario has the same RAM and the same server band
			// picks; picks always defer to the server.
			if got.OpenPicks[config.TierMostCapable] != "llama_server:catalog:big-model" {
				t.Fatalf("%s: picks must defer to server, got %v", sc.name, got.OpenPicks)
			}
			if sc.wantNote != "" && !strings.Contains(strings.Join(got.Notes, "\n"), sc.wantNote) {
				t.Errorf("%s: want note %q, got %v", sc.name, sc.wantNote, got.Notes)
			}
			if sc.wantNoNote != "" && strings.Contains(strings.Join(got.Notes, "\n"), sc.wantNoNote) {
				t.Errorf("%s: want no note %q, got %v", sc.name, sc.wantNoNote, got.Notes)
			}
			if sc.wantUnfit {
				found := false
				for _, n := range got.Notes {
					if strings.HasPrefix(n, "larger than any single GPU:") && strings.Contains(n, "most_capable") {
						found = true
					}
				}
				if !found {
					t.Errorf("%s: want per-pick unfit disclosure naming the tier, got %v", sc.name, got.Notes)
				}
				// The two 8 GiB cards must never be summed into a 16 GiB
				// budget that would fake-fit the 32 GiB model.
				if !strings.Contains(strings.Join(got.Notes, "\n"), "largest GPU's 8.0 GB") {
					t.Errorf("%s: budget must be the single largest pool, got %v", sc.name, got.Notes)
				}
			}
		})
	}
}

// --- Reproduction tests for misleading hardware disclosures -----------------
//
// These tests pin the honest wording contract for the hardware screen: the
// client runs a *preliminary weight-budget screen* (weights + heuristic
// overhead against one memory pool), never a runtime fit verification.
// Context/KV-cache headroom, allocation, and serving capacity are validated
// at runtime, not here, and unknown sizes (model or device) must be
// disclosed, never silently passed.

func TestRecommendDiscreteScreenIsPreliminaryWeightBudget(t *testing.T) {
	// A discrete machine whose picks all fit: the screen still must not
	// claim "fit checked" as if runtime fit were verified, and must say
	// what the screen does and does not cover.
	cat := hardwareTestCatalog(agentclient.HardwareTopology{
		Kind: agentclient.HardwareTopologyDiscrete,
		GPUs: []agentclient.HardwareGPU{{Name: "RTX A6000", MemoryBytes: 48 << 30}},
	}, true)
	got := recommendSetup(wizardRecommendInputs{
		LocusMode: "open_only", Recs: testRecs(), RecsOK: true, Catalog: cat, CatalogOK: true,
	})
	joined := strings.Join(got.Notes, "\n")
	if strings.Contains(joined, "fit checked") {
		t.Errorf("must not claim a runtime fit check, got %q", joined)
	}
	if !strings.Contains(joined, "preliminary weight-budget screen") {
		t.Errorf("must name the screen a preliminary weight-budget screen, got %q", joined)
	}
	if !strings.Contains(joined, "context") {
		t.Errorf("must disclose the context/KV-cache limitation, got %q", joined)
	}
}

func TestRecommendDiscreteUnknownModelSizeDisclosed(t *testing.T) {
	// Picks whose catalog size is unknown (a size-0 record, or an id that
	// resolves to nothing) must be disclosed as unscreened — never skipped
	// silently, which would read as a pass.
	cat := hardwareTestCatalog(agentclient.HardwareTopology{
		Kind: agentclient.HardwareTopologyDiscrete,
		GPUs: []agentclient.HardwareGPU{{Name: "RTX A6000", MemoryBytes: 48 << 30}},
	}, true)
	cat.Models = append(cat.Models, agentclient.RuntimeModel{
		ID: "llama_server:catalog:unsized-model", DisplayName: "display:llama_server:catalog:unsized-model", Runtime: "llama_server", // SizeBytes 0: known model, unknown size
	})
	cat.RecommendedOpenModels = map[string]string{
		"most_capable": "llama_server:catalog:unsized-model",
		"embedding":    "llama_server:catalog:not-in-catalog", // resolves to no catalog entry
	}
	got := recommendSetup(wizardRecommendInputs{
		LocusMode: "open_only", Recs: testRecs(), RecsOK: true, Catalog: cat, CatalogOK: true,
	})
	joined := strings.Join(got.Notes, "\n")
	if !strings.Contains(joined, "model size unknown") {
		t.Errorf("unknown model sizes must be disclosed, got %q", joined)
	}
	if !strings.Contains(joined, "most_capable") || !strings.Contains(joined, "embedding") {
		t.Errorf("unknown-size disclosure must name the affected tiers, got %q", joined)
	}
	// And the disclosure must not fabricate a verdict either way.
	if strings.Contains(joined, "larger than any single GPU") {
		t.Errorf("unknown size must not produce an unfit verdict, got %q", joined)
	}
}

func TestRecommendUnifiedDisclosesContextDeferral(t *testing.T) {
	// A probed unified machine: the RAM-band picks are not a runtime fit
	// verdict there either — context/KV-cache headroom is not checked, so
	// a caveat must be present rather than silence.
	cat := hardwareTestCatalog(agentclient.HardwareTopology{
		Kind: agentclient.HardwareTopologyUnified, UnifiedMemoryBytes: 64 << 30,
	}, true)
	got := recommendSetup(wizardRecommendInputs{
		LocusMode: "open_only", Recs: testRecs(), RecsOK: true, Catalog: cat, CatalogOK: true,
	})
	joined := strings.Join(got.Notes, "\n")
	if joined == "" {
		t.Fatal("unified machines must carry the preliminary-screen limitation note, got none")
	}
	if !strings.Contains(joined, "unified memory: preliminary weight-budget screen") {
		t.Errorf("want unified preliminary weight-budget screen note, got %q", joined)
	}
	if !strings.Contains(joined, "context") {
		t.Errorf("want the context/KV-cache limitation disclosed, got %q", joined)
	}
	if strings.Contains(joined, "fit checked") {
		t.Errorf("must not claim a runtime fit check, got %q", joined)
	}
}

func TestRecommendOldServerDefaultsDisclosed(t *testing.T) {
	// An older server sends SystemRAMBytes but no topology fields:
	// HardwareOK arrives false and topology maps to unknown. The picks
	// still defer to the server's RAM band (it is all the server knows),
	// but the client must not present them as machine-fit-verified.
	cat := hardwareTestCatalog(agentclient.HardwareTopology{}, false)
	got := recommendSetup(wizardRecommendInputs{
		LocusMode: "open_only", Recs: testRecs(), RecsOK: true, Catalog: cat, CatalogOK: true,
	})
	if got.OpenPicks[config.TierMostCapable] != "llama_server:catalog:big-model" {
		t.Errorf("old server: picks should defer to server, got %v", got.OpenPicks)
	}
	if !hasNote(got.Notes, "memory topology unknown: hardware fit not verified for this machine") {
		t.Errorf("old server: want honest unknown-topology note, got %v", got.Notes)
	}
	// And it must not claim the confident no-caveat case.
	for _, n := range got.Notes {
		if strings.HasPrefix(n, "discrete GPUs") {
			t.Errorf("old server: must not claim a topology it did not probe: %v", got.Notes)
		}
	}
}
