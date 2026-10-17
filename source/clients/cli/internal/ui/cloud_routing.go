package ui

import (
	"cercano/source/clients/cli/internal/form"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

func (sp *settingsPage) cloudChoiceFields(row cloudRow) []form.Field {
	choices := sp.cloudDraft.Choices.Clone()
	var fields []form.Field
	for _, quality := range []string{"economy", "standard", "premium"} {
		current := choices.TierOverrides[quality]
		inherited := sp.cloudDraft.Effective[quality]
		if inherited == "" {
			inherited = "recommendation unavailable"
		}
		label := "inherit: " + inherited
		if current != "" {
			label = "reset to inherited recommendation"
		}
		if current != "" {
			label = current
		}
		fields = append(fields, form.NewButton("cloud-pick-cloud-quality-"+quality, "  "+quality+"  "+sp.catalogChoiceLabel(label)+"  ›", true), form.NewText("cloud-custom-"+quality, "  custom "+quality+" ID", current, "empty restores inheritance"))
	}
	image := choices.ImageModel
	if image == "" {
		image = "none"
	}
	fields = append(fields, form.NewButton("cloud-pick-cloud-image", "  image model  "+sp.catalogChoiceLabel(image)+"  ›", true), form.NewText("cloud-custom-image", "  custom image ID", choices.ImageModel, "requires confirmed image capability"))
	return fields
}
func (sp *settingsPage) applyCloudChoice(field, value string) bool {
	if field == "cloud-image" || field == "cloud-custom-image" {
		sp.cloudDraft.Choices = sp.cloudDraft.Choices.Clone()
		sp.cloudDraft.Choices.ImageModel = value
		return true
	}
	quality := strings.TrimPrefix(strings.TrimPrefix(field, "cloud-quality-"), "cloud-custom-")
	if quality != "economy" && quality != "standard" && quality != "premium" {
		return false
	}
	sp.cloudDraft.Choices = sp.cloudDraft.Choices.Clone()
	if value == "" {
		delete(sp.cloudDraft.Choices.TierOverrides, quality)
	} else {
		sp.cloudDraft.Choices.TierOverrides[quality] = value
	}
	return true
}

func (sp *settingsPage) catalogChoiceLabel(id string) string {
	label := id
	for _, m := range sp.cloudModels {
		if m.ID == id && m.DisplayName != "" {
			label = m.DisplayName
			break
		}
	}
	return ansi.Truncate(ansi.Strip(label), 32, "…")
}
