package server

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"cercano/source/server/internal/broker"
	"cercano/source/server/internal/updatecoord/state"
)

func TestLocalForceConstructorSingleton(t *testing.T) {
	s := &Server{}
	if o, e := s.newForceLocalOwner(); o != nil || e != nil {
		t.Fatal("unavailable owner must be optional", o, e)
	}
	s.SetProcessForceStopRequester(func(string) {})
	const n = 24
	results := make(chan *forceLocalOwner, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, e := s.newForceLocalOwner()
			if e != nil {
				t.Error(e)
			}
			results <- o
		}()
	}
	wg.Wait()
	close(results)
	var first *forceLocalOwner
	for o := range results {
		if o == nil {
			t.Fatal("missing owner")
		}
		if first == nil {
			first = o
		}
		if o != first {
			t.Fatal("multiple owner lifetimes created")
		}
	}
}

func TestLocalSaveFailureCleanupDuringRecordingCannotResurrect(t *testing.T) {
	root, store, op := forceLocalFixture(t)
	s, work, calls := failedSaveServer(t)
	ctx := context.Background()
	owner, e := s.newForceLocalOwner()
	if e != nil {
		t.Fatal(e)
	}
	if stopped, e := owner.poll(ctx, s, root, store.InstallID()); e != nil || stopped {
		t.Fatal(stopped, e)
	}
	intent := state.ForceIntent{OpID: op, Agent: owner.agent, IssuedMS: time.Now().UnixMilli(), ExpiresMS: time.Now().Add(20 * time.Second).UnixMilli()}
	if e = store.SubmitForceIntent(ctx, intent, true, time.Now); e != nil {
		t.Fatal(e)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	s.forceSaveRecorder = func(context.Context, []broker.ActiveTurn) error {
		close(entered)
		<-release
		return errors.New("fixture save failure")
	}
	type result struct {
		stopped bool
		err     error
	}
	done := make(chan result, 1)
	go func() { stopped, e := owner.poll(ctx, s, root, store.InstallID()); done <- result{stopped, e} }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("save did not begin")
	}
	owner.CleanupPendingSave(s)
	unblock()
	var out result
	select {
	case out = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("poll did not finish")
	}
	if out.stopped || work.Err() != nil || *calls != 0 {
		t.Fatal("cleanup stopped work", out)
	}
	var receipt *ForceSaveFailure
	if !errors.As(out.err, &receipt) {
		t.Fatal("expected save failure", out.err)
	}
	if owner.pendingSave != nil || s.pendingForceSave != nil {
		t.Fatal("cleanup resurrected an actionable receipt")
	}
	if _, found, e := store.ReadPendingSaveFailure(ctx, op); e != nil || found {
		t.Fatal("cleanup published a late failure", found, e)
	}
	if stopped, e := owner.poll(ctx, s, root, store.InstallID()); stopped || e != nil {
		t.Fatal("closed owner acted", stopped, e)
	}
}
