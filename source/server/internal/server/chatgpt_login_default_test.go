package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cercano/source/server/internal/chatgptauth"
	"cercano/source/server/pkg/config"
	"cercano/source/server/pkg/proto"
)

type chatgptLoginTestStream struct {
	proto.Agent_StartChatGPTLoginServer
	ctx    context.Context
	events []*proto.StartChatGPTLoginEvent
}

func (s *chatgptLoginTestStream) Context() context.Context { return s.ctx }
func (s *chatgptLoginTestStream) Send(e *proto.StartChatGPTLoginEvent) error {
	s.events = append(s.events, e)
	return nil
}

// chatgptLoginFixture serves OpenAI's device-authorization flow and the codex
// /models catalog from one test server. catalog is the /models handler.
func chatgptLoginFixture(t *testing.T, s *Server, catalog http.HandlerFunc) chatgptauth.Flow {
	t.Helper()
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			w.Write([]byte(`{"device_auth_id":"device","user_code":"user","interval":"1"}`))
		case "/api/accounts/deviceauth/token":
			w.Write([]byte(`{"authorization_code":"code","code_verifier":"verifier"}`))
		case "/models":
			catalog(w, r)
		default:
			w.Write([]byte(`{"access_token":"synthetic-access","refresh_token":"synthetic-refresh","expires_in":3600}`))
		}
	}))
	t.Cleanup(endpoint.Close)
	s.chatgptCatalogBase = endpoint.URL
	return chatgptauth.Flow{Issuer: endpoint.URL}
}

func runTestChatGPTLogin(t *testing.T, s *Server, flow chatgptauth.Flow) config.CloudProfile {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream := &chatgptLoginTestStream{ctx: ctx}
	if err := s.runChatGPTLogin(&proto.StartChatGPTLoginRequest{}, stream, false, flow); err != nil {
		t.Fatal(err)
	}
	last := stream.events[len(stream.events)-1]
	if !last.Done || !last.Ok {
		t.Fatalf("login did not succeed: %+v", last)
	}
	for _, p := range s.cfgSvc.Get().CloudProfiles {
		if p.Name == "chatgpt" {
			return p
		}
	}
	t.Fatal("login created no chatgpt profile")
	return config.CloudProfile{}
}

func TestChatGPTLoginDefaultsEveryTierToLatestCatalogModel(t *testing.T) {
	s, _ := newTestServer()
	var sawAuth string
	flow := chatgptLoginFixture(t, s, func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"models":[
			{"slug":"gpt-6-astra","display_name":"GPT-6-Astra","visibility":"list","priority":2},
			{"slug":"internal-preview","display_name":"preview","visibility":"hide","priority":0},
			{"slug":"gpt-6.1-sol","display_name":"GPT-6.1-Sol","visibility":"list","priority":1}
		]}`))
	})

	p := runTestChatGPTLogin(t, s, flow)

	if sawAuth != "Bearer synthetic-access" {
		t.Errorf("catalog auth: got %q want the freshly issued bearer", sawAuth)
	}
	for _, tier := range []config.CostTier{config.CostEconomy, config.CostStandard, config.CostPremium} {
		if got := p.TierOverrides[tier]; got != "gpt-6.1-sol" {
			t.Errorf("tier %s: got %q want latest catalog model gpt-6.1-sol", tier, got)
		}
	}
}

func TestChatGPTLoginKeepsBakedDefaultWhenCatalogFails(t *testing.T) {
	s, _ := newTestServer()
	var asked bool
	flow := chatgptLoginFixture(t, s, func(w http.ResponseWriter, r *http.Request) {
		asked = true
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	p := runTestChatGPTLogin(t, s, flow)

	if !asked {
		t.Fatal("login never asked the catalog, so the fallback path was not exercised")
	}
	if len(p.TierOverrides) != 0 {
		t.Errorf("catalog failure wrote tier overrides: %v", p.TierOverrides)
	}
}
