package ui

import (
	"cercano/source/clients/cli/internal/form"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

func (sp *settingsPage) cloudChoiceFields(row cloudRow) []form.Field {
	choices := sp.cloudDraft.Choices.Clone()
	var fields []form.Field
	var missing []string
	for _, quality := range []string{"economy", "standard", "premium"} {
		current := choices.TierOverrides[quality]
		inherited := sp.cloudDraft.Effective[quality]
		var label string
		switch {
		case current != "":
			label = sp.catalogChoiceLabel(current)
		case inherited != "":
			label = sp.catalogChoiceLabel(inherited) + " (recommended)"
		default:
			label = "No recommendation"
			missing = append(missing, quality)
		}
		fields = append(fields, form.NewRow("cloud-pick-cloud-quality-"+quality, strings.ToUpper(quality[:1])+quality[1:], label+"  ›", true))
		if shouldShowCloudCustomEntry("cloud-quality-" + quality) {
			fields = append(fields, form.NewText("cloud-custom-"+quality, "Custom "+quality+" ID", current, "empty restores inheritance"))
		}
	}
	image := choices.ImageModel
	if image == "" {
		image = "Automatic (chat / provider recommendation)"
	}
	fields = append(fields, form.NewRow("cloud-pick-cloud-image", "Image model", sp.catalogChoiceLabel(image)+"  ›", true))
	if shouldShowCloudCustomEntry("cloud-image") {
		fields = append(fields, form.NewText("cloud-custom-image", "Custom image ID", choices.ImageModel, "requires confirmed image capability"))
	}
	// One consolidated warning instead of repeating the sentence per tier.
	if len(missing) > 0 {
		fields = append(fields, form.NewReadOnly("cloud-recommendation-missing", "Recommendations", "no provider recommendation for "+strings.Join(missing, ", "), "choose from the picker or enter a custom ID"))
	}
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
