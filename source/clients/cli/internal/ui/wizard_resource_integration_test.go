package ui

import (
	"cercano/source/clients/cli/internal/form"
	tea "charm.land/bubbletea/v2"
	"strings"
	"testing"

	"cercano/source/clients/cli/internal/theme"
	"cercano/source/clients/cli/internal/wizard"
	"cercano/source/server/pkg/agentclient"
	"cercano/source/server/pkg/proto"
)

func resourceWizard(t *testing.T, client *agentclient.Client) *wizardPage {
	t.Helper()
	p := theme.BuiltinThemes()[0]
	return newWizardPage(client, p.Palette, theme.NewStyles(p.Palette), 100, 40)
}
func resourcePreferencesPage(t *testing.T, wp *wizardPage) {
	t.Helper()
	if wp.state.Step != wizard.StepResources {
		t.Fatalf("expected resources first: %+v", wp.state)
	}
	pressRows(t, wp, "resource_next")
	if wp.state.ResourcePage != 2 {
		t.Fatal("missing resource summary")
	}
	pressRows(t, wp, "preferences")
	if wp.state.Step != wizard.StepPreference {
		t.Fatal("missing preference screen")
	}
}
func resourceAccount(name, provider string, connected bool) *proto.CloudProfileInfo {
	return &proto.CloudProfileInfo{Name: name, Provider: provider, HasKey: connected}
}

func TestResourcePreferenceEligibilityMatrix(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		frontier, hosted, local bool
		want                    string
	}{
		{"none", false, false, false, ""},
		{"frontier", true, false, false, "frontier"},
		{"hosted", false, true, false, "cloud_open"},
		{"local", false, false, true, "local"},
		{"two clouds", true, true, false, "frontier,balanced,cloud_open"},
		{"frontier local", true, false, true, "frontier,balanced,local"},
		{"hosted local", false, true, true, "balanced,cloud_open,local"},
		{"all", true, true, true, "frontier,balanced,cloud_open,local"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub, client := newScenario(t)
			stub.mu.Lock()
			stub.profiles = nil
			stub.active = ""
			if tc.frontier {
				stub.profiles = append(stub.profiles, resourceAccount("claude", "anthropic-subscription", true))
				stub.active = "claude"
			}
			if tc.hosted {
				stub.profiles = append(stub.profiles, resourceAccount("deepinfra", "deepinfra", true))
				if stub.active == "" {
					stub.active = "deepinfra"
				}
			}
			if !tc.local {
				stub.catalog = &proto.ListRuntimeModelsResponse{}
			}
			stub.mu.Unlock()
			wp := resourceWizard(t, client)
			got := []string{}
			for _, p := range wp.resourcePreferences() {
				got = append(got, p.key)
				wp.chooseResourcePreference(p)
				if err := validateWizardResourceRouting(wp.resourceRouting(), wp.curProfiles); err != nil {
					t.Fatalf("invalid %s proposal: %v", p.key, err)
				}
			}
			if strings.Join(got, ",") != tc.want {
				_, why := wp.localResource()
				t.Fatalf("got %v want %s; local %s", got, tc.want, why)
			}
			if stub.writeCount() != 0 {
				t.Fatal("discovery wrote configuration")
			}
			if tc.want == "" {
				pressRows(t, wp, "resource_next")
				if !wp.rows()[0].Disabled {
					t.Fatal("no-resource setup can continue")
				}
			}
		})
	}
}

func TestResourceBalancedAppliesNamedAccountsOnlyAfterConfirmation(t *testing.T) {
	stub, client := newScenario(t)
	stub.mu.Lock()
	stub.profiles = append(stub.profiles, resourceAccount("deepinfra", "deepinfra", true))
	stub.routing = &proto.RoutingAssignments{Primary: "personal", Tasks: map[string]*proto.TaskModelAssignment{"review": {Destination: "primary", Quality: "premium"}}}
	stub.mu.Unlock()
	wp := resourceWizard(t, client)
	resourcePreferencesPage(t, wp)
	pressRows(t, wp, "balanced")
	if stub.writeCount() != 0 {
		t.Fatal("selection wrote before confirmation")
	}
	preview := wp.stepDesc()
	for _, text := range []string{"Primary work: personal", "Routine work: deepinfra", "Background helpers: local", "explicit task assignment", "downloads and runtime checks"} {
		if !strings.Contains(preview, text) {
			t.Fatalf("preview missing %q: %s", text, preview)
		}
	}
	if !pressRows(t, wp, "finish") {
		t.Fatalf("apply failed: %s", wp.status)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.routingUpdates) != 1 {
		t.Fatalf("want one routing update, got %d", len(stub.routingUpdates))
	}
	a := stub.routing
	if a.Primary != "personal" || a.Secondary != "deepinfra" || a.Tasks["chat"].Destination != "primary" || a.Tasks["dispatch"].Destination != "secondary" || a.Tasks["reconnaissance"].Destination != "local" {
		t.Fatalf("wrong mixed routing: %+v", a)
	}
	if a.Tasks["review"].Quality != "premium" || a.Tasks["review"].Destination != "primary" {
		t.Fatal("explicit override lost")
	}
	if len(stub.upserts)+len(stub.keyNames)+len(stub.removals)+stub.resets != 0 {
		t.Fatal("apply altered accounts or reset setup")
	}
}

func TestResourceLocalOnlyRemovesCloudRoutesNotAccounts(t *testing.T) {
	stub, client := newScenario(t)
	stub.mu.Lock()
	stub.routing = &proto.RoutingAssignments{Primary: "personal", PrimaryBackups: []string{"team-external"}, Secondary: "team-external", SecondaryBackup: "personal"}
	stub.mu.Unlock()
	wp := resourceWizard(t, client)
	resourcePreferencesPage(t, wp)
	pressRows(t, wp, "local")
	if !strings.Contains(wp.stepDesc(), "No cloud inference or fallback") {
		t.Fatal("missing privacy disclosure")
	}
	if !pressRows(t, wp, "finish") {
		t.Fatal(wp.status)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	a := stub.routing
	if stub.locusMode != "open_only" || a.Primary != "" || a.Secondary != "" || len(a.PrimaryBackups) != 0 || a.SecondaryBackup != "" {
		t.Fatalf("local setup retained cloud routes: %+v", a)
	}
	for _, v := range a.Tasks {
		if v.Destination != "local" {
			t.Fatal("new local task not local")
		}
	}
	if len(stub.profiles) != 2 || len(stub.removals)+len(stub.keyNames) != 0 {
		t.Fatal("local setup removed accounts")
	}
}

func TestResourceRevalidationRejectsRemovedCredentialsBeforeWrites(t *testing.T) {
	stub, client := newScenario(t)
	wp := resourceWizard(t, client)
	resourcePreferencesPage(t, wp)
	pressRows(t, wp, "frontier")
	stub.mu.Lock()
	stub.profiles[0].HasKey = false
	stub.mu.Unlock()
	if pressRows(t, wp, "finish") || stub.writeCount() != 0 {
		t.Fatal("missing credential caused writes")
	}
	if !strings.Contains(wp.status, "unavailable") {
		t.Fatal(wp.status)
	}
}

func TestResourceRevalidationRejectsChangedRoutingBeforeWrites(t *testing.T) {
	stub, client := newScenario(t)
	wp := resourceWizard(t, client)
	resourcePreferencesPage(t, wp)
	pressRows(t, wp, "frontier")
	stub.mu.Lock()
	stub.routing = &proto.RoutingAssignments{Primary: "personal", Tasks: map[string]*proto.TaskModelAssignment{"review": {Destination: "secondary", Quality: "economy"}}}
	stub.mu.Unlock()
	if pressRows(t, wp, "finish") || stub.writeCount() != 0 {
		t.Fatal("concurrent routing change overwritten")
	}
	if !strings.Contains(wp.status, "changed since preview") {
		t.Fatal(wp.status)
	}
}

func TestResourceResumeAndCancelPreserveAccounts(t *testing.T) {
	stub, client := newScenario(t)
	wp := resourceWizard(t, client)
	pressRows(t, wp, "connected:personal")
	if !wp.resourceExcluded("personal") {
		t.Fatal("account toggle not saved")
	}
	wp = resourceWizard(t, client)
	if !wp.resourceExcluded("personal") {
		t.Fatal("account selection lost on resume")
	}
	resourcePreferencesPage(t, wp)
	pressRows(t, wp, "frontier")
	wp = resourceWizard(t, client)
	if wp.state.Step != wizard.StepDone || wp.state.CloudProfile != "team-external" {
		t.Fatal("preview did not resume selected account")
	}
	wp.abandon()
	if !wp.abandon() {
		t.Fatal("cancel did not close")
	}
	if stub.writeCount() != 0 {
		t.Fatal("cancel wrote server configuration")
	}
	if _, ok := wizard.Load(); ok {
		t.Fatal("cancel left resume file")
	}
}

func TestResourceOldProgressMigratesWithoutLosingPicks(t *testing.T) {
	stub, client := newScenario(t)
	old := wizard.State{Step: wizard.StepDone, LocusMode: "cloud_primary", TierPicks: map[string]string{"llama_server.everyday": "explicit"}, CloudProfile: "personal"}
	if err := wizard.Save(old); err != nil {
		t.Fatal(err)
	}
	wp := resourceWizard(t, client)
	if wp.state.Step != wizard.StepResources || wp.state.FlowVersion != 2 || wp.state.TierPicks["llama_server.everyday"] != "explicit" || wp.state.CloudProfile != "personal" {
		t.Fatalf("unsafe migration: %+v", wp.state)
	}
	if stub.writeCount() != 0 {
		t.Fatal("migration wrote config")
	}
}

func TestResourceDisconnectedAccountIsNotUsable(t *testing.T) {
	stub, client := newScenario(t)
	stub.mu.Lock()
	for _, p := range stub.profiles {
		p.HasKey = false
	}
	stub.catalog = &proto.ListRuntimeModelsResponse{}
	stub.mu.Unlock()
	wp := resourceWizard(t, client)
	if len(wp.resourcePreferences()) != 0 {
		t.Fatal("unconnected accounts enabled a setup")
	}
	found := false
	for _, row := range wp.rows() {
		if row.Key == "reconnect:personal" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing account recovery action")
	}
}

func TestResourceExplicitTaskReplacementRequiresChoice(t *testing.T) {
	stub, client := newScenario(t)
	stub.mu.Lock()
	stub.routing = &proto.RoutingAssignments{Primary: "personal", Tasks: map[string]*proto.TaskModelAssignment{"review": {Destination: "local", Quality: "economy"}}}
	stub.mu.Unlock()
	wp := resourceWizard(t, client)
	resourcePreferencesPage(t, wp)
	pressRows(t, wp, "frontier")
	if wp.resourceRouting().Tasks["review"].Destination != "local" {
		t.Fatal("override replaced without consent")
	}
	pressRows(t, wp, "replace_tasks")
	if stub.writeCount() != 0 || !strings.Contains(wp.stepDesc(), "Replace 1 existing task") {
		t.Fatal("replacement was not a pending disclosed change")
	}
	if !pressRows(t, wp, "finish") {
		t.Fatal(wp.status)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.routing.Tasks["review"].Destination != "primary" {
		t.Fatal("explicit template replacement not applied")
	}
}

func TestResourceHardwareChangeRejectsLocalBeforeWrites(t *testing.T) {
	stub, client := newScenario(t)
	wp := resourceWizard(t, client)
	resourcePreferencesPage(t, wp)
	pressRows(t, wp, "local")
	stub.mu.Lock()
	stub.catalog = &proto.ListRuntimeModelsResponse{}
	stub.mu.Unlock()
	if pressRows(t, wp, "finish") || stub.writeCount() != 0 {
		t.Fatal("unknown changed hardware applied local setup")
	}
}

func TestResourceReconnectUsesExistingOAuthEditor(t *testing.T) {
	stub, client := newScenario(t)
	stub.mu.Lock()
	stub.profiles[0].HasKey = false
	stub.profiles[0].Flavor = "messages"
	stub.profiles[0].Route = "subscription"
	stub.profiles[0].Provider = "anthropic-subscription"
	stub.mu.Unlock()
	wp := resourceWizard(t, client)
	for i, r := range wp.rows() {
		if r.Key == "reconnect:personal" {
			wp.cursor = i
			break
		}
	}
	cmd, _ := wp.selectRow()
	if cmd == nil {
		t.Fatal("no reconnect action")
	}
	msg, ok := cmd().(wizardAddProviderMsg)
	if !ok || msg.profile != "personal" {
		t.Fatal("reconnect did not target existing account")
	}
	m := Model{agent: client, content: wp, width: 100, height: 40, palette: wp.palette, styles: wp.styles}
	next, _ := m.openWizardProvider(msg)
	sp := next.(Model).content.(*settingsPage)
	if !sp.cloudEditorActive() {
		t.Fatal("existing account editor not open")
	}
	_, auth, err := sp.onCommit("cloud-signin-claude", form.ButtonActivate)
	if err != nil || auth == nil {
		t.Fatalf("missing OAuth recovery: %v", err)
	}
	login, ok := auth().(openClaudeLoginModalMsg)
	if !ok || login.createOnly {
		t.Fatal("reconnect incorrectly creates a new account")
	}
	if stub.writeCount() != 0 {
		t.Fatal("opening reconnect wrote credentials or routing")
	}
}

func TestResourceLocalModelReviewReturnsToDiscovery(t *testing.T) {
	_, client := newScenario(t)
	wp := resourceWizard(t, client)
	pressRows(t, wp, "resource_next")
	pressRows(t, wp, "local_models")
	if wp.state.Step != wizard.StepOpen {
		t.Fatal("model editor not opened")
	}
	pressRows(t, wp, "continue")
	if wp.state.Step != wizard.StepResources || wp.state.ResourcePage != 2 {
		t.Fatal("model editing bypassed resource/preferences steps")
	}
}

func TestResourceChangedAccountEndpointRejectsBeforeWrites(t *testing.T) {
	stub, client := newScenario(t)
	wp := resourceWizard(t, client)
	resourcePreferencesPage(t, wp)
	pressRows(t, wp, "frontier")
	stub.mu.Lock()
	stub.profiles[0].BaseUrl = "https://changed.example.invalid"
	stub.mu.Unlock()
	if pressRows(t, wp, "finish") || stub.writeCount() != 0 {
		t.Fatal("account endpoint changed without renewed preview")
	}
	if !strings.Contains(wp.status, "changed since preview") {
		t.Fatal(wp.status)
	}
}

func TestResourceBackupsExcludeDeselectedAndPrimaryAccounts(t *testing.T) {
	stub, client := newScenario(t)
	stub.mu.Lock()
	stub.routing = &proto.RoutingAssignments{Primary: "personal", PrimaryBackups: []string{"team-external"}}
	stub.mu.Unlock()
	wp := resourceWizard(t, client)
	pressRows(t, wp, "connected:personal")
	resourcePreferencesPage(t, wp)
	pressRows(t, wp, "frontier")
	a := wp.resourceRouting()
	if a.Primary != "team-external" || len(a.PrimaryBackupAccounts()) != 0 {
		t.Fatalf("self/deselected backup retained: %+v", a)
	}
	if err := validateWizardResourceRouting(a, wp.curProfiles); err != nil {
		t.Fatal(err)
	}
}

func TestResourceLegacyOtherProviderPageResumesUnifiedProviders(t *testing.T) {
	stub, client := newScenario(t)
	old := wizard.State{FlowVersion: 2, Step: wizard.StepResources, ResourcePage: 1, CloudProfile: "personal", ExcludedProfiles: []string{"personal"}}
	if err := wizard.Save(old); err != nil {
		t.Fatal(err)
	}
	wp := resourceWizard(t, client)
	if wp.state.ResourcePage != 0 || wp.state.CloudProfile != "personal" || !wp.resourceExcluded("personal") {
		t.Fatalf("bad provider-page resume: %+v", wp.state)
	}
	pressRows(t, wp, "resource_next")
	wp.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if wp.state.Step != wizard.StepResources || wp.state.ResourcePage != 0 {
		t.Fatal("summary Back should return directly to all providers")
	}
	if stub.writeCount() != 0 {
		t.Fatal("resume or navigation wrote configuration")
	}
}
