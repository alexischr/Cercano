package ui

import (
	"fmt"
	"strings"

	"cercano/source/clients/cli/internal/wizard"
	"cercano/source/server/pkg/agentclient"
	"cercano/source/server/pkg/config"
)

// wizardRecommendation is a complete, inspectable recommended setup built
// from the resources the user already has: their cloud account, the machine's
// curated open-model recommendations, and an explicit locus preference. It is
// pure data: building one never writes configuration, downloads a model, or
// restarts a runtime. Downloads lists what applying this recommendation would
// need; the actual downloads happen only in the existing enrollOpenDownloads
// step, after an explicit apply.
//
// Cloud use is disclosed structurally: CloudUsed reports whether any cloud
// tier is assigned, and CloudBackup reports whether cloud serves as fallback
// for a local-primary run (mixed) rather than as the primary side. A
// local-only recommendation (open_only) has both false and empty CloudPicks —
// no cloud routes or backups, by construction.
type wizardRecommendation struct {
	LocusMode string
	// CloudProvider is the account used for the cloud side; empty when
	// the recommendation uses none.
	CloudProvider string
	// CloudUsed reports whether any cloud-side tier assignment exists.
	CloudUsed bool
	// CloudBackup reports whether the cloud side serves as a fallback
	// (mixed runs). False for cloud_only, where cloud is the primary side.
	CloudBackup bool
	// CloudPicks maps each wizard capability tier to the recommended
	// cloud model id. Empty when CloudUsed is false.
	CloudPicks map[config.Tier]string
	// OpenPicks maps each wizard capability tier (plus the embedding
	// tier) to the recommended open model — a curated catalog id when the
	// catalog is available, else a shipped-recommendation id. Empty when
	// the locus mode uses no open side.
	OpenPicks map[config.Tier]string
	// Downloads lists the catalog model ids that would need downloading
	// before this recommendation can serve open work, deduplicated so a
	// model assigned to several tiers downloads once. Empty when every
	// recommended artifact is already installed.
	Downloads []string
	// Notes records honest limitations: missing accounts, unknown
	// hardware, unavailable recommendations. Always show these with the
	// recommendation; a missing side without a note is a bug.
	Notes []string
}

// wizardRecommendInputs carries everything recommendSetup needs. Each field
// comes from an existing surface: locus mode from the wizard state, provider
// from the user's selection or configured account, recs from
// config.LoadTierRecommendations, and the catalog from the agent's
// ListRuntimeModels (whose RecommendedOpenModels and SystemRAMBytes the
// server already computed for this machine).
type wizardRecommendInputs struct {
	// LocusMode is the user's preference: cloud_only, cloud_primary,
	// open_primary, or open_only.
	LocusMode string
	// CloudProvider is the available/selected cloud account. One is
	// enough — no secondary provider is required for any preset.
	CloudProvider string
	// Recs is the shipped tier-recommendations table; RecsOK false means
	// it could not be loaded.
	Recs   config.TierRecommendations
	RecsOK bool
	// Catalog is the agent's runtime model catalog; CatalogOK false means
	// it could not be fetched.
	Catalog   agentclient.RuntimeModelCatalog
	CatalogOK bool
}

// recommendSetup builds the recommended assignments for the given preference
// and resources. It is total: it always returns a recommendation, filling
// what it can and recording what it could not in Notes. Cloud-side picks all
// come from the single chosen account; multiple accounts are never required.
// Open-side picks prefer the server's RAM-tiered curated catalog
// (gate-compatible for this machine) and fall back to the shipped
// open-weight recommendations only when the catalog is unavailable — the
// client never recomputes memory fit, it defers to the server's band.
func recommendSetup(in wizardRecommendInputs) wizardRecommendation {
	rec := wizardRecommendation{
		LocusMode: in.LocusMode,
		CloudUsed: wizard.ModeUsesCloud(in.LocusMode),
		// Mixed runs (cloud_primary/open_primary) use cloud as fallback;
		// open_only uses none; cloud_only uses cloud as the primary side.
		CloudBackup: wizard.ModeUsesCloud(in.LocusMode) && wizard.ModeUsesOpen(in.LocusMode),
	}
	if !rec.CloudUsed {
		// A local-only recommendation uses no account; carrying one here
		// would misrepresent cloud use. (The wizard state may still hold
		// the user's provider choice — it simply isn't part of this
		// recommendation.)
		rec.CloudProvider = ""
	} else {
		rec.CloudProvider = in.CloudProvider
	}
	if rec.CloudUsed {
		rec.CloudPicks = map[config.Tier]string{}
		switch {
		case in.CloudProvider == "":
			rec.Notes = append(rec.Notes, "no cloud account selected: connect one to fill the cloud tiers")
		case !in.RecsOK:
			rec.Notes = append(rec.Notes, "cloud recommendations unavailable: picks left empty for manual choice")
		default:
			for _, t := range wizardTierOrder {
				cands := in.Recs.Candidates(config.ProviderCloud, in.CloudProvider, t)
				if m, ok := config.PickFirst(cands, nil); ok {
					rec.CloudPicks[t] = m
				}
			}
			if len(rec.CloudPicks) == 0 {
				rec.Notes = append(rec.Notes, "no recommendations for provider "+in.CloudProvider+": picks left empty for manual choice")
			}
		}
	}
	if wizard.ModeUsesOpen(in.LocusMode) {
		rec.OpenPicks = map[config.Tier]string{}
		if in.CatalogOK {
			for _, t := range openTierOrder() {
				if id := in.Catalog.RecommendedOpenModels[string(t)]; id != "" {
					rec.OpenPicks[t] = id
				}
			}
		}
		// Fallback: shipped open-weight recommendations for whatever the
		// curated catalog could not fill. Not machine-fit-verified; the
		// note says so.
		fallback := false
		if in.RecsOK {
			for _, t := range openTierOrder() {
				if rec.OpenPicks[t] != "" {
					continue
				}
				if m, ok := config.PickFirst(in.Recs.Candidates(config.ProviderOpen, "", t), nil); ok {
					rec.OpenPicks[t] = m
					fallback = true
				}
			}
		}
		if fallback && !in.CatalogOK {
			rec.Notes = append(rec.Notes, "runtime catalog unavailable: open picks use shipped recommendations, not machine-fit-verified models")
		}
		if len(rec.OpenPicks) == 0 {
			rec.Notes = append(rec.Notes, "no open-model recommendation available for this machine: picks left empty for manual choice")
		}
		rec.Downloads = requiredDownloads(in.Catalog, in.CatalogOK, rec.OpenPicks)
		if in.CatalogOK {
			rec.Notes = append(rec.Notes, hardwareNotes(in.Catalog, rec.OpenPicks)...)
		}
	}
	return rec
}

// hardwareNotes records the honest limits of the open picks given what was
// actually probed about this machine, and names the client's screen for what
// it is: a preliminary weight-budget screen — weights plus a heuristic
// overhead allowance against one memory pool — never a runtime fit
// verification. Context/KV-cache headroom, allocation, and serving capacity
// are validated at runtime, not here. Unknown hardware (including an older
// server without topology fields — HardwareOK false, topology unknown) is
// disclosed as unknown rather than covered by a RAM-band claim; discrete
// machines get a weight-budget screen of the actual picks against their real
// GPU memory (system RAM is never treated as GPU memory, and per-device
// pools are never summed into one budget); unified machines get the same
// limitation disclosure — the screen's context deferral is not a fit verdict
// there either. Picks are read-only input; they are never mutated here.
func hardwareNotes(cat agentclient.RuntimeModelCatalog, picks map[config.Tier]string) []string {
	switch {
	case cat.SystemRAMBytes <= 0:
		return []string{"machine memory unknown: the server recommended its smallest curated profile"}
	case !cat.HardwareOK || cat.Hardware.Insufficient():
		return []string{"memory topology unknown: hardware fit not verified for this machine"}
	case cat.Hardware.Kind == agentclient.HardwareTopologyDiscrete:
		return discreteFitNotes(cat, picks)
	case cat.Hardware.Kind == agentclient.HardwareTopologyUnified:
		return []string{"unified memory: preliminary weight-budget screen only — context/KV-cache headroom is not checked and runtime fit is not verified; runtime checks still apply"}
	}
	return nil
}

// discreteFitNotes screens the actual open picks against the machine's
// discrete GPUs. This is a preliminary weight-budget screen, not a runtime
// fit verification: it compares model weights plus a heuristic overhead
// allowance against the usable fraction of the single largest GPU pool —
// never system RAM, never a sum across devices — and context/KV-cache
// headroom is not included (runtime allocation and serving-capacity checks
// still apply). A single GPU's pool is the only honest budget for a pick,
// so a pick larger than the usable fraction of the largest pool is
// disclosed by name rather than silently kept; a device whose pool size
// is unknown (0) is disclosed as unknown; and a pick whose model size is
// unknown (no catalog record or a zero size) is disclosed as unscreened
// rather than silently skipped. Picks are read-only input.
func discreteFitNotes(cat agentclient.RuntimeModelCatalog, picks map[config.Tier]string) []string {
	var budget int64
	for _, g := range cat.Hardware.GPUs {
		if g.MemoryBytes > budget {
			budget = g.MemoryBytes
		}
	}
	if budget <= 0 {
		return []string{"discrete GPUs detected but per-GPU memory unknown: hardware fit not verified"}
	}
	usable := int64(float64(budget) * estimateUsableFraction)
	var unfit, unsized []string
	for _, t := range openTierOrder() {
		pick := picks[t]
		if pick == "" {
			continue
		}
		// Picks arrive as catalog ids (recommendSetup) or slot labels
		// (the wizard preview's display-name convention); either form
		// resolves against the catalog.
		m, ok := openModelByLabel(cat, pick)
		if !ok || m.SizeBytes <= 0 {
			// No invented verdict either way — the gap is disclosed.
			unsized = append(unsized, fmt.Sprintf("%s (%s)", t, pick))
			continue
		}
		if m.SizeBytes+estimateOverheadBytes(m.SizeBytes) > usable {
			unfit = append(unfit, fmt.Sprintf("%s (%s)", t, openModelDisplay(cat, pick)))
		}
	}
	notes := []string{fmt.Sprintf("discrete GPUs: preliminary weight-budget screen against the largest GPU's %s, not system RAM (weights + estimated overhead only; context/KV-cache not included, runtime fit not verified)", formatBytes(budget))}
	if len(unsized) > 0 {
		notes = append(notes, "model size unknown (weight budget not screened): "+strings.Join(unsized, ", "))
	}
	if len(unfit) > 0 {
		notes = append(notes, "larger than any single GPU: "+strings.Join(unfit, ", ")+" — pick smaller models for these tiers")
	}
	return notes
}

// catalogModelByID finds a catalog model by inventory id (the form
// RecommendedOpenModels is keyed by). False when the id does not
// resolve, so callers can treat it as unknown instead of guessing.
func catalogModelByID(cat agentclient.RuntimeModelCatalog, id string) (agentclient.RuntimeModel, bool) {
	for _, m := range cat.Models {
		if m.ID == id {
			return m, true
		}
	}
	return agentclient.RuntimeModel{}, false
}

// openTierOrder is the deterministic open-slot order used to build picks and
// downloads: the four capability tiers, then the embedding slot.
func openTierOrder() []config.Tier {
	return append(append([]config.Tier{}, wizardTierOrder...), config.TierEmbedding)
}

// requiredDownloads lists, in open-tier order, every distinct catalog
// artifact an open recommendation needs that is not already on disk. The same
// model serving several tiers downloads once (dedup), and already-downloaded
// artifacts are reused rather than re-downloaded. Picks that don't resolve to
// a catalog entry (shipped-recs fallback ids) produce no download — the
// fallback note covers them.
func requiredDownloads(cat agentclient.RuntimeModelCatalog, catOK bool, picks map[config.Tier]string) []string {
	if !catOK {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, t := range openTierOrder() {
		id := picks[t]
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		m, ok := openModelByLabel(cat, openModelDisplay(cat, id))
		if !ok || m.DownloadState == "downloaded" {
			continue
		}
		out = append(out, m.ID)
	}
	return out
}

// tierPicks converts the recommendation into the wizard state's TierPicks
// representation (the persisted and applied slot keys), so applying a
// recommendation flows through the same sparse-patch path as the existing
// wizard finish. Open picks are stored by the slot convention: display name
// for curated catalog picks (resolvable by openModelByLabel on finish), id
// as-is for shipped-recs fallback picks. Because it serializes only what
// recommendSetup filled, a local-only recommendation carries no cloud slots
// and a cloud-only recommendation carries no open slots.
func (r wizardRecommendation) tierPicks(cat agentclient.RuntimeModelCatalog, catOK bool) map[string]string {
	out := map[string]string{}
	for t, m := range r.CloudPicks {
		out[string(t)+"."+wizard.SideCloud] = m
	}
	for t, m := range r.OpenPicks {
		out[wizardOpenTierKey(t)] = openPickSlot(cat, catOK, m)
	}
	return out
}

// openPickSlot renders an open pick for a state slot: display name for
// catalog-resolvable picks (the open-slot convention), the id itself
// otherwise.
func openPickSlot(cat agentclient.RuntimeModelCatalog, catOK bool, id string) string {
	if !catOK {
		return id
	}
	return openModelDisplay(cat, id)
}
