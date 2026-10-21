package ui

import (
	tea "charm.land/bubbletea/v2"
	"strings"
	"testing"

	"cercano/source/clients/cli/internal/form"
	"cercano/source/server/pkg/agentclient"
)

func TestNewCloudProviderDoesNotClaimFailedEdit(t *testing.T) {
	sp := cloudSamplePage()
	selectNewChatGPTProvider(t, sp)
	for _, field := range sp.cloudDetailFields(sp.catalogRow()) {
		if field.Key() == "cloud-retry" {
			t.Fatalf("new untouched provider offers %q without any failed save", field.Label())
		}
	}
}

func TestCloudSignInReplacesStartingStatusAndOffersDone(t *testing.T) {
	sp := cloudSamplePage()
	sp.scope = scopeCloud
	selectNewChatGPTProvider(t, sp)
	sp.form = form.New(sp.buildCloudSections())
	sp.form.OnReload = sp.snapshotSections
	sp.form.SetStatus("starting ChatGPT sign-in…")
	sp.cloudAccountSignedIn(sp.cloudDraft.Name, "chatgpt")
	view := sp.form.View(90, sp.palette, sp.styles)
	if strings.Contains(view, "starting ChatGPT") {
		t.Fatal("successful sign-in retains starting status")
	}
	if !strings.Contains(strings.ToLower(view), "signed in") {
		t.Fatalf("missing success feedback: %s", view)
	}
	found := false
	for _, field := range sp.cloudDetailFields(sp.catalogRow()) {
		if field.Key() == "cloud-done" {
			found = true
		}
	}
	if !found {
		t.Fatal("connected account has no Done action")
	}
}

func TestCloudChatGPTCanonicalRouteDoesNotClaimUnsavedEdit(t *testing.T) {
	sp := cloudSamplePage()
	selectNewChatGPTProvider(t, sp)
	d := sp.cloudDraft
	profile := agentclient.CloudProfileInfo{Name: d.Name, Provider: d.Provider, Flavor: d.Flavor, Backend: d.Backend, BaseURL: d.BaseURL, HasKey: true}
	sp.profiles = append(sp.profiles, profile)
	for i := range sp.cloudView.Providers {
		if sp.cloudView.Providers[i].ID == "openai-responses" {
			sp.cloudView.Providers[i].Profiles = []agentclient.CloudProfileInfo{profile}
		}
	}
	sp.cloudAccountSignedIn(d.Name, "chatgpt")
	if msg, field := sp.catalogPrerequisite(); msg != "" {
		t.Fatalf("successful ChatGPT connection falsely needs %s: %s", field, msg)
	}
}

func selectNewChatGPTProvider(t *testing.T, sp *settingsPage) {
	t.Helper()
	sp.scope = scopeCloud
	if sp.form == nil {
		sp.form = form.New(nil)
		sp.form.OnReload = sp.snapshotSections
	}
	sp.openProviderPicker()
	sp.handlePaste("OpenAI")
	sp.updateProviderPicker(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !sp.cloudDraftNew || !sp.cloudDirty || sp.cloudDraft.Flavor != "responses" {
		t.Fatalf("invalid new ChatGPT fixture: %+v", sp.cloudDraft)
	}
}

func TestCloudRetryOnlyAfterFailedSave(t *testing.T) {
	sp := cloudSamplePage()
	sp.selectCloudRow("profile:work-openai")
	_, _, err := sp.onCommit("cloud-base-url", "https://pending.example.invalid")
	if err == nil {
		t.Fatal("nil-agent save unexpectedly succeeded")
	}
	found := false
	for _, field := range sp.cloudDetailFields(sp.catalogRow()) {
		if field.Key() == "cloud-retry" {
			found = true
			if field.Label() != "Retry saving" {
				t.Fatalf("misleading retry label: %s", field.Label())
			}
		}
	}
	if !found {
		t.Fatal("real save failure has no recovery action")
	}
	sp.commitCloud(classifyCloudCommit("cloud-discard", ""))
	for _, field := range sp.cloudDetailFields(sp.catalogRow()) {
		if field.Key() == "cloud-retry" {
			t.Fatal("discard retained failed-save action")
		}
	}
}

func TestCloudDoneProtectsPendingEdits(t *testing.T) {
	sp := layoutPage()
	sp.selectCloudRow("profile:work-openai")
	sp.form.Reload()
	sp.cloudDraft.apiKey = "pending-secret"
	sp.cloudDraft.apiKeyEdited = true
	sp.cloudDirty = true
	sp.onCommit("cloud-done", form.ButtonActivate)
	if !sp.cloudEditorActive() || !sp.settingsNavigationPrompt || sp.cloudDraft.apiKey != "pending-secret" {
		t.Fatal("Done silently discarded edits")
	}
	sp.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if !sp.cloudEditorActive() || sp.cloudDraft.apiKey != "pending-secret" {
		t.Fatal("keep editing lost pending credentials")
	}
}

func TestCloudProviderStepUsesCloudWording(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.state.Step = "resources"
	wp.state.ResourcePage = 0
	if !strings.Contains(wp.resourceDescription(), "Add your cloud providers") {
		t.Fatal("resource heading omits cloud")
	}
	for _, r := range wp.rows() {
		if r.Key == "add_provider" {
			if !strings.Contains(r.Label, "cloud provider") {
				t.Fatal("provider button omits cloud")
			}
			return
		}
	}
	t.Fatal("missing cloud provider action")
}

func TestCloudEditorSectionArrowsPreserveFocus(t *testing.T) {
	sp := layoutPage()
	sp.selectCloudRow("profile:work-openai")
	sp.form.Reload()
	for _, key := range []string{"cloud-models", "cloud-advanced"} {
		sp.focusCloudField(key)
		sp.Update(tea.KeyPressMsg{Code: tea.KeyRight})
		expanded := sp.cloudModelsExpanded
		if key == "cloud-advanced" {
			expanded = sp.cloudAdvanced
		}
		if !expanded || cleanupFocusedKey(sp) != key {
			t.Fatalf("right arrow did not expand focused %s", key)
		}
		sp.Update(tea.KeyPressMsg{Code: tea.KeyRight})
		expanded = sp.cloudModelsExpanded
		if key == "cloud-advanced" {
			expanded = sp.cloudAdvanced
		}
		if !expanded {
			t.Fatal("right arrow collapsed an open section")
		}
		sp.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
		expanded = sp.cloudModelsExpanded
		if key == "cloud-advanced" {
			expanded = sp.cloudAdvanced
		}
		if expanded {
			t.Fatal("left arrow did not collapse")
		}
	}
}

func TestCloudSignInDoneReturnsToSetup(t *testing.T) {
	for _, kind := range []string{"chatgpt", "claude"} {
		t.Run(kind, func(t *testing.T) {
			stub, client := newScenario(t)
			stub.mu.Lock()
			if kind == "chatgpt" {
				stub.profiles[0].Flavor = "responses"
				stub.profiles[0].Provider = "openai-responses"
				stub.profiles[0].Route = ""
			} else {
				stub.profiles[0].Flavor = "messages"
				stub.profiles[0].Route = "subscription"
			}
			stub.mu.Unlock()
			wp := resourceWizard(t, client)
			m := Model{agent: client, content: wp, width: 100, height: 40, palette: wp.palette, styles: wp.styles}
			next, _ := m.openWizardProvider(wizardAddProviderMsg{page: wp, profile: "personal"})
			m = next.(Model)
			sp := m.content.(*settingsPage)
			sp.form.SetStatus("starting " + kind + " sign-in…")
			var msg tea.Msg
			if kind == "chatgpt" {
				m.chatgptLoginAttempt = 9
				m.chatgptLoginModal = newChatGPTLoginModal("personal", "")
				msg = chatgptLoginFrameMsg{attempt: 9, frame: agentclient.ChatGPTLoginMsg{ProfileName: "personal", Done: true, Ok: true}}
			} else {
				m.claudeLoginAttempt = 9
				m.claudeLoginModal = newClaudeLoginModal("personal", "")
				msg = claudeLoginFrameMsg{attempt: 9, frame: agentclient.ClaudeLoginMsg{ProfileName: "personal", Done: true, Ok: true}}
			}
			next, _ = m.Update(msg)
			m = next.(Model)
			if strings.Contains(sp.form.View(90, sp.palette, sp.styles), "starting ") {
				t.Fatal("successful terminal frame left starting status")
			}
			if cleanupFocusedKey(sp) != "cloud-done" {
				t.Fatal("successful sign-in did not focus Done")
			}
			// Dismiss the successful OAuth modal, then activate the focused Done action.
			next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = next.(Model)
			next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = next.(Model)
			if m.content != wp || m.wizardProviderReturn != nil || wp.state.Step != "resources" {
				t.Fatal("Done did not return to setup resources")
			}
			addAvailable := false
			for _, row := range wp.rows() {
				if row.Key == "add_provider" {
					addAvailable = true
				}
			}
			if !addAvailable {
				t.Fatal("sign-in removed the option to add another cloud account")
			}
			if stub.writeCount() != 0 {
				t.Fatal("returning from sign-in reapplied account or routing changes")
			}
		})
	}
}

func TestCloudDoneMouseClosesProviderEditor(t *testing.T) {
	sp := layoutPage()
	sp.selectCloudRow("profile:work-openai")
	sp.form.Reload()
	layoutClick(t, sp, "Done")
	if sp.cloudEditorActive() || sp.settingsNavigationPrompt {
		t.Fatal("Done click did not close the saved provider editor")
	}
}
