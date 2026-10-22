package ui

import (
	"fmt"
	"strings"

	"cercano/source/clients/cli/internal/wizard"
	"cercano/source/server/pkg/agentclient"
	"cercano/source/server/pkg/config"
	tea "charm.land/bubbletea/v2"
)

const wizardLocalRAMFloor int64 = 48 << 30

// Eligibility is not runtime validation. Never sum GPUs or borrow system RAM
// for a discrete GPU. Unknown probes cannot enable local recommendations.
func wizardLocalHardware(cat agentclient.RuntimeModelCatalog, catalogOK bool) (bool, int64, string) {
	if !catalogOK || cat.SystemRAMBytes <= 0 {
		return false, 0, "Machine memory could not be determined. Local configurations cannot be recommended yet."
	}
	if cat.SystemRAMBytes < wizardLocalRAMFloor {
		return false, 0, fmt.Sprintf("This machine reports %s of memory. Local setups require at least 48 GB and supported GPU acceleration.", formatBytes(cat.SystemRAMBytes))
	}
	if !cat.HardwareOK {
		return false, 0, "GPU capability could not be verified. Local setups are not offered yet."
	}
	var pool int64
	switch cat.Hardware.Kind {
	case agentclient.HardwareTopologyUnified:
		pool = cat.Hardware.UnifiedMemoryBytes
	case agentclient.HardwareTopologyDiscrete:
		for _, g := range cat.Hardware.GPUs {
			if g.MemoryBytes > pool {
				pool = g.MemoryBytes
			}
		}
	default:
		return false, 0, "Supported GPU acceleration could not be identified."
	}
	if pool <= 0 {
		return false, 0, "GPU memory could not be determined. Local setups are not offered yet."
	}
	if cat.Hardware.Kind == agentclient.HardwareTopologyUnified && pool < wizardLocalRAMFloor {
		return false, 0, "Unified memory is below the 48 GB local baseline."
	}
	memoryKind := "dedicated GPU memory"
	if cat.Hardware.Kind == agentclient.HardwareTopologyUnified {
		memoryKind = "unified memory"
	}
	return true, pool, fmt.Sprintf("%s total system memory (48 GB minimum), GPU detected, and %s %s.", formatBytes(cat.SystemRAMBytes), formatBytes(pool), memoryKind)
}

func wizardFrontierProfile(p agentclient.CloudProfileInfo) bool {
	switch p.Provider {
	case "anthropic", "anthropic-subscription", "openai", "openai-responses":
		return true
	}
	return p.Route == "chatgpt" || (p.Flavor == "messages" && p.Route == "subscription")
}

func (wp *wizardPage) resourceAccounts(frontier bool) []agentclient.CloudProfileInfo {
	var out []agentclient.CloudProfileInfo
	for _, p := range wp.curProfiles {
		if wizardFrontierProfile(p) == frontier && p.HasKey && !wp.resourceExcluded(p.Name) {
			out = append(out, p)
		}
	}
	return out
}

func (wp *wizardPage) localResource() (bool, string) {
	ok, pool, reason := wizardLocalHardware(wp.catalog, wp.catalogOK)
	if !ok {
		return false, reason
	}
	usable := int64(float64(pool) * estimateUsableFraction)
	for _, tier := range openTierOrder() {
		pick, present := wp.state.TierPicks[wizardOpenTierKey(tier)]
		if present && pick == "" && tier != config.TierEmbedding {
			return false, "Choose a local model for every task tier before selecting a local setup."
		}
		if pick == "" && wp.curConfig != nil {
			pick = wp.curConfig.ModelTiers[wizardOpenTierKey(tier)]
		}
		if pick == "" {
			pick = wp.catalog.RecommendedOpenModels[string(tier)]
		}
		if pick == "" && tier == config.TierEmbedding {
			continue
		}
		model, found := openModelByLabel(wp.catalog, pick)
		if !found || model.SizeBytes <= 0 || model.Runtime != "llama_server" || (tier != config.TierEmbedding && !model.SupportsChat) {
			return false, "The hardware meets the baseline, but suitable local model sizes are unknown. A local configuration cannot be recommended yet."
		}
		if model.SizeBytes+estimateOverheadBytes(model.SizeBytes) > usable {
			return false, "The current local model choices exceed the preliminary GPU memory budget. Smaller compatible models are needed."
		}
	}
	return true, reason
}

type wizardPreference struct {
	key, label, description, mode, primary, secondary string
	local                                             bool
}

func (wp *wizardPage) resourcePreferences() []wizardPreference {
	frontier, hosted := wp.resourceAccounts(true), wp.resourceAccounts(false)
	// Preserve the active account within its resource class; do not optimize
	// price/provider rankings or infer account capabilities from a display name.
	preferred := func(ps []agentclient.CloudProfileInfo) string {
		for _, p := range ps {
			if p.Name == wp.curActiveProfile {
				return p.Name
			}
		}
		if len(ps) > 0 {
			return ps[0].Name
		}
		return ""
	}
	f, h := preferred(frontier), preferred(hosted)
	local, _ := wp.localResource()
	var out []wizardPreference
	if f != "" {
		out = append(out, wizardPreference{key: "frontier", label: "Frontier-first", description: "Frontier capabilities without local downloads", mode: "cloud_only", primary: f})
	}
	resources := 0
	for _, available := range []bool{f != "", h != "", local} {
		if available {
			resources++
		}
	}
	if resources >= 2 {
		primary := f
		if primary == "" {
			primary = h
		}
		secondary := ""
		if h != "" && h != primary {
			secondary = h
		}
		mode := "cloud_only"
		if local {
			mode = "cloud_primary"
		}
		parts := []string{primary + " for primary work"}
		if secondary != "" {
			parts = append(parts, secondary+" for routine work")
		}
		if local {
			parts = append(parts, "local background helpers")
		}
		out = append(out, wizardPreference{key: "balanced", label: "Balanced", description: strings.Join(parts, "; "), mode: mode, primary: primary, secondary: secondary, local: local})
	}
	if h != "" {
		out = append(out, wizardPreference{key: "cloud_open", label: wp.hostedPreferenceLabel(h), description: "Hosted models; no local downloads", mode: "cloud_only", primary: h})
	}
	if local {
		out = append(out, wizardPreference{key: "local", label: "Local only", description: "Inference stays on this machine; no cloud fallback", mode: "open_only", local: true})
	}
	return out
}

func (wp *wizardPage) resourceRows() []wizardRow {
	if wp.state.Step == wizard.StepPreference {
		var rows []wizardRow
		for _, p := range wp.resourcePreferences() {
			description := p.description
			if p.key == wp.recommendedPreference() {
				description += " (recommended)"
			}
			rows = append(rows, wizardRow{Key: p.key, Label: p.label, Annotation: description})
		}
		if len(rows) == 0 || !wp.routingOK {
			return []wizardRow{{Key: "resources", Label: "Back to cloud providers"}}
		}
		return rows
	}
	var rows []wizardRow
	for _, p := range wp.curProfiles {
		if !p.HasKey {
			rows = append(rows, wizardRow{Key: "reconnect:" + p.Name, Label: "Reconnect " + p.Name, Annotation: "credentials unavailable"})
			continue
		}
		annotation := "selected — credentials connected"
		if wp.resourceExcluded(p.Name) {
			annotation = "not selected"
		}
		rows = append(rows, wizardRow{Key: "connected:" + p.Name, Label: p.Name, Annotation: annotation})
	}
	rows = append(rows, wizardRow{Key: "add_provider", Label: "Add cloud provider…", Annotation: "Claude, ChatGPT/Codex, DeepInfra, Mistral, and more"})
	label := "Continue"
	if len(wp.resourceAccounts(true))+len(wp.resourceAccounts(false)) == 0 {
		label = "Continue without connecting"
	}
	return append(rows, wizardRow{Key: "resource_next", Label: label})
}

func (wp *wizardPage) resourceDescription() string {
	if wp.state.Step != wizard.StepPreference {
		return "Add your cloud providers\n\nConnect Claude, ChatGPT/Codex, DeepInfra, Mistral, or another supported provider. Add as many cloud providers as you want, including additional accounts with the same provider. Extra accounts can be configured as backups in /config.\n\nYou can add another account after signing in, or continue without one. Connected accounts remain if you cancel setup."
	}
	var b strings.Builder
	b.WriteString("How would you like Cercano to work?\n\n")
	accounts := len(wp.resourceAccounts(true)) + len(wp.resourceAccounts(false))
	if accounts > 0 {
		fmt.Fprintf(&b, "* **Cloud** - %d selected account(s) with credentials.\n", accounts)
	} else {
		b.WriteString("* **Cloud** - no connected accounts selected.\n")
	}
	ok, why := wp.localResource()
	if ok {
		fmt.Fprintf(&b, "* **Local models** - Eligible: %s\n", why)
	} else {
		fmt.Fprintf(&b, "* **Local models** - Not offered: %s\n", why)
	}
	if !wp.routingOK {
		b.WriteString("\nCurrent routing could not be read safely. Go back to cloud providers and continue again.")
	}
	if len(wp.resourcePreferences()) == 0 {
		b.WriteString("\nNo configuration is currently available. Go back to connect a cloud provider, or resolve local eligibility before resuming setup.")
	} else if wp.routingOK {
		b.WriteString("\nChoose a configuration below. You can customize its models next.")
	}
	if ok {
		b.WriteString("\nLocal eligibility is preliminary; runtime checks still apply.")
	}
	return b.String()
}

func (wp *wizardPage) selectResourceRow(row wizardRow) (tea.Cmd, bool) {
	if wp.state.Step == wizard.StepPreference {
		if row.Key == "resources" {
			wp.state.Step = wizard.StepResources
			wp.state.ResourcePage = 0
			wp.cursor = 0
			wp.persist()
			return nil, false
		}
		for _, p := range wp.resourcePreferences() {
			if p.key == row.Key {
				wp.chooseResourcePreference(p)
				return nil, false
			}
		}
		wp.status = "Resources changed; review available setups again."
		return nil, false
	}
	if strings.HasPrefix(row.Key, "connected:") {
		name := strings.TrimPrefix(row.Key, "connected:")
		if wp.resourceExcluded(name) {
			var kept []string
			for _, n := range wp.state.ExcludedProfiles {
				if n != name {
					kept = append(kept, n)
				}
			}
			wp.state.ExcludedProfiles = kept
		} else {
			wp.state.ExcludedProfiles = append(wp.state.ExcludedProfiles, name)
		}
		wp.persist()
		return nil, false
	}
	if strings.HasPrefix(row.Key, "reconnect:") {
		return func() tea.Msg {
			return wizardAddProviderMsg{page: wp, profile: strings.TrimPrefix(row.Key, "reconnect:")}
		}, false
	}
	switch row.Key {
	case "add_provider":
		return func() tea.Msg { return wizardAddProviderMsg{page: wp} }, false
	case "resource_next":
		// Discovery is automatic here, not another user-facing setup action.
		if wp.agent != nil {
			wp.loadProviders()
			wp.loadRuntimeCatalog()
			wp.discoverResources()
		}
		wp.state.Step = wizard.StepPreference
		wp.state.ResourcePage = 0
	default:
		return nil, false
	}
	wp.cursor = wp.defaultCursor()
	wp.persist()
	return nil, false
}

func (wp *wizardPage) chooseResourcePreference(p wizardPreference) {
	if wp.state.Preference != p.key {
		wp.state.ReplaceTaskOverrides = false
	}
	wp.state.Preference = p.key
	wp.state.LocusMode = p.mode
	wp.state.UseLocal = p.local
	wp.state.SecondaryProfile = p.secondary
	if p.primary == "" {
		wp.state.CloudProfile = ""
	}
	wp.state.CloudProvider = ""
	wp.state.AuthMethod = ""
	if p.primary != "" {
		if profile := wp.profileByName(p.primary); profile != nil {
			wp.selectExistingAccount(profile)
			wp.state.CloudProvider = firstNonEmpty(profile.Provider, profile.Name)
			wp.state.AuthMethod = "existing"
		}
	}
	wp.autofillTiers()
	wp.state.Step = wizard.StepDone
	wp.showDetails = false
	wp.cursor = 0
	wp.persist()
}

func (wp *wizardPage) resourcePreferenceName() string {
	switch wp.state.Preference {
	case "frontier":
		return "Frontier-first"
	case "balanced":
		return "Balanced"
	case "cloud_open":
		return wp.hostedPreferenceLabel(wp.state.CloudProfile)
	case "local":
		return "Local only"
	}
	return "Setup"
}

func (wp *wizardPage) resourceExcluded(name string) bool {
	for _, n := range wp.state.ExcludedProfiles {
		if n == name {
			return true
		}
	}
	return false
}

func (wp *wizardPage) recommendedPreference() string {
	ps := wp.resourcePreferences()
	for _, p := range ps {
		if p.key == wp.state.Preference {
			return p.key
		}
	}
	if wp.curConfig != nil {
		wanted := ""
		switch wp.curConfig.LocusMode {
		case "open_only":
			wanted = "local"
		case "cloud_primary", "open_primary":
			wanted = "balanced"
		case "cloud_only":
			wanted = "frontier"
		}
		for _, p := range ps {
			if p.key == wanted {
				return p.key
			}
		}
	}
	for _, p := range ps {
		if p.key == "balanced" {
			return p.key
		}
	}
	if len(ps) > 0 {
		return ps[0].key
	}
	return ""
}

func (wp *wizardPage) hostedPreferenceLabel(name string) string {
	if p := wp.profileByName(name); p != nil {
		switch p.Provider {
		case "deepinfra", "mistral", "groq", "together", "fireworks", "cerebras":
			return "Cloud open models"
		}
	}
	return "Hosted cloud models"
}
