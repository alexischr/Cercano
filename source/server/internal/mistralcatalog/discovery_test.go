package mistralcatalog

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cercano/source/server/internal/catalog"
)

func TestSharedCatalogAccountIsolation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path=%s", r.URL.Path)
		}
		switch r.Header.Get("Authorization") {
		case "Bearer personal":
			fmt.Fprint(w, `{"data":[{"id":"shared","max_context_length":4096,"capabilities":{"completion_chat":true,"function_calling":true,"vision":false}},{"id":"personal-private"}]}`)
		case "Bearer work":
			fmt.Fprint(w, `{"data":[{"id":"shared","max_context_length":8192,"capabilities":{"completion_chat":true,"function_calling":true,"vision":true}},{"id":"work-private","type":"fine-tuned"}]}`)
		default:
			w.WriteHeader(401)
			fmt.Fprint(w, "SECRET must never appear")
		}
	}))
	defer srv.Close()
	s := New(srv.Client())
	accounts := []Account{{"personal", srv.URL + "/v1", "personal"}, {"work", srv.URL + "/v1", "work"}}
	s.Sync(accounts)
	all, status := s.List(context.Background(), "")
	if len(all) != 3 || len(status) != 2 {
		t.Fatalf("union=%+v status=%+v", all, status)
	}
	shared := all[1]
	if shared.ID != "shared" || len(shared.Observations) != 2 || shared.Metadata.Present() {
		t.Fatalf("improper metadata merge: %+v", shared)
	}
	if shared.Observations[0].Metadata.ContextLength != 4096 || shared.Observations[1].Metadata.ContextLength != 8192 {
		t.Fatalf("lost differing context: %+v", shared)
	}
	personal, _ := s.List(context.Background(), "personal")
	if len(personal) != 2 || personal[1].Metadata.Vision != catalog.CapabilityUnsupported {
		t.Fatalf("personal=%+v", personal)
	}
	for _, m := range personal {
		if strings.Contains(m.ID, "work") || len(m.Observations) != 1 {
			t.Fatalf("foreign data: %+v", m)
		}
	}
	all[1].Observations[0].Metadata.ContextLength = 1
	personal, _ = s.List(context.Background(), "personal")
	if personal[1].Metadata.ContextLength != 4096 {
		t.Fatal("caller mutated cache")
	}
	accounts[0].Key = "bad"
	s.Sync(accounts)
	all, status = s.List(context.Background(), "")
	if len(all) != 2 || status[0].Error != "Mistral catalog HTTP 401" {
		t.Fatalf("rotation retained stale access: %+v %+v", all, status)
	}
	s.Sync(accounts[1:])
	all, status = s.List(context.Background(), "")
	if len(all) != 2 || len(status) != 1 {
		t.Fatalf("deletion failed: %+v %+v", all, status)
	}
}

func TestDiscoverySafetyAndPresence(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		code       int
		wantErr    bool
	}{
		{"missing-data", `{}`, 200, true}, {"null-data", `{"data":null}`, 200, true},
		{"malformed", `{`, 200, true}, {"empty", `{"data":[]}`, 200, false},
		{"duplicate", `{"data":[{"id":"x"},{"id":"x"}]}`, 200, true},
		{"missing-id", `{"data":[{}]}`, 200, true},
		{"retirement", `{"data":[{"id":"x","deprecation":"nonsense"}]}`, 200, true},
		{"unauthorized", "SECRET", 401, true},
		{"oversized", strings.Repeat("x", maxResponseBytes+1), 200, true},
		{"sparse", `{"data":[{"id":"x","capabilities":{"vision":false}}]}`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.code); fmt.Fprint(w, tc.body) }))
			defer srv.Close()
			models, err := Fetch(context.Background(), srv.Client(), Account{"a", srv.URL, "SECRET"})
			if (err != nil) != tc.wantErr {
				t.Fatalf("models=%+v err=%v", models, err)
			}
			if err != nil && strings.Contains(err.Error(), "SECRET") {
				t.Fatal("credential/body leaked")
			}
			if tc.name == "sparse" {
				m := models[0].Metadata
				if m.Vision != catalog.CapabilityUnsupported || m.Chat != catalog.CapabilityUnknown || m.PriceIn != nil || m.AgentEligible(time.Now()) {
					t.Fatalf("invented metadata: %+v", m)
				}
			}
		})
	}
}

func TestRedirectRefused(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer srv.Close()
	_, err := Fetch(context.Background(), srv.Client(), Account{"a", srv.URL, "secret"})
	if err == nil || calls.Load() != 0 {
		t.Fatalf("redirect followed: calls=%d err=%v", calls.Load(), err)
	}
}

func TestRotationDiscardsInflightResponse(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer old" {
			close(started)
			<-release
			fmt.Fprint(w, `{"data":[{"id":"old-private"}]}`)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"new-private"}]}`)
	}))
	defer srv.Close()
	s := New(srv.Client())
	s.Sync([]Account{{"a", srv.URL, "old"}})
	done := make(chan struct{})
	go func() { s.List(context.Background(), ""); close(done) }()
	<-started
	s.Sync([]Account{{"a", srv.URL, "new"}})
	current, _ := s.List(context.Background(), "")
	if len(current) != 1 || current[0].ID != "new-private" {
		t.Fatalf("new response=%+v", current)
	}
	close(release)
	<-done
	current, _ = s.List(context.Background(), "")
	if len(current) != 1 || current[0].ID != "new-private" {
		t.Fatalf("late response revived stale access: %+v", current)
	}
}

func TestCancelledDiscovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Fetch(ctx, nil, Account{"a", BaseURL, "key"})
	if err != context.Canceled {
		t.Fatalf("err=%v", err)
	}
}

func TestConcurrentRefreshCoalesces(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		fmt.Fprint(w, `{"data":[]}`)
	}))
	defer srv.Close()
	s := New(srv.Client())
	s.Sync([]Account{{"a", srv.URL, "key"}})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.List(context.Background(), "a") }()
	}
	<-started
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("duplicate fetches: %d", calls.Load())
	}
}

func TestCancellationWhileWaitingForSlotIsNotSuccess(t *testing.T) {
	s := New(nil)
	s.Sync([]Account{{"a", BaseURL, "key"}})
	for i := 0; i < cap(s.slots); i++ {
		s.slots <- struct{}{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	models, status := s.List(ctx, "a")
	if len(models) != 0 || len(status) != 1 || status[0].Error == "" {
		t.Fatalf("cancelled discovery reported success: %+v %+v", models, status)
	}
}
