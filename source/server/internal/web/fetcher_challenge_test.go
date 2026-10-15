package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchJavaScriptChallenge(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		blocked    bool
	}{
		{"reddit challenge", `<html><body><main><svg></svg></main><form><input name="js_challenge" value="1"><input name="jsc_token" value="token"></form></body></html>`, true},
		{"single quotes", `<form><input name='jsc_token'><input name='js_challenge'></form>`, true},
		{"article discussing challenge", `<article><p>Reddit uses js_challenge and jsc_token.</p></article>`, false},
		{"normal article", `<article><p>Hello world</p></article>`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			result, err := NewFetcher().Fetch(srv.URL)
			if tc.blocked {
				if err == nil || !strings.Contains(err.Error(), "JavaScript verification") {
					t.Fatalf("expected verification error, got result=%+v err=%v", result, err)
				}
			} else if err != nil || result.Content == "" {
				t.Fatalf("expected readable content, got result=%+v err=%v", result, err)
			}
		})
	}
}
