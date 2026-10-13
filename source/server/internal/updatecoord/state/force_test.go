package state

import (
	"cercano/source/server/internal/updatecoord/operation"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func forceFixture(t *testing.T) (*Store, ForceAgent, ForceIntent, func() time.Time) {
	t.Helper()
	s, e := Open(t.TempDir(), "test-install")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	op := startCurrentOperation(t, s, "9.9.9")
	rec, r, e := s.LoadOperationRecord(ctx, op.ID)
	if e != nil {
		t.Fatal(e)
	}
	rec.State = operation.StateDraining
	rec.ConsentRecorded = true
	if _, e = s.SaveOperationRecord(ctx, r, rec); e != nil {
		t.Fatal(e)
	}
	j := journalFor(op.ID, "9.9.9")
	if _, e = s.SaveActivationJournal(ctx, 0, j); e != nil {
		t.Fatal(e)
	}
	a, e := NewForceAgent(123)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RegisterForceAgent(ctx, a); e != nil {
		t.Fatal(e)
	}
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	f := ForceIntent{OpID: op.ID, Agent: a, IssuedMS: now.UnixMilli(), ExpiresMS: now.Add(20 * time.Second).UnixMilli()}
	return s, a, f, clock
}
func TestForceIntentRequiresConsentPreparationAndExactLifetime(t *testing.T) {
	for _, kind := range []string{"force-consent", "operation-consent", "missing-journal", "advanced-journal", "wrong-pid", "wrong-boot", "expired", "future", "long-ttl"} {
		t.Run(kind, func(t *testing.T) {
			s, a, f, now := forceFixture(t)
			ctx := context.Background()
			confirmed := true
			switch kind {
			case "force-consent":
				confirmed = false
			case "operation-consent":
				rec, r, e := s.LoadOperationRecord(ctx, f.OpID)
				if e != nil {
					t.Fatal(e)
				}
				rec.ConsentRecorded = false
				if _, e = s.SaveOperationRecord(ctx, r, rec); e != nil {
					t.Fatal(e)
				}
			case "missing-journal":
				if _, e := s.db.Exec(`DELETE FROM activation_journals`); e != nil {
					t.Fatal(e)
				}
			case "advanced-journal":
				j, r, e := s.LoadActivationJournal(ctx, f.OpID)
				if e != nil {
					t.Fatal(e)
				}
				j.Checkpoint = JournalSwitchIntent
				if _, e = s.SaveActivationJournal(ctx, r, j); e != nil {
					t.Fatal(e)
				}
			case "wrong-pid":
				f.Agent.PID++
			case "wrong-boot":
				f.Agent, _ = NewForceAgent(a.PID)
			case "expired":
				f.ExpiresMS = now().UnixMilli()
			case "future":
				f.IssuedMS++
				f.ExpiresMS++
			case "long-ttl":
				f.ExpiresMS = f.IssuedMS + MaxForceIntentTTL.Milliseconds() + 1
			}
			if e := s.SubmitForceIntent(ctx, f, confirmed, now); e == nil {
				t.Fatal("unsafe force accepted", kind)
			}
			var count int
			if e := s.db.QueryRow(`SELECT COUNT(*) FROM force_stop_intents`).Scan(&count); e != nil || count != 0 {
				t.Fatal("refusal wrote intent", count, e)
			}
		})
	}
}
func TestForceIntentConsumedExactlyOnceAndCannotRetarget(t *testing.T) {
	s, a, f, now := forceFixture(t)
	ctx := context.Background()
	if e := s.SubmitForceIntent(ctx, f, true, now); e != nil {
		t.Fatal(e)
	}
	f2 := f
	f2.ExpiresMS++
	if e := s.SubmitForceIntent(ctx, f2, true, now); !errors.Is(e, ErrForceReplay) {
		t.Fatal("intent replaced", e)
	}
	var wg sync.WaitGroup
	var wins atomic.Int32
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, ok, e := s.ConsumeForceIntent(ctx, a, now)
			if ok {
				if e != nil || got != f {
					t.Error(got, e)
				}
				wins.Add(1)
			} else if !errors.Is(e, ErrForceReplay) {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("multiple force permissions", wins.Load())
	}
}
func TestForceIntentRevokedByAgentReplacementOrStateChange(t *testing.T) {
	for _, kind := range []string{"agent", "pid", "expiry", "journal", "operation"} {
		t.Run(kind, func(t *testing.T) {
			s, a, f, now := forceFixture(t)
			ctx := context.Background()
			if e := s.SubmitForceIntent(ctx, f, true, now); e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "agent":
				next, _ := NewForceAgent(a.PID)
				if e := s.RegisterForceAgent(ctx, next); e != nil {
					t.Fatal(e)
				}
			case "pid":
				a.PID++
			case "expiry":
				base := now()
				now = func() time.Time { return base.Add(time.Minute) }
			case "journal":
				j, r, e := s.LoadActivationJournal(ctx, f.OpID)
				if e != nil {
					t.Fatal(e)
				}
				j.Checkpoint = JournalSwitchIntent
				if _, e = s.SaveActivationJournal(ctx, r, j); e != nil {
					t.Fatal(e)
				}
			case "operation":
				rec, r, e := s.LoadOperationRecord(ctx, f.OpID)
				if e != nil {
					t.Fatal(e)
				}
				rec.State = operation.StateInstalling
				if _, e = s.SaveOperationRecord(ctx, r, rec); e != nil {
					t.Fatal(e)
				}
			}
			if _, ok, e := s.ConsumeForceIntent(ctx, a, now); ok || e == nil {
				t.Fatal("revoked request consumed", ok, e)
			}
			var consumed int
			if e := s.db.QueryRow(`SELECT consumed FROM force_stop_intents`).Scan(&consumed); e != nil || consumed != 0 {
				t.Fatal("refusal changed consumption", consumed, e)
			}
		})
	}
}
func TestForceIntentCancellationAndExpiryRollBackConsumption(t *testing.T) {
	s, a, f, now := forceFixture(t)
	ctx := context.Background()
	if e := s.SubmitForceIntent(ctx, f, true, now); e != nil {
		t.Fatal(e)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, ok, e := s.ConsumeForceIntent(canceled, a, now); ok || !errors.Is(e, context.Canceled) {
		t.Fatal(ok, e)
	}
	calls := 0
	base := now()
	jump := func() time.Time {
		calls++
		if calls == 1 {
			return base
		}
		return base.Add(time.Minute)
	}
	if _, ok, e := s.ConsumeForceIntent(ctx, a, jump); ok || !errors.Is(e, ErrForceIntent) {
		t.Fatal("expiry did not roll back", ok, e)
	}
	if got, ok, e := s.ConsumeForceIntent(ctx, a, now); !ok || e != nil || got != f {
		t.Fatal("refused attempts consumed request", got, ok, e)
	}
}
