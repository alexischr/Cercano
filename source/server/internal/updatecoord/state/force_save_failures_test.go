package state

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cercano/source/server/internal/updatecoord/operation"
)

func forceSaveFailuresFixture(t *testing.T) (*Store, ForceAgent, ForceIntent, func() time.Time) {
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

func generateReceiptID() (string, error) {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return hex.EncodeToString(b[:]), nil
}

func TestPublishSaveFailureRequiresEligibleOperationAndAgent(t *testing.T) {
	for _, kind := range []string{"no-agent", "wrong-agent", "ineligible-op", "no-force-intent", "invalid-receipt", "expired", "future"} {
		t.Run(kind, func(t *testing.T) {
			s, a, f, now := forceSaveFailuresFixture(t)
			ctx := context.Background()
			receiptID, _ := generateReceiptID()
			expires := now().Add(10 * time.Second)

			if kind != "no-force-intent" {
				if e := s.SubmitForceIntent(ctx, f, true, now); e != nil {
					t.Fatal(e)
				}
				if _, claimed, e := s.ConsumeForceIntent(ctx, a, now); e != nil || !claimed {
					t.Fatal("consume original intent", claimed, e)
				}
			}

			switch kind {
			case "no-agent":
				if _, e := s.db.Exec(`DELETE FROM force_agent_sessions`); e != nil {
					t.Fatal(e)
				}
			case "wrong-agent":
				wrong, _ := NewForceAgent(456)
				if e := s.RegisterForceAgent(ctx, wrong); e != nil {
					t.Fatal(e)
				}
			case "ineligible-op":
				rec, r, e := s.LoadOperationRecord(ctx, f.OpID)
				if e != nil {
					t.Fatal(e)
				}
				rec.State = operation.StateInstalling
				if _, e = s.SaveOperationRecord(ctx, r, rec); e != nil {
					t.Fatal(e)
				}
			case "no-force-intent":
				// Don't submit force intent
			case "invalid-receipt":
				receiptID = ""
			case "expired":
				expires = now().Add(-1 * time.Second)
			case "future":
				expires = now().Add(31 * time.Second)
			}

			if e := s.PublishSaveFailure(ctx, f.OpID, a, receiptID, expires, now); e == nil {
				t.Fatal("unsafe save failure accepted", kind)
			}

			var count int
			if e := s.db.QueryRow(`SELECT COUNT(*) FROM force_save_failures`).Scan(&count); e != nil || count != 0 {
				t.Fatal("refusal wrote save failure", count, e)
			}
		})
	}
}

func TestPublishSaveFailureIsIdempotent(t *testing.T) {
	s, a, f, now := forceSaveFailuresFixture(t)
	ctx := context.Background()
	if e := s.SubmitForceIntent(ctx, f, true, now); e != nil {
		t.Fatal(e)
	}
	if _, claimed, e := s.ConsumeForceIntent(ctx, a, now); e != nil || !claimed {
		t.Fatal("consume original intent", claimed, e)
	}
	receiptID, _ := generateReceiptID()
	expires := now().Add(10 * time.Second)

	if e := s.PublishSaveFailure(ctx, f.OpID, a, receiptID, expires, now); e != nil {
		t.Fatal(e)
	}

	// Identical publication reconciles a possibly ambiguous successful commit.
	if e := s.PublishSaveFailure(ctx, f.OpID, a, receiptID, expires, now); e != nil {
		t.Fatal(e)
	}

	var count int
	if e := s.db.QueryRow(`SELECT COUNT(*) FROM force_save_failures`).Scan(&count); e != nil || count != 1 {
		t.Fatal("duplicate save failure", count, e)
	}
}

func TestSubmitSaveDecisionRequiresPendingAndValid(t *testing.T) {
	for _, kind := range []string{"not-found", "already-confirmed", "already-consumed", "expired", "wrong-op"} {
		t.Run(kind, func(t *testing.T) {
			s, a, f, now := forceSaveFailuresFixture(t)
			ctx := context.Background()
			if e := s.SubmitForceIntent(ctx, f, true, now); e != nil {
				t.Fatal(e)
			}
			if _, claimed, e := s.ConsumeForceIntent(ctx, a, now); e != nil || !claimed {
				t.Fatal("consume original intent", claimed, e)
			}
			receiptID, _ := generateReceiptID()
			expires := now().Add(10 * time.Second)

			if e := s.PublishSaveFailure(ctx, f.OpID, a, receiptID, expires, now); e != nil {
				t.Fatal(e)
			}

			switch kind {
			case "not-found":
				receiptID = "nonexistent"
			case "already-confirmed":
				if e := s.SubmitSaveDecision(ctx, receiptID, ForceDecisionForce, true, now); e != nil {
					t.Fatal(e)
				}

			case "already-consumed":
				if e := s.SubmitSaveDecision(ctx, receiptID, ForceDecisionForce, true, now); e != nil {
					t.Fatal(e)
				}
				if e := s.ConsumeSaveDecision(ctx, receiptID, a, now); e != nil {
					t.Fatal(e)
				}
			case "expired":
				later := now().Add(31 * time.Second)
				now = func() time.Time { return later }
			case "wrong-op":
				rec, r, e := s.LoadOperationRecord(ctx, f.OpID)
				if e != nil {
					t.Fatal(e)
				}
				rec.State = operation.StateInstalling
				if _, e = s.SaveOperationRecord(ctx, r, rec); e != nil {
					t.Fatal(e)
				}
			}

			if e := s.SubmitSaveDecision(ctx, receiptID, ForceDecisionForce, true, now); e == nil {
				t.Fatal("invalid decision accepted", kind)
			}

		})
	}
}

func TestConsumeSaveDecisionIsExactlyOnce(t *testing.T) {
	s, a, f, now := forceSaveFailuresFixture(t)
	ctx := context.Background()
	if e := s.SubmitForceIntent(ctx, f, true, now); e != nil {
		t.Fatal(e)
	}
	if _, claimed, e := s.ConsumeForceIntent(ctx, a, now); e != nil || !claimed {
		t.Fatal("consume original intent", claimed, e)
	}
	receiptID, _ := generateReceiptID()
	expires := now().Add(10 * time.Second)

	if e := s.PublishSaveFailure(ctx, f.OpID, a, receiptID, expires, now); e != nil {
		t.Fatal(e)
	}

	if e := s.SubmitSaveDecision(ctx, receiptID, ForceDecisionForce, true, now); e != nil {
		t.Fatal(e)
	}

	var wg sync.WaitGroup
	var wins atomic.Int32
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := s.ConsumeSaveDecision(ctx, receiptID, a, now); e == nil {
				wins.Add(1)
			} else if !errors.Is(e, ErrSaveFailureReplay) {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("multiple consumptions", wins.Load())
	}

	// Second consume should fail
	if e := s.ConsumeSaveDecision(ctx, receiptID, a, now); !errors.Is(e, ErrSaveFailureReplay) {
		t.Fatal("replayed consumption", e)
	}
}

func TestSaveFailureReconciliation(t *testing.T) {
	s, a, f, now := forceSaveFailuresFixture(t)
	ctx := context.Background()
	if e := s.SubmitForceIntent(ctx, f, true, now); e != nil {
		t.Fatal(e)
	}
	if _, claimed, e := s.ConsumeForceIntent(ctx, a, now); e != nil || !claimed {
		t.Fatal("consume original intent", claimed, e)
	}
	receiptID, _ := generateReceiptID()
	expires := now().Add(10 * time.Second)

	// Publish save failure
	if e := s.PublishSaveFailure(ctx, f.OpID, a, receiptID, expires, now); e != nil {
		t.Fatal(e)
	}

	// Read pending failure
	sf, found, e := s.ReadPendingSaveFailure(ctx, f.OpID)
	if e != nil || !found || sf.ReceiptID != receiptID {
		t.Fatal("pending save failure not found", sf, found, e)
	}

	// Submit decision
	if e := s.SubmitSaveDecision(ctx, receiptID, ForceDecisionForce, true, now); e != nil {
		t.Fatal(e)
	}

	// Consume decision
	if e := s.ConsumeSaveDecision(ctx, receiptID, a, now); e != nil {
		t.Fatal(e)
	}

	// Should not be found as pending
	sf, found, e = s.ReadPendingSaveFailure(ctx, f.OpID)
	if e != nil || found {
		t.Fatal("consumed save failure still pending", sf, found, e)
	}
}

func TestSaveFailureCorruptionRefusal(t *testing.T) {
	for _, query := range []string{
		`UPDATE force_save_failures SET install_id='other'`,
		`UPDATE force_save_failures SET boot_id='bad'`,
		`UPDATE force_save_failures SET pid='text'`,
		`UPDATE force_save_failures SET issued_ms='text'`,
		`UPDATE force_save_failures SET expires_ms=issued_ms`,
		`PRAGMA ignore_check_constraints=ON; UPDATE force_save_failures SET decision=3`,
		`PRAGMA ignore_check_constraints=ON; UPDATE force_save_failures SET confirmed=2`,
		`PRAGMA ignore_check_constraints=ON; UPDATE force_save_failures SET consumed=2`,
	} {
		t.Run(query, func(t *testing.T) {
			s, a, f, now := forceSaveFailuresFixture(t)
			ctx := context.Background()
			if e := s.SubmitForceIntent(ctx, f, true, now); e != nil {
				t.Fatal(e)
			}
			if _, claimed, e := s.ConsumeForceIntent(ctx, a, now); e != nil || !claimed {
				t.Fatal("consume original intent", claimed, e)
			}
			receiptID, _ := generateReceiptID()
			expires := now().Add(10 * time.Second)

			if e := s.PublishSaveFailure(ctx, f.OpID, a, receiptID, expires, now); e != nil {
				t.Fatal(e)
			}

			if _, e := s.db.Exec(query); e != nil {
				t.Fatal(e)
			}

			if e := s.SubmitSaveDecision(ctx, receiptID, ForceDecisionForce, true, now); e == nil {
				t.Fatal("corruption authorized decision", e)
			}
		})
	}
}
func TestSaveFailureRequiresConsumedIntent(t *testing.T) {
	s, a, f, now := forceSaveFailuresFixture(t)
	ctx := context.Background()
	if err := s.SubmitForceIntent(ctx, f, true, now); err != nil {
		t.Fatal(err)
	}
	id, err := generateReceiptID()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.PublishSaveFailure(ctx, f.OpID, a, id, now().Add(10*time.Second), now); err == nil {
		t.Fatal("published failure without consuming original force intent")
	}
}

func publishedSaveFailure(t *testing.T) (*Store, ForceAgent, ForceIntent, string, func() time.Time) {
	t.Helper()
	s, a, f, now := forceSaveFailuresFixture(t)
	ctx := context.Background()
	if e := s.SubmitForceIntent(ctx, f, true, now); e != nil {
		t.Fatal(e)
	}
	if _, ok, e := s.ConsumeForceIntent(ctx, a, now); e != nil || !ok {
		t.Fatal(ok, e)
	}
	id, e := generateReceiptID()
	if e != nil {
		t.Fatal(e)
	}
	if e = s.PublishSaveFailure(ctx, f.OpID, a, id, now().Add(10*time.Second), now); e != nil {
		t.Fatal(e)
	}
	return s, a, f, id, now
}

func TestSaveDecisionRequiresFreshConfirmation(t *testing.T) {
	s, a, f, id, now := publishedSaveFailure(t)
	ctx := context.Background()
	for _, decision := range []ForceDecision{ForceDecisionPending, ForceDecisionForce, ForceDecision(3)} {
		if e := s.SubmitSaveDecision(ctx, id, decision, false, now); e == nil {
			t.Fatal("unconfirmed/invalid decision accepted", decision)
		}
	}
	if e := s.ConsumeSaveDecision(ctx, id, a, now); e == nil {
		t.Fatal("undecided receipt consumed")
	}
	pending, ok, e := s.ReadPendingSaveFailure(ctx, f.OpID)
	if e != nil || !ok || pending.Confirmed || pending.Decision != ForceDecisionPending {
		t.Fatal(pending, ok, e)
	}
	if e = s.SubmitSaveDecision(ctx, id, ForceDecisionCancel, false, now); e != nil {
		t.Fatal(e)
	}
	// A publication retry cannot reset cancellation or extend its deadline.
	if e = s.PublishSaveFailure(ctx, f.OpID, a, id, now().Add(10*time.Second), now); e != nil {
		t.Fatal(e)
	}
	if e = s.PublishSaveFailure(ctx, f.OpID, a, id, now().Add(20*time.Second), now); e == nil {
		t.Fatal("deadline extended")
	}
	pending, ok, e = s.ReadPendingSaveFailure(ctx, f.OpID)
	if e != nil || !ok || pending.Decision != ForceDecisionCancel || !pending.Confirmed {
		t.Fatal(pending, ok, e)
	}
	if e = s.SubmitSaveDecision(ctx, id, ForceDecisionForce, true, now); e == nil {
		t.Fatal("cancel replaced with force")
	}
	if e = s.ConsumeSaveDecision(ctx, id, a, now); e != nil {
		t.Fatal(e)
	}
	if e = s.PublishSaveFailure(ctx, f.OpID, a, id, now().Add(10*time.Second), now); e != nil {
		t.Fatal(e)
	}
	if e = s.ConsumeSaveDecision(ctx, id, a, now); e == nil {
		t.Fatal("publication resurrected consumed decision")
	}
}

func TestSaveDecisionRevokesChangedAuthority(t *testing.T) {
	for _, phase := range []string{"submit", "consume"} {
		for _, change := range []string{"agent", "operation", "intent", "expired", "backward-clock"} {
			t.Run(phase+"/"+change, func(t *testing.T) {
				s, a, f, id, now := publishedSaveFailure(t)
				ctx := context.Background()
				if phase == "consume" {
					if e := s.SubmitSaveDecision(ctx, id, ForceDecisionForce, true, now); e != nil {
						t.Fatal(e)
					}
				}
				switch change {
				case "agent":
					replacement, e := NewForceAgent(a.PID)
					if e != nil {
						t.Fatal(e)
					}
					if e = s.RegisterForceAgent(ctx, replacement); e != nil {
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
				case "intent":
					if _, e := s.db.Exec(`UPDATE force_stop_intents SET consumed=0`); e != nil {
						t.Fatal(e)
					}
				case "expired":
					later := now().Add(10 * time.Second)
					now = func() time.Time { return later }
				case "backward-clock":
					earlier := now().Add(-time.Millisecond)
					now = func() time.Time { return earlier }
				}
				var e error
				if phase == "submit" {
					e = s.SubmitSaveDecision(ctx, id, ForceDecisionForce, true, now)
				} else {
					e = s.ConsumeSaveDecision(ctx, id, a, now)
				}
				if e == nil {
					t.Fatal("changed authority accepted")
				}
				pending, ok, e := s.ReadPendingSaveFailure(ctx, f.OpID)
				if e != nil || !ok || pending.Consumed || (phase == "submit" && pending.Confirmed) {
					t.Fatal("refusal mutated decision", pending, ok, e)
				}
			})
		}
	}
}

func TestSaveDecisionRejectsForeignConsumer(t *testing.T) {
	s, a, _, id, now := publishedSaveFailure(t)
	ctx := context.Background()
	if e := s.SubmitSaveDecision(ctx, id, ForceDecisionForce, true, now); e != nil {
		t.Fatal(e)
	}
	foreign, e := NewForceAgent(a.PID)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConsumeSaveDecision(ctx, id, foreign, now); e == nil {
		t.Fatal("foreign consumer accepted")
	}
	if e = s.ConsumeSaveDecision(ctx, id, a, now); e != nil {
		t.Fatal(e)
	}
}
