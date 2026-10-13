package state

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestForceConsumptionAcrossHandlesAndReopen(t *testing.T) {
	s, a, f, now := forceFixture(t)
	ctx := context.Background()
	root := filepath.Dir(filepath.Dir(filepath.Dir(s.Directory())))
	if e := s.SubmitForceIntent(ctx, f, true, now); e != nil {
		t.Fatal(e)
	}
	other, e := Open(root, s.InstallID())
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	var wg sync.WaitGroup
	var wins atomic.Int32
	for _, store := range []*Store{s, other} {
		wg.Add(1)
		go func(store *Store) {
			defer wg.Done()
			_, ok, e := store.ConsumeForceIntent(ctx, a, now)
			if ok {
				wins.Add(1)
			} else if !errors.Is(e, ErrForceReplay) {
				t.Error(e)
			}
		}(store)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("not a database-atomic consume", wins.Load())
	}
	third, e := Open(root, s.InstallID())
	if e != nil {
		t.Fatal(e)
	}
	defer third.Close()
	if _, ok, e := third.ConsumeForceIntent(ctx, a, now); ok || !errors.Is(e, ErrForceReplay) {
		t.Fatal("reopen replayed intent", ok, e)
	}
}
func TestForceCorruptRowsRefuseWithoutConsumption(t *testing.T) {
	for _, query := range []string{
		`UPDATE force_stop_intents SET install_id='other'`,
		`UPDATE force_stop_intents SET boot_id='bad'`,
		`UPDATE force_stop_intents SET pid='text'`,
		`UPDATE force_stop_intents SET issued_ms='text'`,
		`UPDATE force_stop_intents SET expires_ms=issued_ms`,
		`PRAGMA ignore_check_constraints=ON; UPDATE force_stop_intents SET confirmed=0`,
		`PRAGMA ignore_check_constraints=ON; UPDATE force_stop_intents SET consumed=2`,
	} {
		t.Run(query, func(t *testing.T) {
			s, a, f, now := forceFixture(t)
			ctx := context.Background()
			if e := s.SubmitForceIntent(ctx, f, true, now); e != nil {
				t.Fatal(e)
			}
			if _, e := s.db.Exec(query); e != nil {
				t.Fatal(e)
			}
			if _, ok, e := s.ConsumeForceIntent(ctx, a, now); ok || e == nil {
				t.Fatal("corruption authorized force", ok, e)
			}
		})
	}
}
func TestForceSessionRefusesForeignRowsAndMissingConsent(t *testing.T) {
	s, _, f, now := forceFixture(t)
	if _, e := s.db.Exec(`UPDATE force_agent_sessions SET install_id='other'`); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.ReadForceAgent(context.Background()); e == nil {
		t.Fatal("foreign session accepted")
	}
	if e := s.SubmitForceIntent(context.Background(), f, true, now); e == nil {
		t.Fatal("foreign session authorized force")
	}
}
