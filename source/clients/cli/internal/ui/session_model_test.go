package ui

import (
	"strings"
	"testing"

	"cercano/source/server/pkg/agentclient"
)

func TestSessionModelDisplay(t *testing.T) {
	got := sessionModelText(agentclient.SessionModelStatus{Override: &agentclient.SessionModelRoute{Profile: "deepinfra", Model: "exact-model"}, Note: "Next message.", Profiles: []agentclient.SessionModelProfile{{Name: "deepinfra", Provider: "deepinfra"}}}, true)
	for _, want := range []string{"deepinfra / exact-model", "no fallback", "Next message.", "Saved profiles:"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if got := sessionModelText(agentclient.SessionModelStatus{}, false); !strings.Contains(got, "no override") {
		t.Fatal(got)
	}
}
func TestSessionModelStaleReplyIgnored(t *testing.T) {
	m := New(nil, false)
	model, cmd := m.Update(sessionModelMsg{convID: "another-session", status: agentclient.SessionModelStatus{Override: &agentclient.SessionModelRoute{Profile: "wrong", Model: "wrong"}}})
	got := model.(Model)
	if cmd != nil || got.convID != m.convID {
		t.Fatal("stale response changed session")
	}
}

func TestSessionTierConfirmationIsConcise(t *testing.T) {
	status := agentclient.SessionModelStatus{Override: &agentclient.SessionModelRoute{Profile: "private-account", Model: "gpt-6-astra"}, Note: "Verbose routing internals", Profiles: []agentclient.SessionModelProfile{{Name: "other-account"}}}
	got := sessionTierConfirmation("premium", status)
	if got != "Premium · gpt-6-astra (this session)." {
		t.Fatalf("unexpected confirmation: %q", got)
	}
	if strings.Contains(got, "\n") || strings.Contains(got, "account") {
		t.Fatal("tier confirmation leaked status details")
	}
	if !strings.Contains(sessionModelText(status, true), "Saved profiles:") {
		t.Fatal("explicit /model status lost details")
	}
}

func TestSessionTierReplyRendersConciseNotice(t *testing.T) {
	m := New(nil, false)
	status := agentclient.SessionModelStatus{Override: &agentclient.SessionModelRoute{Profile: "openai-responses", Model: "gpt-6-astra"}, Note: "Verbose routing internals", Profiles: []agentclient.SessionModelProfile{{Name: "other-account"}}}
	next, _ := m.Update(sessionModelMsg{convID: m.convID, tier: "premium", status: status})
	m = next.(Model)
	entries := m.mainChat().Entries()
	if len(entries) == 0 || entries[len(entries)-1].Content != "Premium · gpt-6-astra (this session)." {
		t.Fatalf("actual handler still prints verbose status: %+v", entries[len(entries)-1])
	}
}
