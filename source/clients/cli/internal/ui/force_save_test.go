package ui

import (
	"cercano/source/server/pkg/forcesave"
	"context"
	"testing"
	"time"
)

type fakeForceSave struct {
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
	return nil
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
