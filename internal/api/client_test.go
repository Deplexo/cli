package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	client, err := New(server.URL, server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	return client, server
}

func TestNormalizeOrigin(t *testing.T) {
	for _, raw := range []string{"http://example.com", "https://user@example.com", "https://example.com/path", "https://example.com?", "https://example.com?q=x", "https://example.com/#fragment", "https:///"} {
		if _, err := NormalizeOrigin(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	got, err := NormalizeOrigin("https://EXAMPLE.com:443/")
	if err != nil || got != "https://example.com" {
		t.Fatalf("normalization: %q %v", got, err)
	}
}

func TestRedirectsNeverForwardCredentials(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			for _, sameOrigin := range []bool{true, false} {
				t.Run(http.StatusText(status)+method+map[bool]string{true: "same", false: "other"}[sameOrigin], func(t *testing.T) {
					var reached atomic.Int32
					target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Add(1) }))
					defer target.Close()
					client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path == "/target" {
							reached.Add(1)
							return
						}
						location := target.URL
						if sameOrigin {
							location = "/target"
						}
						w.Header().Set("Location", location)
						w.WriteHeader(status)
					})
					err := client.request(context.Background(), method, client.origin+"/test", "test-access", "", nil, nil)
					if err == nil || reached.Load() != 0 {
						t.Fatalf("redirect followed: %v calls=%d", err, reached.Load())
					}
				})
			}
		}
	}
}

func TestBoundedDecodeAndRedaction(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"oversized", strings.Repeat("x", MaxResponseBytes+1), 200},
		{"malformed", `{"email":"test-secret"`, 200},
		{"server-error", `{"error":{"code":"test-secret","message":"test-secret"}}`, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			_, err := client.Profile(context.Background(), "test-secret")
			if err == nil || strings.Contains(err.Error(), "test-secret") {
				t.Fatalf("unsafe error: %v", err)
			}
		})
	}
}

func TestDiscoveryAndPairingValidation(t *testing.T) {
	for _, target := range []string{"https://evil.example/oauth/token", "http://example.com/oauth/token", "https://user@example.com/oauth/token", "https://example.com/oauth/token?secret=x"} {
		client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			origin := "https://" + r.Host
			_ = json.NewEncoder(w).Encode(Discovery{Issuer: origin, DeviceEndpoint: origin + "/device", TokenEndpoint: target, RevocationEndpoint: origin + "/revoke"})
		})
		if _, err := client.Discover(context.Background()); err == nil {
			t.Errorf("accepted %s", target)
		}
	}
	for _, suffix := range []string{"?device_code=test-secret", "?user_code=wrong", "?user_code=ABCD-EFGH&token=test-secret"} {
		client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			origin := "https://" + r.Host
			_ = json.NewEncoder(w).Encode(Device{DeviceCode: "test-device", UserCode: "ABCD-EFGH", VerificationURI: origin + "/new-devices", VerificationURIComplete: origin + "/new-devices" + suffix, ExpiresIn: 600})
		})
		if _, err := client.Authorize(context.Background(), Discovery{DeviceEndpoint: client.origin + "/device"}, "profile:read"); err == nil {
			t.Errorf("accepted %s", suffix)
		}
	}
}

func TestOAuthFormsAndProfileContract(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if r.Header.Get("Authorization") != "" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.URL.RawQuery != "" {
				t.Error("unsafe OAuth request")
			}
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("client_id") != ClientID || r.Form.Get("refresh_token") != "test-refresh" || r.Form.Get("grant_type") != "refresh_token" {
				t.Errorf("wrong form: %v", r.Form)
			}
			_, _ = io.WriteString(w, `{"access_token":"test-access","refresh_token":"replacement","token_type":"Bearer","expires_in":900,"scope":"profile:read"}`)
		case "/user/api/v1/profile":
			if r.Header.Get("Authorization") != "Bearer test-access" {
				t.Error("missing credential")
			}
			_, _ = io.WriteString(w, `{"id":"11111111-1111-4111-8111-111111111111","email":"alex@example.com","name":"Alex","avatar_url":""}`)
		}
	})
	tokens, err := client.Exchange(context.Background(), Discovery{TokenEndpoint: client.origin + "/token"}, "refresh_token", "test-refresh")
	if err != nil {
		t.Fatal(err)
	}
	p, err := client.Profile(context.Background(), tokens.AccessToken)
	if err != nil || p.Email != "alex@example.com" {
		t.Fatalf("profile: %+v %v", p, err)
	}
}

func TestCancellationAndMutationNoRetry(t *testing.T) {
	var calls atomic.Int32
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(503) })
	_, err := client.CreateApp(context.Background(), "test-access", CreateApp{Name: "example", RepoURL: "https://github.com/example/app"})
	if err == nil || !strings.Contains(err.Error(), "outcome is unknown") || calls.Load() != 1 {
		t.Fatalf("mutation result: %v calls=%d", err, calls.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Profile(ctx, "test-access")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestRetryAfter(t *testing.T) {
	if retryAfter("12") != 12*time.Second || retryAfter("-1") != 0 || retryAfter("garbage") != 0 {
		t.Fatal("invalid Retry-After handling")
	}
	if d := retryAfter(time.Now().Add(time.Minute).UTC().Format(http.TimeFormat)); d < 58*time.Second || d > time.Minute {
		t.Fatalf("date duration %v", d)
	}
}

func TestAppActionsAndPaginationContracts(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cancel"):
			_, _ = io.WriteString(w, `{"ok":true,"cancelledJobId":42}`)
		case strings.HasSuffix(r.URL.Path, "/start"):
			_, _ = io.WriteString(w, `{"appId":"`+id+`","jobId":43,"status":"pending"}`)
		case strings.HasSuffix(r.URL.Path, "/stop"):
			_, _ = io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/deployments"):
			_, _ = io.WriteString(w, `{"data":[],"pagination":{"total":51,"limit":50,"offset":0,"hasMore":true}}`)
		}
	})
	cancelled, err := client.CancelDeployment(context.Background(), "test-access", id)
	if err != nil || !cancelled.OK || cancelled.CancelledJobID != 42 {
		t.Fatalf("cancel response: %+v %v", cancelled, err)
	}
	accepted, err := client.AppAction(context.Background(), "test-access", id, "start")
	if err != nil || accepted.JobID != 43 {
		t.Fatalf("start response: %+v %v", accepted, err)
	}
	if _, err := client.AppAction(context.Background(), "test-access", id, "stop"); err == nil {
		t.Fatal("accepted incomplete mutation response")
	}
	page, err := client.Deployments(context.Background(), "test-access", id, 50, 0)
	if err != nil || !page.Pagination.HasMore {
		t.Fatalf("pagination: %+v %v", page, err)
	}
}
