package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cercano/source/clients/cli/internal/overlay"
	"cercano/source/server/pkg/agentclient"
	tea "charm.land/bubbletea/v2"
)

type cloudCatalogKickMsg struct{ page *settingsPage }
type cloudCatalogResultMsg struct {
	page       *settingsPage
	generation uint64
	scope      string
	browse     bool
	models     []agentclient.CloudModelInfo
	catalog    *agentclient.RuntimeModelCatalog
	err        string
}

func (sp *settingsPage) catalogScope() string {
	d := sp.cloudDraft
	return strings.Join([]string{sp.cloudSelected, d.Name, d.Provider, d.Flavor, d.Backend, d.BaseURL, d.Route}, "\x00")
}
func (sp *settingsPage) invalidateCatalog() {
	sp.catalogGeneration++
	if sp.catalogCancel != nil {
		sp.catalogCancel()
		sp.catalogCancel = nil
	}
	sp.catalogBusy = false
	sp.cloudModels = nil
	sp.cloudModelsFetched = false
	sp.cloudModelsError = ""
	sp.cloudProviderCatalog = nil
}
func (sp *settingsPage) catalogRow() cloudRow {
	if strings.HasPrefix(sp.cloudSelected, "template:") {
		for _, p := range sp.cloudView.Providers {
			if "template:"+p.ID == sp.cloudSelected {
				preset := presetFromProvider(p)
				return cloudRow{ID: sp.cloudSelected, Label: p.Label, Preset: &preset}
			}
		}
	}
	for _, r := range buildCloudRowsFromProviders(sp.cloudView) {
		if r.ID == sp.cloudSelected {
			return r
		}
	}
	return cloudRow{IsProfile: !sp.cloudDraftNew}
}

// Credential drafts never count as stored credentials, and catalog requests
// always address the saved profile. Unknown/custom endpoint auth is not guessed.
func (sp *settingsPage) catalogPrerequisite() (string, string) {
	d := sp.cloudDraft
	r := sp.catalogRow()
	subscription := d.Route == "subscription" || d.Route == "chatgpt" || d.Flavor == "responses"
	if subscription && sp.cloudDraftNew {
		key := "cloud-signin"
		if d.Route == "subscription" {
			key = "cloud-signin-claude"
		}
		return "Sign in to load available models.", key
	}
	if d.apiKeyEdited {
		return "Save this account to load its available models.", "cloud-save"
	}
	for _, p := range sp.profiles {
		if p.Name == d.Name && !sp.cloudDraftNew {
			if p.Flavor != d.Flavor || p.BaseURL != d.BaseURL || p.Backend != d.Backend || p.Route != d.Route || p.Provider != d.Provider {
				return "Save this account to load its available models.", "cloud-save"
			}
		}
	}
	requiresKey := r.Preset != nil && r.Preset.ID != "deepinfra" && d.Flavor != "bedrock" && !subscription
	if requiresKey && !r.HasKey {
		return "Add an API key to load available models.", "cloud-key"
	}
	if sp.cloudDraftNew {
		return "Save this account to load its available models.", "cloud-save"
	}
	return "", ""
}
func (sp *settingsPage) catalogState() overlay.CatalogState {
	if msg, field := sp.catalogPrerequisite(); msg != "" {
		label := "Add API key"
		if field == "cloud-save" {
			label = "Return to Save"
		} else if strings.HasPrefix(field, "cloud-signin") {
			label = "Sign in"
		}
		return overlay.CatalogState{Message: msg, Action: overlay.CatalogCredentials, ActionLabel: label}
	}
	if sp.catalogBusy {
		message := "Loading available models…"
		if sp.cloudModelsFetched && len(sp.cloudModels) > 0 {
			message = "Refreshing available models…"
		}
		return overlay.CatalogState{Message: message, Busy: true}
	}
	if sp.cloudModelsError != "" {
		return overlay.CatalogState{Message: "Couldn’t load available models. " + sp.cloudModelsError, ActionLabel: "Retry", Action: overlay.CatalogRefresh}
	}
	if sp.cloudModelsFetched && len(sp.cloudModels) == 0 {
		return overlay.CatalogState{Message: "No models were returned for this account.", ActionLabel: "Refresh", Action: overlay.CatalogRefresh}
	}
	if !sp.cloudModelsFetched {
		return overlay.CatalogState{Message: "Available models have not been loaded.", ActionLabel: "Load models", Action: overlay.CatalogRefresh}
	}
	return overlay.CatalogState{}
}
func (sp *settingsPage) catalogKick() tea.Cmd {
	return func() tea.Msg { return cloudCatalogKickMsg{page: sp} }
}
func (sp *settingsPage) startCatalog(force, browse bool) tea.Cmd {
	if sp.agent == nil || sp.cloudSelected == "" {
		return nil
	}
	if !browse {
		if msg, _ := sp.catalogPrerequisite(); msg != "" {
			sp.updateCatalogPicker()
			return nil
		}
	}
	if sp.catalogBusy {
		return nil
	}
	if !force && !browse && sp.cloudModelsFetched {
		sp.updateCatalogPicker()
		return nil
	}
	sp.catalogGeneration++
	gen := sp.catalogGeneration
	scope := sp.catalogScope()
	name := sp.cloudDraft.Name
	client := sp.agent
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	sp.catalogCancel = cancel
	sp.catalogBusy = true
	sp.updateCatalogPicker()
	return func() tea.Msg {
		defer cancel()
		out := cloudCatalogResultMsg{page: sp, generation: gen, scope: scope, browse: browse}
		if browse {
			cat, err := client.ListRuntimeModels(ctx)
			if err != nil {
				out.err = "Discovery unavailable"
			} else {
				out.catalog = &cat
			}
		} else {
			models, problem, err := client.ListCloudProfileModels(ctx, name)
			out.models = models
			out.err = problem
			if err != nil {
				out.err = "Discovery unavailable"
			}
		}
		return out
	}
}
func (sp *settingsPage) applyCatalogResult(msg cloudCatalogResultMsg) {
	if msg.page != sp || msg.generation != sp.catalogGeneration || msg.scope != sp.catalogScope() {
		return
	}
	sp.catalogBusy = false
	sp.catalogCancel = nil
	sp.cloudModelsError = msg.err
	if msg.browse {
		sp.cloudProviderCatalog = msg.catalog
	} else {
		sp.cloudModelsFetched = true
		sp.cloudModels = msg.models
		if msg.err != "" {
			sp.cloudModels = nil
		}
	}
	sp.updateCatalogPicker()
	sp.reloadCatalogForm()
}
func catalogCapability(v agentclient.CatalogCapability) string {
	switch v {
	case agentclient.CatalogCapabilitySupported:
		return "Supported"
	case agentclient.CatalogCapabilityUnsupported:
		return "Unsupported"
	default:
		return "Unknown"
	}
}
func catalogEntry(m agentclient.CloudModelInfo, account string) overlay.CatalogEntry {
	e := overlay.CatalogEntry{ID: m.ID, Name: m.DisplayName, Account: account}
	if e.Name == "" {
		e.Name = m.ID
	}
	md := m.Metadata
	if md == nil {
		e.Details = []overlay.CatalogDetail{{Label: "Capabilities", Value: "Unknown"}, {Label: "Context", Value: "Unknown"}, {Label: "Pricing", Value: "Unknown"}}
		return e
	}
	e.Aliases = md.Aliases
	for _, c := range []struct {
		name  string
		value agentclient.CatalogCapability
	}{{"Chat", md.Chat}, {"Tools", md.Tools}, {"Vision", md.Vision}, {"Reasoning", md.Reasoning}} {
		e.Details = append(e.Details, overlay.CatalogDetail{Label: c.name, Value: catalogCapability(c.value)})
	}
	contextLabel := "Unknown"
	if md.ContextLength > 0 {
		contextLabel = fmt.Sprintf("%s tokens", commaNumber(md.ContextLength))
	}
	e.Details = append(e.Details, overlay.CatalogDetail{Label: "Context", Value: contextLabel})
	for _, p := range []struct {
		name  string
		value *int64
	}{{"Input / 1M tokens", md.PriceIn}, {"Output / 1M tokens", md.PriceOut}} {
		v := "Unknown"
		if p.value != nil {
			v = fmt.Sprintf("$%.4g", float64(*p.value)/1e6)
		}
		e.Details = append(e.Details, overlay.CatalogDetail{Label: p.name, Value: v})
	}
	if len(md.Aliases) > 0 {
		e.Details = append(e.Details, overlay.CatalogDetail{Label: "Aliases", Value: strings.Join(md.Aliases, ", ")})
	}
	if md.Deprecated {
		e.Details = append(e.Details, overlay.CatalogDetail{Label: "Lifecycle", Value: "Deprecated"})
	}
	if md.RetirementAt != "" {
		e.Details = append(e.Details, overlay.CatalogDetail{Label: "Retires", Value: md.RetirementAt})
	}
	if md.ReplacedBy != "" {
		e.Details = append(e.Details, overlay.CatalogDetail{Label: "Replacement", Value: md.ReplacedBy})
	}
	return e
}
func commaNumber(n int64) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
func (sp *settingsPage) pickerEntries() []overlay.CatalogEntry {
	if sp.catalogPicker != nil && sp.catalogPicker.Browse {
		var entries []overlay.CatalogEntry
		if sp.cloudProviderCatalog != nil {
			for _, m := range sp.cloudProviderCatalog.Models {
				if m.CatalogSource != "mistral" {
					continue
				}
				entry := overlay.CatalogEntry{ID: m.CatalogID, Name: m.DisplayName}
				if entry.Name == "" {
					entry.Name = m.CatalogID
				}
				for _, obs := range m.AccountObservations {
					perAccount := catalogEntry(agentclient.CloudModelInfo{ID: m.CatalogID, DisplayName: m.DisplayName, Metadata: obs.Metadata}, obs.Profile)
					entry.Accounts = append(entry.Accounts, obs.Profile)
					for _, detail := range perAccount.Details {
						detail.Account = obs.Profile
						entry.Details = append(entry.Details, detail)
					}
				}
				entries = append(entries, entry)
			}
		}
		return entries
	}
	row := sp.catalogRow()
	mistral := sp.cloudDraft.Provider == "mistral" || (row.Preset != nil && row.Preset.ID == "mistral")
	image := sp.catalogField == "cloud-image"
	current := ""
	if image {
		current = sp.cloudDraft.Choices.Clone().ImageModel
	} else {
		current = sp.cloudDraft.Choices.Clone().TierOverrides[strings.TrimPrefix(sp.catalogField, "cloud-quality-")]
	}
	special := overlay.CatalogEntry{Name: "Use inherited recommendation", Action: overlay.CatalogInherit, Current: current == ""}
	if image {
		special.Name = "No image override"
		special.Action = overlay.CatalogNone
	}
	if image {
		special.Details = []overlay.CatalogDetail{{Label: "Effect", Value: "Do not use an account-specific image override"}}
	} else {
		quality := strings.TrimPrefix(sp.catalogField, "cloud-quality-")
		special.Details = []overlay.CatalogDetail{{Label: "Recommended model", Value: sp.cloudDraft.Effective[quality]}, {Label: "Effect", Value: "Follow the provider recommendation after Save"}}
	}
	entries := []overlay.CatalogEntry{special}
	seen := false
	models := sp.cloudModels
	fallback := false
	if len(models) == 0 && sp.cloudModelsFetched && sp.cloudModelsError != "" && sp.cloudDraft.Flavor == "messages" {
		models = fallbackClaudeModels()
		fallback = true
	}
	for _, m := range models {
		e := catalogEntry(m, sp.cloudDraft.Name)
		e.Current = m.ID == current
		seen = seen || e.Current
		e.Unconfirmed = fallback
		if fallback {
			e.Details = append(e.Details, overlay.CatalogDetail{Label: "Source", Value: "Static fallback; access unconfirmed"})
		}
		if m.Metadata != nil {
			md := m.Metadata
			if image && md.Vision != agentclient.CatalogCapabilitySupported {
				e.DisabledReason = "Confirmed vision capability required"
			}
			if !image && ((md.Chat == agentclient.CatalogCapabilityUnsupported || md.Tools == agentclient.CatalogCapabilityUnsupported) || (mistral && (md.Chat != agentclient.CatalogCapabilitySupported || md.Tools != agentclient.CatalogCapabilitySupported))) {
				e.DisabledReason = "Chat and tool support required"
			}
			if md.RetirementAt != "" {
				if retired, err := time.Parse(time.RFC3339, md.RetirementAt); err == nil && !retired.After(time.Now()) {
					e.DisabledReason = "Model retired"
				}
			}
		} else if image {
			e.DisabledReason = "Vision capability unknown"
		} else if mistral {
			e.DisabledReason = "Chat and tool capabilities unconfirmed"
		}
		entries = append(entries, e)
	}
	if current != "" && !seen {
		entries = append(entries, overlay.CatalogEntry{ID: current, Name: current, Account: sp.cloudDraft.Name, Current: true, Unconfirmed: true, Details: []overlay.CatalogDetail{{Label: "Availability", Value: "Not in current catalog; access unconfirmed"}}})
	}
	return append(entries, overlay.CatalogEntry{Name: "Enter custom model ID…", Action: overlay.CatalogCustom})
}
func (sp *settingsPage) updateCatalogPicker() {
	if sp.catalogPicker == nil {
		return
	}
	sp.catalogPicker.SetEntries(sp.pickerEntries())
	if !sp.catalogPicker.Browse {
		sp.catalogPicker.State = sp.catalogState()
		return
	}
	state := overlay.CatalogState{Busy: sp.catalogBusy}
	if sp.cloudModelsError != "" {
		state.Message = "Couldn’t load provider catalog. " + sp.cloudModelsError
		state.Action = overlay.CatalogRefresh
		state.ActionLabel = "Retry"
	}
	if sp.cloudProviderCatalog != nil {
		var failures []string
		for _, s := range sp.cloudProviderCatalog.CatalogAccountStatus {
			if s.Error != "" {
				failures = append(failures, s.Profile+": "+s.Error)
			}
		}
		if len(failures) > 0 {
			state.Message = "Partial catalog — " + strings.Join(failures, "; ")
		}
	}
	sp.catalogPicker.State = state
}
func (sp *settingsPage) openCatalog(field string, browse bool) tea.Cmd {
	sp.catalogField = field
	title := "Choose a model · " + strings.TrimPrefix(field, "cloud-quality-")
	if field == "cloud-image" {
		title = "Choose an image model"
	}
	if browse {
		title = "Mistral models · Across accounts"
	}
	c := overlay.NewCatalog(title, sp.cloudDraft.Name, browse)
	sp.catalogPicker = &c
	sp.updateCatalogPicker()
	if browse {
		if sp.catalogBusy {
			sp.invalidateCatalog()
		}
		return sp.startCatalog(true, true)
	}
	return sp.startCatalog(false, false)
}
func (sp *settingsPage) focusCloudField(key string) {
	if sp.form == nil {
		return
	}
	index := 0
	for _, section := range sp.form.Sections {
		for _, field := range section.Fields {
			if field.Key() == key {
				sp.form.SetCursor(index)
				sp.scrollToFocus()
				return
			}
			index++
		}
	}
}
func (sp *settingsPage) updateCatalogKey(msg tea.Msg) tea.Cmd {
	sp.catalogPicker.Resize(min(sp.width, 120), min(sp.viewportHeight(), 36))
	cmd, out := sp.catalogPicker.Update(msg)
	switch out.Action {
	case overlay.CatalogCancel:
		sp.catalogPicker = nil
		if sp.catalogBusy {
			sp.invalidateCatalog()
		}
		return nil
	case overlay.CatalogRefresh:
		return sp.startCatalog(true, sp.catalogPicker.Browse)
	case overlay.CatalogCredentials:
		_, field := sp.catalogPrerequisite()
		sp.catalogPicker = nil
		sp.focusCloudField(field)
		return nil
	case overlay.CatalogCustom:
		field := "cloud-custom-" + strings.TrimPrefix(sp.catalogField, "cloud-quality-")
		if sp.catalogField == "cloud-image" {
			field = "cloud-custom-image"
		}
		revealCloudCustomEntry(sp.catalogField)
		sp.catalogPicker = nil
		sp.form.Reload()
		sp.focusCloudField(field)
		return nil
	case overlay.CatalogSelect, overlay.CatalogInherit, overlay.CatalogNone:
		if sp.catalogPicker.Browse {
			return nil
		}
		if out.Action == overlay.CatalogSelect && out.Account != sp.cloudDraft.Name {
			return nil
		}
		sp.applyCloudDraftEdit(sp.catalogField, out.ID)
		sp.catalogPicker = nil
		sp.form.Reload()
		return nil
	}
	return cmd
}

// Async discovery must never reconstruct an editor holding uncommitted input.
func (sp *settingsPage) reloadCatalogForm() {
	if sp.form == nil {
		return
	}
	for _, section := range sp.form.Sections {
		for _, field := range section.Fields {
			if field.Editing() {
				sp.catalogReloadPending = true
				return
			}
		}
		for _, group := range section.Groups {
			for _, field := range group.Fields {
				if field.Editing() {
					sp.catalogReloadPending = true
					return
				}
			}
		}
	}
	sp.catalogReloadPending = false
	sp.form.Reload()
}
