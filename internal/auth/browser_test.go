package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Deplexo/cli/internal/api"
	"golang.org/x/oauth2"
)

func TestBrowserCallbackBindingAndSingleUse(t *testing.T) {
	const host = "127.0.0.1:1234"
	const issuer = "https://deplexo.test"
	result := make(chan browserResult, 1)
	handler := browserCallback(host, "expected", issuer, result)
	for _, raw := range []string{
		"?code=code&state=wrong",
		"?code=code&state=expected&state=expected",
		"?code=code&state=expected&iss=https://other.test",
		"?code=code&error=denied&state=expected",
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "http://"+host+"/oauth/callback"+raw, nil))
		if w.Code != 400 {
			t.Fatalf("invalid callback status %d", w.Code)
		}
	}
	request := httptest.NewRequest("GET", "http://"+host+"/oauth/callback?code=code&state=expected&iss="+url.QueryEscape(issuer), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	if w.Code != 200 {
		t.Fatalf("valid callback: %d", w.Code)
	}
	if got := <-result; got.code != "code" || got.err != nil {
		t.Fatalf("result: %+v", got)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	if w.Code != 409 {
		t.Fatalf("accepted duplicate after channel drained: %d", w.Code)
	}
}

func TestBrowserLoginPKCEAndCredentialLifecycle(t *testing.T) {
	for _, mode := range []string{"success", "save failure", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			store := &memoryStore{missing: true}
			if mode == "save failure" {
				store.failSave = 1
			}
			var challenge, redirect string
			var revoked bool
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				origin := "https://" + r.Host
				switch r.URL.Path {
				case "/.well-known/oauth-authorization-server":
					_ = json.NewEncoder(w).Encode(api.Discovery{Issuer: origin, AuthorizationEndpoint: origin + "/authorize", DeviceEndpoint: origin + "/device", TokenEndpoint: origin + "/token", RevocationEndpoint: origin + "/revoke", CodeChallengeMethods: []string{"S256"}, Scopes: []string{"profile:read"}})
				case "/token":
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "test-code" || r.Form.Get("client_id") != api.ClientID || r.Form.Get("resource") != origin+"/user/api/v1" || r.Form.Get("redirect_uri") != redirect || oauth2.S256ChallengeFromVerifier(r.Form.Get("code_verifier")) != challenge {
						t.Error("code exchange lost PKCE/client/resource binding")
					}
					writeTokens(w)
				case "/user/api/v1/profile":
					_, _ = io.WriteString(w, `{"id":"account-one","email":"alex@example.com"}`)
				case "/revoke":
					revoked = true
				default:
					t.Errorf("unexpected endpoint %s", r.URL.Path)
				}
			}))
			defer server.Close()
			client, err := api.New(server.URL, server.Client().Transport)
			if err != nil {
				t.Fatal(err)
			}
			m := &Manager{API: client, Store: store, Profile: "default"}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			profile, err := m.LoginBrowser(ctx, []string{"profile:read"}, func(_ context.Context, raw string) error {
				u, e := url.Parse(raw)
				if e != nil {
					return e
				}
				q := u.Query()
				challenge = q.Get("code_challenge")
				redirect = q.Get("redirect_uri")
				if q.Get("code_challenge_method") != "S256" || len(q.Get("state")) < 43 {
					t.Error("weak authorization request")
				}
				if mode == "cancel" {
					cancel()
					return nil
				}
				callback := redirect + "?code=test-code&state=" + url.QueryEscape(q.Get("state")) + "&iss=" + url.QueryEscape(server.URL)
				response, e := http.Get(callback)
				if e != nil {
					return e
				}
				defer func() {
					if err := response.Body.Close(); err != nil {
						t.Error(err)
					}
				}()
				if response.StatusCode != 200 {
					t.Errorf("callback status %d", response.StatusCode)
				}
				return nil
			})
			if mode == "success" {
				if err != nil || profile.ID != "account-one" || store.missing || store.s.RefreshToken != "test-new-refresh" {
					t.Fatalf("login failed: %v", err)
				}
			}
			if mode == "save failure" && (err == nil || !revoked || !store.missing) {
				t.Fatalf("credential cleanup: %v revoked=%v", err, revoked)
			}
			if mode == "cancel" && (!errors.Is(err, context.Canceled) || !store.missing) {
				t.Fatalf("cancellation: %v", err)
			}
			response, e := http.Get(redirect)
			if e == nil {
				if err := response.Body.Close(); err != nil {
					t.Error(err)
				}
				t.Fatal("callback listener survived login")
			}
		})
	}
}
