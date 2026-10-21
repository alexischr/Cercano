package ui

import (
	"cercano/source/server/pkg/agentclient"
	"cercano/source/server/pkg/config"
	"fmt"

	"cercano/source/clients/cli/internal/form"
)

// cloudDraft is the in-progress profile edit backing the detail editor.
type cloudDraft struct {
	// Pending credentials are memory-only and never included in profile metadata.
	apiKey                                       string
	apiKeyEdited                                 bool
	Name, Flavor, Backend, Route, BaseURL, Model string
	Choices                                      *agentclient.CloudModelChoices
	Effective                                    map[string]string
	Provider, Region, AWSProfile                 string
}

// cloudCustomEntry tracks which editor fields got their "Enter custom model ID…"
// text input revealed via the catalog picker. The inputs stay hidden until the
// picker's custom action is chosen so the editor never demands free-form model
// IDs up front.
var cloudCustomEntry map[string]bool

func (sp *settingsPage) resetCloudCustomEntry() {
	cloudCustomEntry = nil
}

// shouldShowCloudCustomEntry reports whether the picker's "custom model ID"
// action has revealed the free-form text field for this choice slot.
func shouldShowCloudCustomEntry(field string) bool {
	return cloudCustomEntry[field]
}

// revealCloudCustomEntry records that the picker's custom action was chosen for
// a choice slot, so subsequent form snapshots keep its text field visible.
func revealCloudCustomEntry(field string) {
	if cloudCustomEntry == nil {
		cloudCustomEntry = make(map[string]bool)
	}
	cloudCustomEntry[field] = true
}

// selectCloudRow expands a list row's detail editor and seeds the draft from the
// matching configured profile (edit) or preset template (create). Also clears
// the cloud-model catalog cache so the next detail-fields build fetches a
// fresh catalog for THIS profile's endpoint rather than reusing a stale one
// from the previously-selected row.
func (sp *settingsPage) selectCloudRow(rowID string) {
	sp.invalidateCatalog()
	sp.catalogPicker = nil
	sp.cloudEditorOpen = rowID != "" && (sp.cloudEditorActive() || len(rowID) > 8 && rowID[:8] == "profile:")
	sp.offset = 0
	sp.cloudSelected = rowID
	sp.cloudAdvanced = false
	sp.cloudModelsExpanded = false
	sp.cloudDeleteConfirm = false
	sp.cloudDirty = false
	sp.cloudSaveFailed = false
	sp.cloudDraft = cloudDraft{Choices: (&agentclient.CloudModelChoices{}).Clone()}
	sp.cloudDraftNew = true
	sp.cloudModels = nil
	sp.cloudModelsFetched = false
	sp.cloudModelsError = ""
	sp.cloudProviderCatalog = nil
	sp.resetCloudCustomEntry()
	switch {
	case rowID == "other":
		sp.cloudDraft.Flavor = "chat_completions"
	case len(rowID) > 8 && rowID[:8] == "profile:":
		name := rowID[8:]
		for _, p := range sp.profiles {
			if p.Name == name {
				sp.cloudDraft = cloudDraft{Name: p.Name, Flavor: p.Flavor, Backend: p.Backend, Route: p.Route, BaseURL: p.BaseURL, Model: p.Model, Choices: p.Choices.Clone(), Effective: p.RecommendedQualityModels, Provider: p.Provider, Region: p.Region, AWSProfile: p.AWSProfile}
				sp.cloudDraftNew = false
				return
			}
		}
	case len(rowID) > 9 && rowID[:9] == "template:":
		id := rowID[9:]
		for _, prov := range sp.cloudView.Providers {
			if prov.ID == id {
				sp.cloudDraft = cloudDraft{Name: prov.ID, Flavor: prov.Flavor, Backend: prov.Backend, Route: prov.Route, BaseURL: prov.BaseURL, Choices: (&agentclient.CloudModelChoices{}).Clone()}
				// Recommendations are baked provider defaults, not authenticated
				// discovery results. Show them even before this account is saved.
				profile := config.CloudProfile{Flavor: prov.Flavor, Backend: prov.Backend, Route: prov.Route, BaseURL: prov.BaseURL}
				models := config.Defaults().ModelProfiles
				sp.cloudDraft.Effective = map[string]string{}
				for _, quality := range []config.CostTier{config.CostEconomy, config.CostStandard, config.CostPremium} {
					sp.cloudDraft.Effective[string(quality)] = models.ResolveCloudModelForTier(profile, quality.CapabilityTier())
				}
				return
			}
		}
	}
}

// buildCloudSection supplies the configured-provider modal form when open,
// otherwise the provider list and any new-provider setup draft.
func (sp *settingsPage) buildCloudSection() form.Section {
	// Save keeps the draft selected while the profile snapshot reloads. Refresh
	// derived recommendations without reselecting (and discarding draft edits).
	if !sp.cloudDraftNew {
		for _, p := range sp.profiles {
			if sp.cloudSelected == "profile:"+p.Name && sp.cloudDraft.Name == p.Name {
				sp.cloudDraft.Effective = p.RecommendedQualityModels
				break
			}
		}
	}
	if sp.cloudEditorActive() {
		fields := sp.cloudDetailFields(sp.catalogRow())
		return form.Section{Title: "Edit provider", Fields: fields}
	}
	section := sp.cloudProviderListSection()
	if sp.cloudSelected != "" {
		section.Fields = append(section.Fields, sp.cloudDetailFields(sp.catalogRow())...)
	}
	return section
}

func (sp *settingsPage) cloudProviderListSection() form.Section {
	rows := buildCloudRowsFromProviders(sp.cloudView)
	var fields []form.Field
	seen := map[string]bool{}
	for _, r := range rows {
		if !r.IsProfile || r.SubProfile {
			continue
		}
		label := r.Label
		if r.Preset != nil {
			family := cloudProviderFamily(r.Preset.ID)
			if seen[family] {
				continue
			}
			seen[family] = true
			label = cloudProviderLabel(r.Preset.ID, r.Preset.Label)
		}
		fields = append(fields, form.NewRow("cloud-row:"+r.ID, label, authHint(r), r.Active))
	}
	if len(fields) == 0 {
		fields = append(fields, form.NewReadOnly("cloud-empty", "Providers", "No providers configured", ""))
	}
	fields = append(fields, form.NewButton("cloud-add-provider", "+ Add provider…", true))
	return form.Section{Title: "Cloud Providers", Fields: fields}
}

// cloudDetailFields are shared by the provider editor popover and setup draft.
func (sp *settingsPage) cloudDetailFields(r cloudRow) []form.Field {
	d := sp.cloudDraft
	var out []form.Field
	if r.Preset != nil && !sp.cloudDraftNew {
		var options []form.Option
		for _, provider := range sp.cloudView.Providers {
			if cloudProviderFamily(provider.ID) != cloudProviderFamily(r.Preset.ID) {
				continue
			}
			for _, account := range provider.Profiles {
				options = append(options, form.Option{Label: account.AccountDisplayNameLabel(), Value: "profile:" + account.Name})
			}
		}
		if len(options) > 1 {
			out = append(out, form.NewSelect("cloud-account-select", "Account", options, sp.cloudSelected))
		}
	}
	out = append(out, form.NewHeading("cloud-connection-heading", "Connection"))
	if sp.cloudDraftNew {
		out = append(out, form.NewText("cloud-name", "Account name", d.Name, "A unique name for this connection"))
	} else {
		account := agentclient.CloudProfileInfo{Name: d.Name, Flavor: d.Flavor, Route: d.Route}
		for _, p := range sp.profiles {
			if p.Name == d.Name {
				account = p
				break
			}
		}
		hint := "Credentials are saved separately for this account"
		if account.AccountEmail == "" && account.AccountDisplayName == "" && (account.Route == "subscription" || account.Route == "chatgpt") {
			hint = "Sign in again to identify this account; existing credentials are unchanged"
		}
		out = append(out, form.NewReadOnly("cloud-account", "Account", account.AccountDisplayNameLabel(), hint))
	}
	// ChatGPT subscription sign-in: the responses flavor can authenticate with
	// a ChatGPT Plus/Pro subscription via device-code OAuth instead of an API
	// key. Offer the button on responses rows; the api-key field stays as the
	// sanctioned fallback one row below.
	if r.Preset != nil && r.Preset.Flavor == "responses" {
		out = append(out, form.NewButton("cloud-signin", "sign in with ChatGPT", true))
	}
	// Claude subscription sign-in: only show the OAuth action for an existing
	// subscription profile, or for the bare anthropic template that creates one.
	// Direct API-key Anthropic profiles use the key field instead; offering both
	// auth paths on those rows makes it look like two separate profiles should be
	// signed into.
	if sp.shouldShowClaudeSignIn(r, d) {
		out = append(out, form.NewButton("cloud-signin-claude", "sign in with Claude (subscription)", true))
	}
	if d.Route == "subscription" {
		out = append(out, form.NewReadOnly("cloud-route", "Authentication", "Subscription sign-in", ""))
	}
	if d.Route != "subscription" && d.Flavor != "responses" {
		out = append(out, form.NewMasked("cloud-key", "API key", sp.draftHasKey(r)))
	}
	if sp.cloudDraftNew && r.Preset != nil {
		family := cloudProviderFamily(r.Preset.ID)
		if family == "anthropic" || family == "openai" {
			label := "Use API key instead"
			if d.Route != "subscription" && d.Flavor != "responses" {
				label = "Use subscription sign-in instead"
			}
			out = append(out, form.NewButton("cloud-auth-switch", label, true))
		}
	}
	modelLabel := "▸ Model tiers"
	if sp.cloudModelsExpanded {
		modelLabel = "▾ Model tiers"
	}
	out = append(out, form.NewButton("cloud-models", modelLabel, true))
	if sp.cloudModelsExpanded {

		out = append(out, sp.cloudChoiceFields(r)...)
		state := sp.catalogState()
		if state.Message != "" {
			out = append(out, form.NewReadOnly("cloud-catalog-state", "Model catalog", state.Message, "Edits save automatically when committed"))
		}
		if state.Action != 0 {
			out = append(out, form.NewButton("cloud-catalog-action", state.ActionLabel, true))
		}
		if r.Preset != nil && r.Preset.ID == "mistral" {
			out = append(out, sp.mistralProviderCatalogFields()...)
		}
		if sp.cloudModelsError != "" {
			out = append(out, form.NewReadOnly("cloud-model-discovery-error", "Model catalog", sp.cloudModelsError, "Existing model IDs are retained; access has not been confirmed"))
		}

	}
	// Advanced is collapsed by default; draft actions follow the editor.

	label := "▸ Advanced settings"
	if sp.cloudAdvanced {
		label = "▾ Advanced settings"
	}
	out = append(out, form.NewButton("cloud-advanced", label, true))
	if sp.cloudAdvanced {
		if !sp.cloudDraftNew {
			out = append(out, form.NewReadOnly("cloud-name", "Profile ID", d.Name, "Internal identifier"))
		}
		// Custom connections remain editable after the first field creates the profile.
		if r.ID == "other" || (r.IsProfile && r.Preset == nil) {
			out = append(out,
				form.NewSelect("cloud-flavor", "flavor", []form.Option{
					{Label: "chat_completions", Value: "chat_completions"},
					{Label: "messages", Value: "messages"},
				}, d.Flavor),
				form.NewSelect("cloud-backend", "backend", []form.Option{
					{Label: "default", Value: ""},
					{Label: "openai", Value: "openai"},
					{Label: "gemini", Value: "gemini"},
					{Label: "groq", Value: "groq"},
				}, d.Backend),
			)
		} else {
			out = append(out, form.NewReadOnly("cloud-flavor", "flavor", d.Flavor, ""))
			if d.Flavor == "chat_completions" {
				be := d.Backend
				if be == "" {
					be = "default"
				}
				out = append(out, form.NewReadOnly("cloud-backend", "backend", be, ""))
			}
		}
		if d.Route != "subscription" {
			out = append(out, form.NewText("cloud-base-url", "base-url", d.BaseURL, "https://…"))
		}

	}
	out = append(out, form.NewHeading("cloud-actions-heading", ""))
	if sp.cloudDirty && sp.cloudSaveFailed {
		out = append(out, form.NewButton("cloud-retry", "Retry saving", true))
	}
	if sp.cloudDeleteConfirm {
		out = append(out, form.NewReadOnly("cloud-delete-warning", "Remove account?", d.Name, "This removes the saved connection"), form.NewButton("cloud-delete-confirm", "Confirm removal", true), form.NewButton("cloud-delete-cancel", "Keep account", true))
	} else if !sp.cloudDraftNew {
		out = append(out, form.NewButton("cloud-done", "Done", !sp.pendingCommit))
		out = append(out, form.NewButton("cloud-add-account", "add account", true), form.NewButton("cloud-delete", "delete account", true))
	}
	return out
}

// Every named subscription account owns its sign-in and refresh credentials.
func (sp *settingsPage) shouldShowClaudeSignIn(r cloudRow, d cloudDraft) bool {
	return (d.Flavor == "messages" || (r.Preset != nil && r.Preset.Flavor == "messages")) && d.Route == "subscription"
}

func (sp *settingsPage) cloudAccountNameExists(name string) bool {
	for _, p := range sp.profiles {
		if p.Name == name {
			return true
		}
	}
	for _, provider := range sp.cloudView.Providers {
		for _, p := range provider.Profiles {
			if p.Name == name {
				return true
			}
		}
	}
	for _, p := range sp.cloudView.CustomProfiles {
		if p.Name == name {
			return true
		}
	}
	return false
}

func (sp *settingsPage) addCloudAccount() {
	old := sp.cloudDraft
	base := old.Name
	name := base
	for i := 2; sp.cloudAccountNameExists(name) || name == base; i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	// Copy connection structure, never credentials or model overrides.
	sp.cloudDraft = cloudDraft{Name: name, Flavor: old.Flavor, Backend: old.Backend, Route: old.Route, BaseURL: old.BaseURL, Provider: old.Provider, Region: old.Region, AWSProfile: old.AWSProfile, Choices: (&agentclient.CloudModelChoices{}).Clone()}
	sp.cloudDraftNew = true
	sp.cloudDirty = true
	sp.cloudSaveFailed = false
	sp.cloudModels = nil
	sp.cloudModelsFetched = false
	sp.cloudModelsError = ""
	sp.cloudProviderCatalog = nil
	sp.resetCloudCustomEntry()
}

// draftHasKey reports whether the row's profile already has a stored key (drives
// the masked field's "(stored)" vs "(not set)" hint).
func (sp *settingsPage) draftHasKey(r cloudRow) bool {
	if sp.cloudDraft.apiKeyEdited {
		return sp.cloudDraft.apiKey != ""
	}
	return !sp.cloudDraftNew && r.IsProfile && r.HasKey
}

// A successful login commits account identity, not unrelated model/key edits.
// Keep those edits pending while making the newly created account reselectable.
func (sp *settingsPage) cloudAccountSignedIn(name, route string) {
	// Refresh visible account labels immediately after a successful sign-in.
	defer func() {
		if sp.form != nil {
			sp.form.Reload()
			if sp.cloudDraft.Name == name && !sp.cloudDirty {
				sp.focusCloudField("cloud-done")
			}
		}
	}()
	sp.profilesLoaded = false
	if sp.cloudDraft.Name != name {
		return
	}
	sp.invalidateCatalog()
	if sp.cloudDraftNew {
		sp.cloudDirty = sp.cloudDraft.apiKeyEdited || (sp.cloudDraft.Choices != nil && (len(sp.cloudDraft.Choices.TierOverrides) > 0 || sp.cloudDraft.Choices.ImageModel != ""))
	}
	sp.cloudDraftNew = false
	sp.cloudSelected = "profile:" + name
	sp.cloudDraft.Route = route
	if !sp.cloudDirty {
		sp.cloudSaveFailed = false
	}
	if sp.form != nil {
		if sp.cloudDirty {
			sp.form.SetStatus("Signed in successfully. Some changes still need saving.")
		} else {
			sp.form.SetStatus("Signed in successfully. Choose Done to return.")
		}
	}
}
