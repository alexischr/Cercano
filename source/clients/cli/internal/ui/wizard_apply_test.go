package ui

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	"cercano/source/clients/cli/internal/wizard"
	"cercano/source/server/pkg/agentclient"
)

// allowedWizardPatchFields is the closed set of ConfigUpdate fields the
// wizard's apply path may touch. Everything else — user-set cloud defaults,
// context-size overrides, retention, watchdog, runtime switches — must ride
// through the wizard untouched. This test pins that: if a new ConfigUpdate
// field starts being set here, it fails until the set is consciously widened.
var allowedWizardPatchFields = map[string]bool{
	"LocusMode":      true,
	"OpenRuntime":    true,
	"ModelTierKey":   true,
	"ModelTierValue": true,
}

func assertPatchTouchesOnlyAllowedFields(t *testing.T, p agentclient.ConfigUpdate) {
	t.Helper()
	v := reflect.ValueOf(p)
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Name
		if !allowedWizardPatchFields[name] && !v.Field(i).IsZero() {
			t.Errorf("patch touches non-allowed field %s=%v", name, v.Field(i))
		}
	}
}

// TestWizardApplyPatchesPreservesExistingUserConfig is the Phase 1
// characterization: the wizard's apply path must carry existing users'
// configuration forward unchanged. Every sparse patch it emits sets only
// locus mode, the open runtime switch, and tier taxonomy slots — never
// user-chosen models, context sizes, retention, or watchdog settings.
func TestWizardApplyPatchesPreservesExistingUserConfig(t *testing.T) {
	cases := []struct {
		name string
		st   wizard.State
	}{
		{
			name: "cloud_only_anthropic",
			st:   wizard.State{LocusMode: "cloud_only", CloudProvider: "anthropic"},
		},
		{
			name: "mixed_cloud_primary_open_picks",
			st: wizard.State{
				LocusMode: "cloud_primary",
				TierPicks: map[string]string{
					"most_capable.cloud":           "claude-opus-4",
					"llama_server.fast_light_text": "Qwen3 4B Instruct Q4_K_M",
					"llama_server.embedding":       "Bge M3 Q8_0",
				},
			},
		},
		{
			name: "open_only",
			st: wizard.State{
				LocusMode: "open_only",
				TierPicks: map[string]string{
					"llama_server.most_capable": "GLM 4.5 Air Q4_K_M",
					"llama_server.embedding":    "Bge M3 Q8_0",
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, p := range wizardApplyPatches(tc.st) {
				assertPatchTouchesOnlyAllowedFields(t, p)
			}
		})
	}
}

// TestWizardApplyPatchesOrderAndShape pins the ordering the finish path
// relies on: the finish patch first (locus mode + open runtime), then one
// patch per non-cloud tier pick in sorted key order. Cloud keys are excluded
// here by design — they are committed through the profile mutation
// (applyCloudChoices), never through these taxonomy patches; the persisted
// order in applyConfig is finish patch → cloud choices → tier patches.
func TestWizardApplyPatchesOrderAndShape(t *testing.T) {
	st := wizard.State{
		LocusMode: "cloud_primary",
		TierPicks: map[string]string{
			"fast_light.cloud":             "claude-haiku-4",
			"most_capable.cloud":           "claude-opus-4",
			"llama_server.fast_light_text": "Qwen3 4B Instruct Q4_K_M",
			"llama_server.embedding":       "Bge M3 Q8_0",
			"llama_server.most_capable":    "GLM 4.5 Air Q4_K_M",
		},
	}
	patches := wizardApplyPatches(st)
	if len(patches) != 4 { // finish + 3 open picks; cloud keys excluded
		t.Fatalf("want 4 patches (finish + open picks), got %d", len(patches))
	}
	if got := patches[0]; got.LocusMode != "cloud_primary" || got.OpenRuntime != "llama_server" {
		t.Errorf("first patch: want locus finish (cloud_primary + llama_server), got %+v", got)
	}
	assertPatchTouchesOnlyAllowedFields(t, patches[0])
	// Remaining patches: sorted open-slot keys, taxonomy encoding, value
	// carried through verbatim (display-name labels stay display names).
	wantKeys := []string{
		"llama_server.embedding",
		"llama_server.fast_light_text",
		"llama_server.most_capable",
	}
	for i, want := range wantKeys {
		p := patches[i+1]
		if p.ModelTierKey != want {
			t.Errorf("patch %d: want key %s, got %q", i+1, want, p.ModelTierKey)
		}
		if p.ModelTierValue != st.TierPicks[want] {
			t.Errorf("patch %d (%s): value not carried verbatim: %q", i+1, want, p.ModelTierValue)
		}
		assertPatchTouchesOnlyAllowedFields(t, p)
	}
}

// TestWizardCloudOnlyNoOpenRuntime is a characterization of the existing
// behavior the new presets must preserve: a cloud-only finish must not flip
// the open runtime — a user running another runtime (or none) keeps it.
func TestWizardCloudOnlyNoOpenRuntime(t *testing.T) {
	patches := wizardApplyPatches(wizard.State{LocusMode: "cloud_only", CloudProvider: "anthropic"})
	if patches[0].OpenRuntime != "" {
		t.Errorf("cloud-only finish must not set OpenRuntime, got %q", patches[0].OpenRuntime)
	}
}

// TestWizardFinishLeavesResumeStateOnFail pins the reversibility seam: a
// failed apply keeps the persisted resume file so re-entry is guided from
// the failed step, not from scratch. (Abandon now only clears state; there
// is no cloud rollback.)
func TestWizardFinishLeavesResumeStateOnFail(t *testing.T) {
	wp := newTestWizardPage(t)
	// Walk to open_only via the UI so the persisted state matches a real
	// user flow.
	press(t, wp, tea.KeyDown) // cursor: cloud_primary → open_only
	press(t, wp, tea.KeyEnter)
	if wp.state.Step != wizard.StepOpen {
		t.Fatalf("locus open_only should land on the open step, got %s", wp.state.Step)
	}
	if wp.state.LocusMode != "open_only" {
		t.Fatalf("locus mode: want open_only, got %s", wp.state.LocusMode)
	}
	// Continue is the last row of the open step.
	for range wp.rows() {
		press(t, wp, tea.KeyDown)
	}
	press(t, wp, tea.KeyEnter)
	if wp.state.Step != wizard.StepDone {
		t.Fatalf("want %s, got %s", wizard.StepDone, wp.state.Step)
	}
	// advance() persisted the state; now fail the apply.
	wp.applyFn = func() error { return errTestFail }
	if _, closed := wp.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); closed {
		t.Fatal("finish with failing apply must not close the page")
	}
	if _, ok := wizard.Load(); !ok {
		t.Fatal("failed finish must keep the resume state for guided re-entry")
	}
	st, _ := wizard.Load()
	if st.LocusMode != "open_only" || st.Step != wizard.StepDone {
		t.Fatalf("resumed state: want open_only@done, got %s@%s", st.LocusMode, st.Step)
	}
}

var errTestFail = &testError{"apply failed"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

func TestWizardCloudOnlyIgnoresStaleOpenPicks(t *testing.T) {
	patches := wizardApplyPatches(wizard.State{LocusMode: "cloud_only", TierPicks: map[string]string{"llama_server.everyday": "hidden-old-pick"}})
	if len(patches) != 1 {
		t.Fatalf("cloud-only applies hidden local choices: %+v", patches)
	}
}

func TestWizardApplyIgnoresUnownedResumeKeys(t *testing.T) {
	patches := wizardApplyPatches(wizard.State{LocusMode: "open_only", TierPicks: map[string]string{
		"llama_server.everyday": "wanted", "ollama.everyday": "unrelated", "retired.local": "stale", "everyday.cloud": "cloud",
	}})
	if len(patches) != 2 || patches[1].ModelTierKey != "llama_server.everyday" {
		t.Fatalf("unowned keys applied: %+v", patches)
	}
}
