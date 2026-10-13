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
