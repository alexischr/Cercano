package ui

import (
	"cercano/source/server/pkg/forcesave"
	"context"
	"errors"
	"testing"
	"time"
)

type fakeForceSave struct {
	err              error
	calls            int
	force, confirmed bool
}

func (f *fakeForceSave) Pending(context.Context) (forcesave.Receipt, bool, error) {
	return forcesave.Receipt{}, false, nil
}
func (f *fakeForceSave) Decide(_ context.Context, _ forcesave.Receipt, force, confirmed bool) error {
	f.calls++
	f.force = force
	f.confirmed = confirmed
	return f.err
}
func TestForceSavePromptExplicitChoice(t *testing.T) {
	for _, key := range []string{"f", "F", "c", "C", "n", "esc", "y", "enter"} {
		t.Run(key, func(t *testing.T) {
			client := &fakeForceSave{}
			m := Model{}
			m, cmd := m.applyForceSavePoll(forceSavePollMsg{client, forcesave.Receipt{ID: "fixture", OperationID: 1, Expires: time.Now().Add(time.Minute)}, true})
			if cmd == nil || m.pendingConfirm == nil || client.calls != 0 {
				t.Fatal("poll did not present passive choice")
			}
			m, cmd, _ = m.resolveConfirmHotkey(key)
			if cmd != nil {
				cmd()
			}
			if key == "enter" {
				if client.calls != 0 {
					t.Fatal("enter authorized force")
				}
				return
			}
			wantForce := key == "f" || key == "F"
			if client.calls != 1 || client.force != wantForce || client.confirmed != wantForce {
				t.Fatal("wrong authorization", client)
			}
		})
	}
}
func TestForceSavePromptDoesNotDisplaceOrRepeat(t *testing.T) {
	client := &fakeForceSave{}
	msg := forceSavePollMsg{client, forcesave.Receipt{ID: "fixture", Expires: time.Now().Add(time.Minute)}, true}
	existing := &confirmRequest{title: "existing"}
	m := Model{pendingConfirm: existing}
	m, _ = m.applyForceSavePoll(msg)
	if m.pendingConfirm != existing || m.forceSaveSeen != "" {
		t.Fatal("displaced existing gate")
	}
	m.pendingConfirm = nil
	m, _ = m.applyForceSavePoll(msg)
	if m.pendingConfirm == nil {
		t.Fatal("never presented")
	}
	m.pendingConfirm = nil
	m, _ = m.applyForceSavePoll(msg)
	if m.pendingConfirm != nil {
		t.Fatal("duplicate prompt")
	}
	msg.receipt.ID = "expired"
	msg.receipt.Expires = time.Now().Add(-time.Second)
	m, _ = m.applyForceSavePoll(msg)
	if m.pendingConfirm != nil || client.calls != 0 {
		t.Fatal("expired choice offered")
	}
}

func TestForceSavePromptWithdrawnWhenNoLongerPending(t *testing.T) {
	client := &fakeForceSave{}
	m := Model{}
	msg := forceSavePollMsg{client, forcesave.Receipt{ID: "receipt", Expires: time.Now().Add(time.Minute)}, true}
	m, _ = m.applyForceSavePoll(msg)
	if m.pendingConfirm == nil {
		t.Fatal("missing prompt")
	}
	m, _ = m.applyForceSavePoll(forceSavePollMsg{client: client})
	if m.pendingConfirm != nil || client.calls != 0 {
		t.Fatal("stale prompt retained or submitted")
	}
}

func TestForceSaveSubmissionErrorDoesNotReplayConsent(t *testing.T) {
	client := &fakeForceSave{err: errors.New("ambiguous commit fixture")}
	msg := forceSavePollMsg{client, forcesave.Receipt{ID: "receipt", Expires: time.Now().Add(time.Minute)}, true}
	m := Model{}
	m, _ = m.applyForceSavePoll(msg)
	m, cmd, handled := m.resolveConfirmHotkey("f")
	if !handled || cmd == nil {
		t.Fatal("missing explicit force submission")
	}
	result, ok := cmd().(forceSaveResultMsg)
	if !ok || result.err == nil || !result.force || client.calls != 1 {
		t.Fatal("lost submission uncertainty", result, client)
	}
	// An unchanged pending receipt after a response failure must not reopen the
	// prompt or submit consent again automatically.
	m, _ = m.applyForceSavePoll(msg)
	if m.pendingConfirm != nil || client.calls != 1 {
		t.Fatal("ambiguous response replayed consent")
	}
}

func TestForceSaveReceiptExpiresWhilePromptOpen(t *testing.T) {
	client := &fakeForceSave{}
	deadline := time.Now().Add(50 * time.Millisecond)
	m := Model{}
	m, _ = m.applyForceSavePoll(forceSavePollMsg{client, forcesave.Receipt{ID: "receipt", Expires: deadline}, true})
	if m.pendingConfirm == nil {
		t.Fatal("prompt not presented before expiry")
	}
	time.Sleep(time.Until(deadline) + time.Millisecond)
	// No new poll has yet cleared the gate: the key handler must independently
	// refuse an expired receipt rather than rely solely on polling.
	m, cmd, handled := m.resolveConfirmHotkey("f")
	if !handled {
		t.Fatal("force key not handled")
	}
	if cmd != nil {
		cmd()
	}
	if m.pendingConfirm != nil || client.calls != 0 {
		t.Fatal("expired consent was submitted")
	}
}
