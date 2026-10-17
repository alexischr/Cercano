package ui

import (
	"cercano/source/server/pkg/agentclient"
	"cercano/source/server/pkg/config"
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
