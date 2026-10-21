package ui

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"cercano/source/server/pkg/agentclient"
	"cercano/source/server/pkg/config"
)

func (wp *wizardPage) resourceRouting() *agentclient.RoutingAssignments {
	a := wp.curRouting.Clone()
	a.Primary = wp.state.CloudProfile
	a.Secondary = wp.state.SecondaryProfile
	a.SecondaryRedirect = ""
	a.LocalRedirect = ""
	eligibleBackup := func(name string) bool {
		p := wp.profileByName(name)
		return p != nil && p.HasKey && !wp.resourceExcluded(name)
	}
	var backups []string
	for _, name := range a.PrimaryBackupAccounts() {
		if name != a.Primary && eligibleBackup(name) {
			backups = append(backups, name)
		}
	}
	a.SetPrimaryBackupAccounts(backups)
	if a.SecondaryBackup == a.Secondary || !eligibleBackup(a.SecondaryBackup) {
		a.SecondaryBackup = ""
	}
	if wp.state.Preference == "local" {
		a.Primary = ""
		a.Secondary = ""
		a.SecondaryBackup = ""
		a.SetPrimaryBackupAccounts(nil)
		a.SecondaryRedirect = "local"
	} else {
		if a.Secondary == "" {
			a.SecondaryRedirect = "primary"
		}
		if !wp.state.UseLocal {
			if a.Secondary != "" {
				a.LocalRedirect = "secondary"
			} else {
				a.LocalRedirect = "primary"
			}
		}
	}
	for _, def := range config.TaskDefinitions() {
		key := string(def.Task)
		if _, explicit := a.Tasks[key]; explicit && !wp.state.ReplaceTaskOverrides {
			continue
		}
		dest := def.Default.Destination
		switch wp.state.Preference {
		case "local":
			dest = config.DestinationLocal
		case "frontier", "cloud_open":
			dest = config.DestinationPrimary
		case "balanced":
			if dest == config.DestinationLocal && !wp.state.UseLocal {
				dest = config.DestinationSecondary
			}
			if dest == config.DestinationSecondary && wp.state.SecondaryProfile == "" {
				dest = config.DestinationPrimary
			}
		}
		a.Tasks[key] = agentclient.TaskAssignment{Destination: string(dest), Quality: string(def.Default.Quality)}
	}
	return a
}

// Revalidate without writes. Credential presence is the non-secret prerequisite;
// provider entitlement/expiry and exact local runtime readiness are not inferred.
func (wp *wizardPage) validateResourceSelection(ctx context.Context) error {
	if !wp.routingOK {
		return fmt.Errorf("routing discovery is unavailable; go back to cloud providers and continue again before applying")
	}
	view, err := wp.agent.GetCloudProviders(ctx)
	if err != nil {
		return err
	}
	live := view.Assignments.Clone()
	if live.Primary == "" {
		live.Primary = view.Active
	}
	if !reflect.DeepEqual(live, wp.curRouting) {
		return fmt.Errorf("routing changed since preview; go back to cloud providers and continue again and review the setup again")
	}
	profiles, _, err := wp.agent.GetCloudProfiles(ctx)
	if err != nil {
		return err
	}
	find := func(name string) *agentclient.CloudProfileInfo {
		for i := range profiles {
			if profiles[i].Name == name && profiles[i].HasKey && !wp.resourceExcluded(name) {
				return &profiles[i]
			}
		}
		return nil
	}
	primary, secondary := find(wp.state.CloudProfile), find(wp.state.SecondaryProfile)
	switch wp.state.Preference {
	case "frontier":
		if primary == nil || !wizardFrontierProfile(*primary) || wp.state.UseLocal || wp.state.LocusMode != "cloud_only" {
			return fmt.Errorf("frontier account unavailable; reconnect or choose another setup")
		}
	case "cloud_open":
		if primary == nil || wizardFrontierProfile(*primary) || wp.state.UseLocal || wp.state.LocusMode != "cloud_only" {
			return fmt.Errorf("hosted-model account unavailable; reconnect or choose another setup")
		}
	case "balanced":
		if primary == nil {
			return fmt.Errorf("primary account unavailable; reconnect or choose another setup")
		}
		if wp.state.SecondaryProfile != "" && (secondary == nil || secondary.Name == primary.Name || wizardFrontierProfile(*secondary)) {
			return fmt.Errorf("routine-work account unavailable; go back to cloud providers and continue again")
		}
		classes := 1
		if secondary != nil && wizardFrontierProfile(*primary) {
			classes++
		}
		if wp.state.UseLocal {
			classes++
		}
		if classes < 2 {
			return fmt.Errorf("balanced setup needs at least two usable resource types")
		}
		mode := "cloud_only"
		if wp.state.UseLocal {
			mode = "cloud_primary"
		}
		if wp.state.LocusMode != mode {
			return fmt.Errorf("setup placement changed; review your preference")
		}
	case "local":
		if !wp.state.UseLocal || wp.state.LocusMode != "open_only" || wp.state.CloudProfile != "" || wp.state.SecondaryProfile != "" {
			return fmt.Errorf("local-only setup cannot contain cloud assignments")
		}
	default:
		return fmt.Errorf("choose an available setup preference before applying")
	}
	sameConnection := func(name string) bool {
		before, after := wp.profileByName(name), find(name)
		if before == nil || after == nil {
			return false
		}
		return before.Provider == after.Provider && before.Flavor == after.Flavor && before.Backend == after.Backend && before.BaseURL == after.BaseURL && before.Route == after.Route && before.Region == after.Region && before.AWSProfile == after.AWSProfile && before.AccountEmail == after.AccountEmail
	}
	for _, name := range []string{wp.state.CloudProfile, wp.state.SecondaryProfile} {
		if name != "" && !sameConnection(name) {
			return fmt.Errorf("account %q changed since preview; go back to cloud providers and continue again", name)
		}
	}
	proposed := wp.resourceRouting()
	if err := validateWizardResourceRouting(proposed, profiles); err != nil {
		return fmt.Errorf("invalid proposed routing: %w", err)
	}
	for _, name := range append(proposed.PrimaryBackupAccounts(), proposed.SecondaryBackup) {
		if name != "" && !sameConnection(name) {
			return fmt.Errorf("backup account %q is unavailable; go back to cloud providers and continue again", name)
		}
	}
	if wp.state.UseLocal {
		cat, err := wp.agent.ListRuntimeModels(ctx)
		if err != nil {
			return fmt.Errorf("cannot verify local capability: %w", err)
		}
		probe := *wp
		probe.catalog = cat
		probe.catalogOK = true
		if ok, why := probe.localResource(); !ok {
			return fmt.Errorf("local setup is no longer eligible: %s", why)
		}
	}
	return nil
}

func (wp *wizardPage) resourceConfirmation() string {
	if !wp.routingOK {
		return "Resource discovery needs another check.\n\nReview connected accounts and go back to cloud providers and continue again before applying."
	}
	if wp.state.UseLocal {
		if ok, why := wp.localResource(); !ok {
			return "Local resources need another check.\n\n" + why + "\n\nReview resources or choose another preference."
		}
	}
	for _, name := range []string{wp.state.CloudProfile, wp.state.SecondaryProfile} {
		if name != "" {
			p := wp.profileByName(name)
			if p == nil || !p.HasKey || wp.resourceExcluded(name) {
				return "A selected cloud account is unavailable.\n\nReview connected accounts before applying."
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Ready to use %s\n\n", wp.resourcePreferenceName())
	if wp.curRouting != nil && len(wp.curRouting.Tasks) > 0 && !wp.state.ReplaceTaskOverrides {
		b.WriteString("Template defaults (existing task overrides take precedence):\n")
	}
	if wp.state.UseLocal && wp.state.Preference == "local" {
		b.WriteString("Primary work: local models\nNo cloud inference or fallback.\n")
	} else {
		fmt.Fprintf(&b, "Primary work: %s\n", wp.state.CloudProfile)
		if wp.state.SecondaryProfile != "" {
			fmt.Fprintf(&b, "Routine work: %s\n", wp.state.SecondaryProfile)
		}
		if wp.state.UseLocal {
			b.WriteString("Background helpers: local models\n")
		}
		b.WriteString("Work can be sent to your connected cloud providers.\n")
		backups := wp.resourceRouting().PrimaryBackupAccounts()
		if len(backups) > 0 {
			fmt.Fprintf(&b, "Existing cloud backups: %s\n", strings.Join(backups, ", "))
		}
		if backup := wp.resourceRouting().SecondaryBackup; backup != "" {
			fmt.Fprintf(&b, "Routine-work backup: %s\n", backup)
		}
	}
	if wp.curRouting != nil && len(wp.curRouting.Tasks) > 0 {
		if wp.state.ReplaceTaskOverrides {
			fmt.Fprintf(&b, "\nReplace %d existing task assignment(s) on confirmation.\n", len(wp.curRouting.Tasks))
		} else {
			fmt.Fprintf(&b, "\n%d explicit task assignment(s) retained; view details.\n", len(wp.curRouting.Tasks))
		}
	}
	if wp.state.UseLocal {
		downloads := requiredDownloads(wp.catalog, wp.catalogOK, wp.selectedOpenPicks())
		var size int64
		for _, d := range downloads {
			if m, ok := openModelByLabel(wp.catalog, d); ok {
				size += m.SizeBytes
			}
		}
		if len(downloads) > 0 {
			fmt.Fprintf(&b, "\nLocal models: %d download(s), approximately %s.\n", len(downloads), formatBytes(size))
		} else {
			b.WriteString("\nSelected local models are already downloaded.\n")
		}
		b.WriteString("Local inference is ready only after downloads and runtime checks succeed.\n")
	}
	b.WriteString("\nNothing is applied until you confirm.")
	if wp.showDetails {
		b.WriteString("\n\nTask assignments\n")
		a := wp.resourceRouting()
		for _, def := range config.TaskDefinitions() {
			task := a.Tasks[string(def.Task)]
			fmt.Fprintf(&b, "  %s: %s / %s\n", def.Label, task.Destination, task.Quality)
		}
		fmt.Fprintf(&b, "  Secondary redirect: %s\n  Local redirect: %s\n", firstNonEmpty(a.SecondaryRedirect, "none"), firstNonEmpty(a.LocalRedirect, "none"))
		b.WriteString("\n")
		b.WriteString(wp.summary())
	}
	return b.String()
}

func validateWizardResourceRouting(a *agentclient.RoutingAssignments, profiles []agentclient.CloudProfileInfo) error {
	c := config.Config{ActiveCloudProfile: a.Primary, SecondaryCloudProfile: a.Secondary, SecondaryBackupCloudProfile: a.SecondaryBackup, SecondaryRedirect: config.Destination(a.SecondaryRedirect), LocalRedirect: config.Destination(a.LocalRedirect), TaskAssignments: map[config.Task]config.TaskAssignment{}}
	c.SetPrimaryBackups(a.PrimaryBackupAccounts())
	for _, p := range profiles {
		c.CloudProfiles = append(c.CloudProfiles, config.CloudProfile{Name: p.Name})
	}
	for key, v := range a.Tasks {
		c.TaskAssignments[config.Task(key)] = config.TaskAssignment{Destination: config.Destination(v.Destination), Quality: config.CostTier(v.Quality)}
	}
	return c.ValidateRouting()
}
