package ui

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"

	"cercano/source/clients/cli/internal/form"
	tea "charm.land/bubbletea/v2"
)

// cleanupFieldAt finds a field by key and returns it with its flattened index
// (mirroring form.Form cursor indexing) or a nil field when absent.
func cleanupFieldAt(sp *settingsPage, key string) (form.Field, int) {
	idx := 0
	for _, sec := range sp.form.Sections {
		for _, f := range sec.Fields {
			if f.Key() == key {
				return f, idx
			}
			idx++
		}
	}
	return nil, -1
}

// cleanupFocusedKey reports the key of the field at the form cursor.
func cleanupFocusedKey(sp *settingsPage) string {
	want := sp.form.Cursor()
	idx := 0
	for _, sec := range sp.form.Sections {
		for _, f := range sec.Fields {
			if idx == want {
				return f.Key()
			}
			idx++
		}
	}
	return ""
}

// TestCloudCustomEntryHiddenUntilPickerChoosesCustom pins the picker-based
// custom model entry: the cloud-custom-* text inputs must stay hidden until the
// user picks "Enter custom model ID…" from the catalog picker, must then be
// created and focused without marking the draft dirty, and must disappear
// again after the draft is discarded.
func TestCloudCustomEntryHiddenUntilPickerChoosesCustom(t *testing.T) {
	sp := cloudSamplePage()
	sp.selectCloudRow("profile:work-openai")
	sp.form = form.New([]form.Section{sp.buildCloudSection()})
	sp.form.OnCommit = sp.onCommit
	sp.form.OnReload = func() []form.Section {
		return []form.Section{sp.buildCloudSection()}
	}

	if _, idx := cleanupFieldAt(sp, "cloud-custom-image"); idx >= 0 {
		t.Fatalf("cloud-custom-image should be hidden before the picker chooses custom")
	}
	if _, idx := cleanupFieldAt(sp, "cloud-custom-premium"); idx >= 0 {
		t.Fatalf("cloud-custom-premium should be hidden before the picker chooses custom")
	}

	// Activate the image-model picker button.
	if _, idx := cleanupFieldAt(sp, "cloud-pick-cloud-image"); idx < 0 {
		t.Fatalf("image picker button missing")
	} else {
		sp.form.SetCursor(idx)
	}
	sp.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if sp.catalogPicker == nil {
		t.Fatalf("catalog picker did not open")
	}
	// Tab to the custom-ID footer action and confirm it.
	for i := 0; i < 4; i++ {
		sp.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	}
	sp.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if sp.catalogPicker != nil {
		t.Fatalf("catalog picker did not close after choosing custom")
	}
	f, _ := cleanupFieldAt(sp, "cloud-custom-image")
	if f == nil {
		t.Fatalf("cloud-custom-image should appear after choosing the custom picker entry")
	}
	if got := cleanupFocusedKey(sp); got != "cloud-custom-image" {
		t.Fatalf("focus should move to cloud-custom-image, got %q", got)
	}
	if sp.cloudDirty {
		t.Fatalf("choosing the custom entry must not mark the draft dirty")
	}

	// Commit a custom ID through the new text field.
	if _, _, err := sp.onCommit("cloud-custom-image", "my-custom-image"); err == nil {
		t.Fatalf("commit custom image: %v", err)
	}
	if !sp.cloudDirty {
		t.Fatalf("editing a custom model ID should mark the draft dirty")
	}
	if got := sp.cloudDraft.Choices.ImageModel; got != "my-custom-image" {
		t.Fatalf("draft image model = %q, want %q", got, "my-custom-image")
	}
	if _, cmd, err := sp.commitCloud(classifyCloudCommit("cloud-discard", "")); err != nil {
		t.Fatalf("discard: %v", err)
	} else if cmd != nil {
		t.Fatalf("discard should not require an RPC")
	}
	sp.form.Reload()
	if _, idx := cleanupFieldAt(sp, "cloud-custom-image"); idx >= 0 {
		t.Fatalf("cloud-custom-image should be hidden again after discard")
	}

	// The quality picker follows the same path.
	if _, idx := cleanupFieldAt(sp, "cloud-pick-cloud-quality-premium"); idx < 0 {
		t.Fatalf("premium picker button missing")
	} else {
		sp.form.SetCursor(idx)
	}
	sp.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if sp.catalogPicker == nil {
		t.Fatalf("quality catalog picker did not open")
	}
	for i := 0; i < 4; i++ {
		sp.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	}
	sp.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if f, _ := cleanupFieldAt(sp, "cloud-custom-premium"); f == nil {
		t.Fatalf("cloud-custom-premium should appear after choosing the custom picker entry")
	}
}

// TestCloudChoiceLabelsCompactAndSingleMissingRecommendationWarning pins the
// compact choice labels and the consolidated missing-recommendation warning:
// each unavailable tier collapses its button label to "inherit" (never the
// repeated "recommendation unavailable" sentence), and exactly one read-only
// field lists every tier that lacks a provider recommendation.
func TestCloudChoiceLabelsCompactAndSingleMissingRecommendationWarning(t *testing.T) {
	sp := cloudSamplePage()
	sp.profiles[0].RecommendedQualityModels = map[string]string{
		"premium": "zai-org/GLM-5.3",
	}
	sp.selectCloudRow("profile:work-openai")
	sp.form = form.New([]form.Section{sp.buildCloudSection()})

	for quality, want := range map[string]string{
		"economy":  "Economy",
		"standard": "Standard",
	} {
		f, _ := cleanupFieldAt(sp, "cloud-pick-cloud-quality-"+quality)
		if f == nil {
			t.Fatalf("picker button for %s missing", quality)
		}
		if got := f.Label(); got != want {
			t.Fatalf("label for %s = %q, want %q", quality, got, want)
		}
	}
	pf, _ := cleanupFieldAt(sp, "cloud-pick-cloud-quality-premium")
	if pf == nil {
		t.Fatalf("picker button for premium missing")
	}
	if got := pf.Display(); !strings.Contains(got, "zai-org/GLM-5.3 (recommended)") {
		t.Fatalf("premium label should show the inherited model compactly, got %q", got)
	}

	warnings := 0
	for _, sec := range sp.form.Sections {
		for _, f := range sec.Fields {
			if f.Label() == "  Recommendations" {
				warnings++
				if got := f.Display(); got != "no provider recommendation for economy, standard" {
					t.Fatalf("consolidated warning display = %q", got)
				}
			}
		}
	}
	if warnings != 1 {
		t.Fatalf("want exactly one missing-recommendation warning field, got %d", warnings)
	}
	for _, sec := range sp.form.Sections {
		for _, f := range sec.Fields {
			if strings.Contains(f.Label(), "recommendation unavailable") {
				t.Fatalf("label %q still repeats the missing-recommendation sentence", f.Label())
			}
		}
	}
}

func TestCloudAccountActionsWithoutSaveDiscard(t *testing.T) {
	sp := cloudSamplePage()
	sp.selectCloudRow("profile:work-openai")
	sp.form = form.New([]form.Section{sp.buildCloudSection()})
	for _, key := range []string{"cloud-save", "cloud-discard"} {
		if _, i := cleanupFieldAt(sp, key); i >= 0 {
			t.Fatalf("obsolete action %s", key)
		}
	}
	_, adv := cleanupFieldAt(sp, "cloud-advanced")
	_, add := cleanupFieldAt(sp, "cloud-add-account")
	_, del := cleanupFieldAt(sp, "cloud-delete")
	if adv < 0 || add <= adv || del <= add {
		t.Fatal("account action ordering changed")
	}
}

func TestCloudTierModelColumnsAlign(t *testing.T) {
	p := cloudSamplePage()
	p.selectCloudRow("profile:work-openai")
	p.cloudDraft.Effective = map[string]string{"economy": "economy-model", "standard": "standard-model", "premium": "premium-model"}
	p.cloudDraft.Choices = p.cloudDraft.Choices.Clone()
	p.cloudDraft.Choices.ImageModel = "image-model"
	f := form.New([]form.Section{{Title: "Model tiers", Fields: p.cloudChoiceFields(p.catalogRow())}})
	rendered := ansi.Strip(f.View(100, p.palette, p.styles))
	column := -1
	for _, id := range []string{"economy-model", "standard-model", "premium-model", "image-model"} {
		found := false
		for _, line := range strings.Split(rendered, "\n") {
			if at := strings.Index(line, id); at >= 0 {
				at = ansi.StringWidth(line[:at])
				if column < 0 {
					column = at
				}
				if column != at {
					t.Fatalf("misaligned %s: %s", id, rendered)
				}
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %s: %s", id, rendered)
		}
	}
}
