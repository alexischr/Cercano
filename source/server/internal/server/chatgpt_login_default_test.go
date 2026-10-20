package server

import (
	"context"
	"fmt"
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

func TestChatGPTLoginActivatesWithStaleBackupReferences(t *testing.T) {
	s, router := newTestServer()
	c := s.cfgSvc.Get()
	c.SetPrimaryBackups([]string{"deleted-chatgpt-2", "deleted-claude"})
	s.cfgSvc.Set(c)
	flow := chatgptLoginFixture(t, s, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"models":[]}`))
	})
	stream := &chatgptLoginTestStream{ctx: t.Context()}
	if err := s.runChatGPTLogin(&proto.StartChatGPTLoginRequest{ProfileName: "chatgpt", SetActive: true}, stream, false, flow); err != nil {
		t.Fatal(err)
	}
	last := stream.events[len(stream.events)-1]
	if !last.Done || !last.Ok {
		t.Fatalf("browser-approved login failed activation: %+v", last)
	}
	if s.cfgSvc.Get().ActiveCloudProfile != "chatgpt" || router.last == nil {
		t.Fatal("signed-in provider was not activated")
	}
}

func TestChatGPTLoginWithoutSetupMakesMainProviderAvailable(t *testing.T) {
	for _, reauthenticate := range []bool{false, true} {
		t.Run(fmt.Sprint("reauth=", reauthenticate), func(t *testing.T) {
			s, _ := newTestServer()
			c := config.Defaults()
			c.LocusMode = "cloud_primary"
			if reauthenticate {
				c.CloudProfiles = []config.CloudProfile{{Name: "chatgpt", Flavor: "responses", Route: "chatgpt"}}
				c.ActiveCloudProfile = "chatgpt"
			}
			s.cfgSvc.Set(c)
			s.providerSvc.InstallAbsentCloud("setup was interrupted")
			flow := chatgptLoginFixture(t, s, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"models":[]}`)) })
			stream := &chatgptLoginTestStream{ctx: t.Context()}
			// No set_active flag: clients outside the wizard must work too.
			if err := s.runChatGPTLogin(&proto.StartChatGPTLoginRequest{ProfileName: "chatgpt"}, stream, reauthenticate, flow); err != nil {
				t.Fatal(err)
			}
			last := stream.events[len(stream.events)-1]
			if !last.Ok {
				t.Fatalf("login failed: %+v", last)
			}
			provider, cloud, _, err := s.providerSvc.Main()
			if err != nil || provider == nil || !cloud {
				t.Fatalf("prompt unavailable after login: cloud=%v err=%v", cloud, err)
			}
		})
	}
}

func TestSavingFirstCloudKeyWithoutSetupMakesMainProviderAvailable(t *testing.T) {
	s, _ := newTestServer()
	c := config.Defaults()
	c.LocusMode = "cloud_primary"
	c.CloudProfiles = []config.CloudProfile{{Name: "first", Flavor: "responses"}}
	s.cfgSvc.Set(c)
	s.providerSvc.InstallAbsentCloud("setup was interrupted")
	result, err := s.SetCloudProfileKey(t.Context(), &proto.SetCloudProfileKeyRequest{Name: "first", ApiKey: "fixture"})
	if err != nil || !result.Ok {
		t.Fatalf("save: result=%+v err=%v", result, err)
	}
	provider, cloud, _, err := s.providerSvc.Main()
	if err != nil || provider == nil || !cloud {
		t.Fatalf("prompt unavailable after key save: cloud=%v err=%v", cloud, err)
	}
}
