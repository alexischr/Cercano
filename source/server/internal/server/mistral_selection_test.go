package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"cercano/source/server/internal/secrets"
	"cercano/source/server/pkg/config"
	"cercano/source/server/pkg/proto"
)

func TestMistralSessionSelectionRevalidatesAccount(t *testing.T) {
	var revoked atomic.Bool
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer work-key" {
			t.Error("wrong account credential")
		}
		if revoked.Load() {
			fmt.Fprint(w, `{"data":[]}`)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"exact","capabilities":{"completion_chat":true,"function_calling":true}},{"id":"retired","deprecation":"2000-01-01","capabilities":{"completion_chat":true,"function_calling":true}}]}`)
	}))
	defer api.Close()
	s, _ := newServerWithStore(t)
	cfg := config.Defaults()
	cfg.CloudProfiles = []config.CloudProfile{{Name: "work", Provider: "mistral", Flavor: "chat_completions", BaseURL: api.URL + "/v1"}}
	s.SetConfigPersistence("", cfg)
	keys := secrets.NewMemory()
	if err := keys.Set("work", "work-key"); err != nil {
		t.Fatal(err)
	}
	s.SetSecrets(keys)
	req := &proto.SessionModelRequest{ConversationId: "one", Action: "set", Profile: "work", Model: "exact"}
	if _, err := s.SessionModel(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	req.Model = "retired"
	if _, err := s.SessionModel(t.Context(), req); err == nil {
		t.Fatal("retired model selected")
	}
	req.Model = "foreign-private"
	if _, err := s.SessionModel(t.Context(), req); err == nil {
		t.Fatal("foreign model selected")
	}
	revoked.Store(true)
	req.Model = "exact"
	if _, err := s.SessionModel(t.Context(), req); err == nil {
		t.Fatal("cached availability bypassed revalidation")
	}
}
