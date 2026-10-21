package ui

import (
	"strings"
	"testing"
)

func TestProviderSectionsStartCollapsed(t *testing.T) {
	sp := cloudSamplePage()
	sp.scope = scopeCloud
	sp.selectCloudRow("profile:work-openai")
	found := 0
	for _, s := range sp.buildCloudSections() {
		if s.Title == "Model tiers" || s.Title == "Advanced settings" {
			found++
			if s.Header == nil {
				t.Errorf("%s must toggle from its section header", s.Title)
			}
			if len(s.Fields) != 0 {
				t.Errorf("%s should collapse the whole section, got %d body fields", s.Title, len(s.Fields))
			}
		}
	}
	if found != 2 {
		t.Fatalf("missing collapsible sections: %d", found)
	}
}

func TestProviderEditorLabelsUseRendererIndentation(t *testing.T) {
	sp := cloudSamplePage()
	sp.selectCloudRow("profile:work-openai")
	sp.cloudAdvanced = true
	sp.cloudModelsExpanded = true
	for _, s := range sp.buildCloudSections() {
		for _, field := range s.Fields {
			if field.Label() != strings.TrimSpace(field.Label()) {
				t.Errorf("%s manually indents its label %q", field.Key(), field.Label())
			}
		}
	}
}

func TestProviderSectionTogglesPreserveDraftAndFieldFocus(t *testing.T) {
	p := layoutPage()
	p.selectCloudRow("profile:work-openai")
	p.cloudDraft.apiKey = "pending-secret"
	p.cloudDraft.apiKeyEdited = true
	p.cloudDraft.Choices.TierOverrides = map[string]string{"standard": "explicit-model"}
	p.cloudDirty = true
	p.form.Reload()
	layoutClick(t, p, "Model tiers")
	if !p.cloudModelsExpanded {
		t.Fatal("model section did not expand")
	}
	p.focusCloudField("cloud-pick-cloud-quality-standard")
	if key := cleanupFocusedKey(p); key != "cloud-pick-cloud-quality-standard" {
		t.Fatalf("header shifted model field focus: %s", key)
	}
	layoutClick(t, p, "Model tiers")
	if p.cloudModelsExpanded {
		t.Fatal("model section did not collapse")
	}
	if _, i := cleanupFieldAt(p, "cloud-pick-cloud-quality-standard"); i >= 0 {
		t.Fatal("collapsed model fields remain keyboard-focusable")
	}
	layoutClick(t, p, "Advanced settings")
	if !p.cloudAdvanced {
		t.Fatal("advanced section did not expand")
	}
	layoutClick(t, p, "Advanced settings")
	if p.cloudAdvanced {
		t.Fatal("advanced section did not collapse")
	}
	if p.cloudDraft.apiKey != "pending-secret" || !p.cloudDraft.apiKeyEdited || p.cloudDraft.Choices.TierOverrides["standard"] != "explicit-model" || !p.cloudDirty {
		t.Fatal("section toggles changed unsaved credentials or model choices")
	}
	layoutClick(t, p, "Model tiers")
	layoutClick(t, p, "Advanced settings")
	p.selectCloudRow("profile:work-openai")
	if p.cloudModelsExpanded || p.cloudAdvanced {
		t.Fatal("section expansion leaked into a newly opened editor")
	}
}
