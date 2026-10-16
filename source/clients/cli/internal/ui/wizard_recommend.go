package ui

import (
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
		if in.CatalogOK && in.Catalog.SystemRAMBytes == 0 {
			rec.Notes = append(rec.Notes, "machine memory unknown: the server recommended its smallest curated profile")
		}
	}
	return rec
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
