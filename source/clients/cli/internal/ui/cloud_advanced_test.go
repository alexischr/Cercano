package ui

import (
	"cercano/source/clients/cli/internal/form"
	"testing"
)

func TestCloudAdvancedVisibilityAndDraftPreservation(t *testing.T) {
	sp := cloudSamplePage()
	sp.selectCloudRow("profile:work-openai")
	positions := func() map[string]int {
		result := map[string]int{}
		for i, f := range sp.buildCloudSection().Fields {
			result[f.Key()] = i
		}
		return result
	}
	keys := positions()
	for _, key := range []string{"cloud-name", "cloud-flavor", "cloud-backend", "cloud-base-url"} {
		if _, ok := keys[key]; ok {
			t.Fatalf("advanced field %s visible by default", key)
		}
	}
	for _, key := range []string{"cloud-account", "cloud-key", "cloud-pick-cloud-quality-standard", "cloud-advanced"} {
		if _, ok := keys[key]; !ok {
			t.Fatalf("missing routine field %s", key)
		}
	}
	if keys["cloud-account"] >= keys["cloud-key"] || keys["cloud-key"] >= keys["cloud-pick-cloud-quality-standard"] || keys["cloud-pick-cloud-quality-standard"] >= keys["cloud-advanced"] {
		t.Fatal("expected Account -> credentials -> Models -> Advanced")
	}
	sp.cloudDraft.apiKey = "pending-secret"
	sp.cloudDraft.apiKeyEdited = true
	sp.cloudDirty = true
	if _, _, err := sp.onCommit("cloud-advanced", ""); err != nil {
		t.Fatal(err)
	}
	keys = positions()
	for _, key := range []string{"cloud-name", "cloud-flavor", "cloud-backend", "cloud-base-url"} {
		if _, ok := keys[key]; !ok {
			t.Fatalf("expanded field %s missing", key)
		}
	}
	if _, _, err := sp.onCommit("cloud-advanced", ""); err != nil {
		t.Fatal(err)
	}
	if sp.cloudAdvanced || !sp.cloudDirty || sp.cloudDraft.apiKey != "pending-secret" || !sp.cloudDraft.apiKeyEdited {
		t.Fatal("toggle changed draft or failed to collapse")
	}
	sp.cloudAdvanced = true
	sp.selectCloudRow("template:gemini")
	if sp.cloudAdvanced {
		t.Fatal("advanced expansion leaked across accounts")
	}
}

func TestCustomProviderAdvancedFieldsRemainEditableAfterAutosave(t *testing.T) {
	sp := cloudSamplePage()
	sp.selectCloudRow("other")
	sp.cloudDraftNew = false
	sp.cloudSelected = "profile:custom"
	sp.cloudAdvanced = true
	fields := sp.cloudDetailFields(cloudRow{ID: "profile:custom", IsProfile: true})
	found := 0
	for _, f := range fields {
		if f.Key() == "cloud-flavor" || f.Key() == "cloud-backend" {
			found++
			if _, ok := f.(*form.SelectField); !ok {
				t.Fatalf("%s became read-only after first autosave", f.Key())
			}
		}
	}
	if found != 2 {
		t.Fatal("custom connection controls missing")
	}
}
