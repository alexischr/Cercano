package ui

import (
	"context"
	"fmt"
	"strings"

	"cercano/source/clients/cli/internal/wizard"
	"cercano/source/server/pkg/agentclient"
)

func (wp *wizardPage) profileByName(name string) *agentclient.CloudProfileInfo {
	for i := range wp.curProfiles {
		if wp.curProfiles[i].Name == name {
			return &wp.curProfiles[i]
		}
	}
	return nil
}

func (wp *wizardPage) selectedCloudProfile() *agentclient.CloudProfileInfo {
	if wp.state.CloudProfile != "" {
		return wp.profileByName(wp.state.CloudProfile)
	}
	return wp.existingActiveProfile() // compatibility with older saved progress
}

func (wp *wizardPage) selectExistingAccount(p *agentclient.CloudProfileInfo) {
	if wp.state.CloudProfile != p.Name {
		// Cloud model edits belong to the previous account. Local edits do not.
		for key := range wp.state.TierPicks {
			if strings.HasSuffix(key, "."+wizard.SideCloud) {
				delete(wp.state.TierPicks, key)
			}
		}
		wp.state.CloudPicksEdited = nil
	}
	wp.state.CloudProfile = p.Name
}

// Only called from explicit acceptance. Do not silently replace a deleted
// account with another provider or let a stale resumed name create an account.
func (wp *wizardPage) confirmCloudAccount(ctx context.Context) error {
	if !wizard.ModeUsesCloud(wp.state.LocusMode) || wp.state.CloudProfile == "" {
		return nil
	}
	profiles, active, err := wp.agent.GetCloudProfiles(ctx)
	if err != nil {
		return err
	}
	for _, p := range profiles {
		if p.Name == wp.state.CloudProfile {
			if p.Provider != "" && p.Provider != wp.state.CloudProvider {
				return fmt.Errorf("selected account %q changed provider; review setup again", p.Name)
			}
			if active == p.Name {
				return nil
			}
			return wp.agent.SetActiveCloudProfile(ctx, p.Name)
		}
	}
	return fmt.Errorf("selected account %q is no longer available; choose another account", wp.state.CloudProfile)
}
