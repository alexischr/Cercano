package ui

import (
	"cercano/source/clients/cli/internal/form"
	"cercano/source/server/pkg/agentclient"
	"cercano/source/server/pkg/config"
	"fmt"
	"strings"
)

// fallbackClaudeModels is the curated static catalog rendered when the live
// /v1/models fetch through the profile's endpoint fails (Meridian offline,
// endpoint 404, timeout, etc.). Kept intentionally short — the current
// major Claude models the user most likely wants — so drift when Anthropic
// ships a new model just means the fetched dropdown is the definitive source
// on a running Meridian, and the static list is the "you'll always have
// something to click" safety net.
//
// Update in tandem when Anthropic promotes new default models.
func fallbackClaudeModels() []agentclient.CloudModelInfo {
	out := []agentclient.CloudModelInfo{}
	for _, m := range config.ClaudeModelChoices() {
		out = append(out, agentclient.CloudModelInfo{ID: m.ID, DisplayName: m.DisplayName})
	}
	return out
}

// modelOptionsFromCatalog turns a []CloudModelInfo into form.Options for a
// Select field. Ensures currentID is present as an option even when the
// live fetch didn't return it (so a legacy or custom model doesn't get
// dropped just because the catalog doesn't mention it) and always tacks on
// a "custom…" escape hatch so power users can type any ID.
func modelOptionsFromCatalog(models []agentclient.CloudModelInfo, currentID string) []form.Option {
	seen := map[string]bool{}
	out := make([]form.Option, 0, len(models)+2)
	foundCurrent := false
	for _, m := range models {
		if m.ID == currentID {
			foundCurrent = true
			break
		}
	}
	if currentID != "" && !foundCurrent {
		out = append(out, form.Option{Label: currentID + " (not in current catalog; access unconfirmed)", Value: currentID})
		seen[currentID] = true
	}
	for _, m := range models {
		if seen[m.ID] {
			continue
		}
		label := m.DisplayName
		if label == "" {
			label = m.ID
		}
		label = cloudModelMetadataLabel(m, label)
		out = append(out, form.Option{Label: label, Value: m.ID})
		seen[m.ID] = true
	}
	return out
}

// cloudModelMetadataLabel describes only metadata returned for this account.
// Display annotations never replace the exact provider model ID used as Value.
func cloudModelMetadataLabel(m agentclient.CloudModelInfo, label string) string {
	if m.Metadata == nil {
		return label
	}
	md := m.Metadata
	details := []string{}
	if md.ContextLength > 0 {
		details = append(details, fmt.Sprintf("%d context", md.ContextLength))
	}
	capability := func(name string, value agentclient.CatalogCapability) string {
		switch value {
		case agentclient.CatalogCapabilitySupported:
			return name + " supported"
		case agentclient.CatalogCapabilityUnsupported:
			return name + " unsupported"
		default:
			return name + " unknown"
		}
	}
	details = append(details, capability("chat", md.Chat), capability("tools", md.Tools), capability("vision", md.Vision), capability("reasoning", md.Reasoning))
	if md.PriceIn == nil || md.PriceOut == nil {
		details = append(details, "pricing unknown")
	} else {
		details = append(details, fmt.Sprintf("$%.4g/$%.4g per M tokens", float64(*md.PriceIn)/1e6, float64(*md.PriceOut)/1e6))
	}
	if len(md.Aliases) > 0 {
		details = append(details, "aliases: "+strings.Join(md.Aliases, ", "))
	}
	if md.Deprecated {
		details = append(details, "deprecated")
	}
	if md.RetirementAt != "" {
		details = append(details, "retires "+md.RetirementAt)
	}
	if md.ReplacedBy != "" {
		details = append(details, "replacement: "+md.ReplacedBy)
	}
	accounts := []string{}
	for _, obs := range m.AccountObservations {
		accounts = append(accounts, obs.Profile)
	}
	if len(accounts) > 0 {
		details = append(details, "accounts: "+strings.Join(accounts, ", "))
	}
	return label + " — " + strings.Join(details, "; ")
}
