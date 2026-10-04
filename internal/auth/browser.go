package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Deplexo/cli/internal/api"
	"golang.org/x/oauth2"
)

func (m *Manager) LoginBrowser(ctx context.Context, scopes []string, open func(context.Context, string) error) (api.Profile, error) {
	return m.login(ctx, scopes, func(ctx context.Context, d api.Discovery) (api.Tokens, error) {
		if d.AuthorizationEndpoint == "" || !slices.Contains(d.CodeChallengeMethods, "S256") {
			return api.Tokens{}, errors.New("this server does not support browser PKCE login; use deplexo auth login --device")
		}
		ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			listener, err = net.Listen("tcp6", "[::1]:0")
		}
		if err != nil {
			return api.Tokens{}, errors.New("could not open a local callback listener; use deplexo auth login --device")
		}
		verifier, state := oauth2.GenerateVerifier(), oauth2.GenerateVerifier()
		redirect := "http://" + listener.Addr().String() + "/oauth/callback"
		result := make(chan browserResult, 1)
		server := &http.Server{Handler: browserCallback(listener.Addr().String(), state, d.Issuer, result), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 8192}
		var owned sync.WaitGroup
		owned.Go(func() {
			if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				select {
				case result <- browserResult{err: errors.New("the login callback listener stopped")}:
				default:
				}
			}
		})
		defer func() { _ = server.Close(); owned.Wait() }()
		authorize, _ := url.Parse(d.AuthorizationEndpoint)
		query := url.Values{"client_id": {api.ClientID}, "response_type": {"code"}, "redirect_uri": {redirect}, "scope": {strings.Join(scopes, " ")}, "state": {state}, "code_challenge": {oauth2.S256ChallengeFromVerifier(verifier)}, "code_challenge_method": {"S256"}, "resource": {m.API.Origin() + "/user/api/v1"}}
		authorize.RawQuery = query.Encode()
		if err := open(ctx, authorize.String()); err != nil {
			return api.Tokens{}, err
		}
		select {
		case <-ctx.Done():
			return api.Tokens{}, ctx.Err()
		case response := <-result:
			if response.err != nil {
				return api.Tokens{}, response.err
			}
			return m.API.ExchangeCode(ctx, d, response.code, verifier, redirect)
		}
	})
}

type browserResult struct {
	code string
	err  error
}

func browserCallback(host, state, issuer string, result chan<- browserResult) http.Handler {
	var received atomic.Bool
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != http.MethodGet || r.Host != host || r.URL.Path != "/oauth/callback" || len(r.URL.RawQuery) > 4096 {
			http.Error(w, "Invalid login callback.", http.StatusBadRequest)
			return
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			http.Error(w, "Invalid login callback.", http.StatusBadRequest)
			return
		}
		for key, values := range query {
			if len(values) != 1 || !slices.Contains([]string{"code", "state", "error", "error_description", "iss"}, key) {
				http.Error(w, "Invalid login callback.", http.StatusBadRequest)
				return
			}
		}
		if value, present := query["iss"]; present && value[0] != issuer {
			http.Error(w, "Unexpected authorization issuer.", http.StatusBadRequest)
			return
		}
		if subtle.ConstantTimeCompare([]byte(query.Get("state")), []byte(state)) != 1 || (query.Get("code") == "") == (query.Get("error") == "") {
			http.Error(w, "This callback does not match the pending login.", http.StatusBadRequest)
			return
		}
		response := browserResult{code: query.Get("code")}
		if query.Get("error") != "" {
			response.err = errors.New("authorization was not approved; start a new login when ready")
		} else if !api.ValidToken(response.code) {
			http.Error(w, "Invalid authorization code.", http.StatusBadRequest)
			return
		}
		if !received.CompareAndSwap(false, true) {
			http.Error(w, "This authorization was already received.", http.StatusConflict)
			return
		}
		select {
		case result <- response:
			_, _ = w.Write([]byte("Authorization received. Return to your terminal to check the sign-in result."))
		default:
			http.Error(w, "This authorization was already received.", http.StatusConflict)
		}
	})
}
