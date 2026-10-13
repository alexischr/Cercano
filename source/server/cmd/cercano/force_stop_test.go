package main

import (
	"cercano/source/server/internal/server"
	"context"
	"os"
	"testing"
)

func TestForceStopUsesOwnedChannelAndCannotBeDowngraded(t *testing.T) {
	owner := newProcessStopRequest()
	s := &server.Server{}
	configureProcessStopRequester(s, owner)
	owner.request("ordinary pending request")
	ok, e := s.RequestForceUpdateStop(context.Background(), int64(os.Getpid()), true)
	if !ok || e != nil || !owner.isForce() {
		t.Fatal(ok, e)
	}
	owner.request("ordinary repeat")
	if !owner.isForce() {
		t.Fatal("force intent downgraded")
	}
	select {
	case <-owner.wait():
	default:
		t.Fatal("owner not awakened")
	}
	// No signal, real server, provider or subprocess is created by this fixture.
}
func TestForceStopUnavailableWithoutLifecycleOwner(t *testing.T) {
	s := &server.Server{}
	configureProcessStopRequester(s, nil)
	if ok, e := s.RequestForceUpdateStop(context.Background(), int64(os.Getpid()), true); ok || e != server.ErrForceUnavailable {
		t.Fatal(ok, e)
	}
	var none *processStopRequest
	if none.isForce() {
		t.Fatal("embedded mode inferred force ownership")
	}
}
