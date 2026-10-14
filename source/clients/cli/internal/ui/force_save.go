package ui

import (
	"cercano/source/server/pkg/forcesave"
	tea "charm.land/bubbletea/v2"
	"context"
	"time"
)

type forceSaveClient interface {
	Pending(context.Context) (forcesave.Receipt, bool, error)
	Decide(context.Context, forcesave.Receipt, bool, bool) error
}
type forceSavePollMsg struct {
	client  forceSaveClient
	receipt forcesave.Receipt
	found   bool
}
type forceSaveResultMsg struct {
	force bool
	err   error
}

func discoverForceSaveCmd() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	local, e := forcesave.Discover(ctx)
	if e != nil || local == nil {
		return forceSavePollMsg{}
	}
	r, ok, e := local.Pending(ctx)
	return forceSavePollMsg{local, r, ok && e == nil}
}
func pollForceSaveCmd(client forceSaveClient) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(2 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		r, ok, e := client.Pending(ctx)
		return forceSavePollMsg{client, r, ok && e == nil}
	}
}
func submitForceSaveCmd(client forceSaveClient, r forcesave.Receipt, force bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return forceSaveResultMsg{force, client.Decide(ctx, r, force, force)}
	}
}
func (m Model) applyForceSavePoll(msg forceSavePollMsg) (Model, tea.Cmd) {
	if msg.client == nil {
		return m, nil
	}
	next := pollForceSaveCmd(msg.client)
	if m.pendingConfirm != nil && m.pendingConfirm.forceSaveReceipt != "" {
		if !msg.found || msg.receipt.ID != m.pendingConfirm.forceSaveReceipt || !time.Now().Before(msg.receipt.Expires) {
			m.pendingConfirm = nil
			m.advanceConfirmation()
		}
	}

	// Do not displace another permission gate or silently approve an expired one.
	// Keep polling so a queued opportunity can be shown once the gate is cleared.
	if !msg.found || !time.Now().Before(msg.receipt.Expires) || msg.receipt.ID == m.forceSaveSeen || m.pendingConfirm != nil {
		return m, next
	}
	m.forceSaveSeen = msg.receipt.ID
	r, client := msg.receipt, msg.client
	decide := func(force bool) func(Model) (Model, tea.Cmd) {
		return func(m Model) (Model, tea.Cmd) {
			m.pendingConfirm = nil
			if !time.Now().Before(r.Expires) {
				return m.restartNotice("Update decision expired. No force override was submitted; work was not stopped by this choice.")
			}
			return m, submitForceSaveCmd(client, r, force)
		}
	}
	cancel, force := decide(false), decide(true)
	// Only the explicit F key authorizes force; generic Yes and Enter must not.
	m.pendingConfirm = &confirmRequest{forceSaveReceipt: r.ID,
		title: "Could not save interruption history", details: []string{
			"Cancel leaves the agent and active work running.",
			"Force anyway interrupts owned work despite incomplete history. Files or external systems may already have changed. Commands will not be automatically replayed.",
			"This choice expires shortly; it is not proof that an update completed.",
		}, hints: "c cancel · f force anyway", onYes: cancel, onNo: cancel,
		extras: map[string]func(Model) (Model, tea.Cmd){"c": cancel, "C": cancel, "f": force, "F": force},
	}
	return m, next
}
