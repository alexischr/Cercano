package server

import (
	"cercano/source/server/internal/broker"
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
)

func TestForceStopSealsBusyAdmissionAndRetainsInterruptionGeneration(t *testing.T) {
	s := &Server{turnBroker: broker.New()}
	leave, e := s.updateWork.enter()
	if e != nil {
		t.Fatal(e)
	}
	defer leave()
	owned, gen, release := s.turnBroker.BeginTurn(context.Background(), "owned")
	defer release()
	var calls atomic.Int32
	s.SetProcessForceStopRequester(func(string) { calls.Add(1) })
	ok, e := s.RequestForceUpdateStop(context.Background(), int64(os.Getpid()), true)
	if !ok || e != nil || calls.Load() != 1 {
		t.Fatal(ok, e, calls.Load())
	}
	if owned.Err() != context.Canceled || !s.turnBroker.IsCurrent("owned", gen) || !s.turnBroker.HasActiveTurn("owned") {
		t.Fatal("owned work falsely completed or terminal generation discarded")
	}
	s.updateWork.mu.Lock()
	count, paused := s.updateWork.active, s.updateWork.paused
	s.updateWork.mu.Unlock()
	if count != 1 || !paused {
		t.Fatal("active work count reset", count, paused)
	}
	if newRelease, e := s.updateWork.enter(); e == nil {
		newRelease()
		t.Fatal("new work admitted")
	}
	if s.safeStopAlreadyCommitted() {
		t.Fatal("force incorrectly claims idle stop")
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, e := s.RequestForceUpdateStop(context.Background(), int64(os.Getpid()), true); !ok || e != nil {
				t.Error(ok, e)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("duplicate stop callback")
	}
}
func TestForceStopRefusalHasNoEffects(t *testing.T) {
	for _, kind := range []string{"no-consent", "wrong-pid", "no-owner", "canceled", "other-seal"} {
		t.Run(kind, func(t *testing.T) {
			s := &Server{turnBroker: broker.New()}
			work, _, release := s.turnBroker.BeginTurn(context.Background(), "owned")
			defer release()
			calls := 0
			s.SetProcessForceStopRequester(func(string) { calls++ })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			pid := int64(os.Getpid())
			confirmed := true
			switch kind {
			case "no-consent":
				confirmed = false
			case "wrong-pid":
				pid++
			case "no-owner":
				s.SetProcessForceStopRequester(nil)
			case "canceled":
				cancel()
			case "other-seal":
				r, e := s.updateWork.pauseWhenIdle(context.Background())
				if e != nil {
					t.Fatal(e)
				}
				defer r()
			}
			ok, e := s.RequestForceUpdateStop(ctx, pid, confirmed)
			if ok || e == nil || calls != 0 || work.Err() != nil || s.forceStopCommitted {
				t.Fatal("refusal had effects", ok, e, calls)
			}
			if kind == "canceled" && !errors.Is(e, context.Canceled) {
				t.Fatal(e)
			}
			if kind != "other-seal" {
				r, e := s.updateWork.enter()
				if e != nil {
					t.Fatal("refusal sealed admission", e)
				}
				r()
			}
		})
	}
}
