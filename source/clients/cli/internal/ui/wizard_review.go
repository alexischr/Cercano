package ui

import (
	"fmt"
	"strings"

	"cercano/source/clients/cli/internal/wizard"
)

func wizardTemplateName(mode string) string {
	switch mode {
	case "cloud_only":
		return "Cloud"
	case "cloud_primary":
		return "Cloud + local helpers"
	case "open_primary":
		return "Local + cloud fallback"
	case "open_only":
		return "Local only"
	}
	return mode
}

// compactSummary is the confirmation overview. Full assignment changes and
// diagnostics remain available through View details, without additional writes.
func (wp *wizardPage) compactSummary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Template: %s\n", wizardTemplateName(wp.state.LocusMode))
	if wp.curConfig != nil && wp.curConfig.LocusMode != "" && wp.curConfig.LocusMode != wp.state.LocusMode {
		fmt.Fprintf(&b, "  Replaces: %s\n", wizardTemplateName(wp.curConfig.LocusMode))
	}
	switch wp.state.LocusMode {
	case "cloud_only":
		b.WriteString("Work runs in the cloud. No local downloads.\n")
	case "cloud_primary":
		b.WriteString("Main work goes to the cloud; helpers run locally.\n")
	case "open_primary":
		b.WriteString("Main work runs locally. Cloud fallback may send work off this machine.\n")
	case "open_only":
		b.WriteString("No cloud use or fallback. Local models must be ready.\n")
	}
	if wizard.ModeUsesCloud(wp.state.LocusMode) {
		b.WriteString("\nCloud account\n")
		if p := wp.selectedCloudProfile(); p != nil {
			fmt.Fprintf(&b, "  %s", p.Name)
			if p.Name != wp.curActiveProfile {
				fmt.Fprintf(&b, " (was: %s)", wp.curActiveProfile)
			}
			b.WriteByte('\n')
			choices, err := wizardCloudChoices(wp.state, p.Choices)
			if err != nil {
				fmt.Fprintf(&b, "  Cannot apply: %s\n", err)
			}
			for _, tier := range []struct{ label, quality string }{{"Deep reasoning", "premium"}, {"Everyday development", "standard"}, {"Lightweight tasks", "economy"}} {
				model := p.EffectiveQualityModels[tier.quality]
				edited := false
				slotQuality := map[string]string{"most_capable.cloud": "premium", "everyday.cloud": "standard", "fast_light.cloud": "economy", "fast_light_text.cloud": "economy"}
				for key, changed := range wp.state.CloudPicksEdited {
					if _, present := wp.state.TierPicks[key]; present && changed && slotQuality[key] == tier.quality {
						edited = true
					}
				}
				if choices != nil && edited {
					model = choices.TierOverrides[tier.quality]
					if model == "" {
						model = "automatic account default"
					}
				}
				if model == "" {
					model = "account default"
				}
				fmt.Fprintf(&b, "  %s: %s\n", tier.label, model)
			}
		} else {
			fmt.Fprintf(&b, "  %s\n", firstNonEmpty(wp.state.CloudProvider, "Not selected"))
		}
	}
	if wizard.ModeUsesOpen(wp.state.LocusMode) {
		b.WriteString("\nLocal models\n")
		order := []string{}
		groups := map[string][]string{}
		displayNames := map[string]string{}
		for _, tier := range openTierOrder() {
			pick := wp.state.TierPicks[wizardOpenTierKey(tier)]
			if pick == "" {
				continue
			}
			if model, ok := openModelByLabel(wp.catalog, pick); ok {
				pick = model.ID
				displayNames[pick] = firstNonEmpty(model.DisplayName, model.ID)
			}
			if _, ok := groups[pick]; !ok {
				order = append(order, pick)
			}
			name := map[string]string{"most_capable": "Deep reasoning", "everyday": "Everyday development", "fast_light": "Lightweight tasks", "fast_light_text": "Lightweight text", "embedding": "Embeddings"}[string(tier)]
			if name == "" {
				name = string(tier)
			}
			groups[pick] = append(groups[pick], name)
		}
		downloads, unknown := 0, 0
		for _, pick := range order {
			fmt.Fprintf(&b, "  %s — %s\n", normalizeModelLabel(firstNonEmpty(displayNames[pick], pick)), strings.Join(groups[pick], ", "))
			model, ok := openModelByLabel(wp.catalog, pick)
			if !wp.catalogOK || !ok {
				unknown++
			} else if model.DownloadState != "downloaded" {
				downloads++
			}
		}
		if len(order) == 0 {
			b.WriteString("  No models selected.\n")
		}
		if downloads > 0 {
			fmt.Fprintf(&b, "\n%d model download(s) after confirmation.\n", downloads)
		}
		if unknown > 0 || !wp.catalogOK {
			b.WriteString("Download requirements could not be fully verified.\n")
		}
		b.WriteString("Local readiness is unverified; runtime checks still apply.\n")
	}
	b.WriteString("\nNothing is applied until you confirm.")
	return b.String()
}
