package state

import (
	"context"
	"time"
)

// ForceControl exposes only the approved local control surface. It cannot
// initialize/migrate a database, allocate operations, select versions or rewrite
// the install journal. Opening requires an existing current dedicated database.
type ForceControl struct{ store *Store }

func OpenExistingForceControl(ctx context.Context, root, id string) (*ForceControl, error) {
	s, e := openExistingCurrent(ctx, root, id, false)
	if e != nil {
		return nil, e
	}
	return &ForceControl{store: s}, nil
}
func (c *ForceControl) Close() error { return c.store.Close() }
func (c *ForceControl) RegisterAgent(ctx context.Context, a ForceAgent) error {
	return c.store.RegisterForceAgent(ctx, a)
}
func (c *ForceControl) Agent(ctx context.Context) (ForceAgent, bool, error) {
	return c.store.ReadForceAgent(ctx)
}
func (c *ForceControl) Submit(ctx context.Context, f ForceIntent, confirmed bool, now func() time.Time) error {
	return c.store.SubmitForceIntent(ctx, f, confirmed, now)
}
func (c *ForceControl) Consume(ctx context.Context, a ForceAgent, now func() time.Time) (ForceIntent, bool, error) {
	return c.store.ConsumeForceIntent(ctx, a, now)
}

func (c *ForceControl) Intent(ctx context.Context, opID int64) (ForceIntent, bool, bool, error) {
	return c.store.ReadForceIntent(ctx, opID)
}

// PublishSaveFailure publishes an idempotent save failure for the given operation.
// The caller supplies a crypto-random receiptID for idempotent publication.
func (c *ForceControl) PublishSaveFailure(ctx context.Context, opID int64, agent ForceAgent, receiptID string, expires time.Time, now func() time.Time) error {
	return c.store.PublishSaveFailure(ctx, opID, agent, receiptID, expires, now)
}

// PendingSaveFailure reads a pending save failure for the given operation.
func (c *ForceControl) PendingSaveFailure(ctx context.Context, opID int64) (ForceSaveFailure, bool, error) {
	return c.store.ReadPendingSaveFailure(ctx, opID)
}

// SubmitSaveDecision submits an explicit force confirmation for a save failure.
func (c *ForceControl) SubmitSaveDecision(ctx context.Context, receiptID string, decision ForceDecision, confirmed bool, now func() time.Time) error {
	return c.store.SubmitSaveDecision(ctx, receiptID, decision, confirmed, now)
}

// ConsumeSaveDecision consumes a save failure decision once.
func (c *ForceControl) ConsumeSaveDecision(ctx context.Context, receiptID string, owned ForceAgent, now func() time.Time) error {
	return c.store.ConsumeSaveDecision(ctx, receiptID, owned, now)
}
