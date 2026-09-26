package auth

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Deplexo/cli/internal/api"
	"github.com/Deplexo/cli/internal/credentials"
	"github.com/Deplexo/cli/internal/output"
)

type processTransport struct {
	base  http.RoundTripper
	crash bool
}

func (p processTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if p.crash && r.URL.Path == "/token" {
		os.Exit(23)
	}
	return p.base.RoundTrip(r)
}

func TestRefreshProcessHelper(t *testing.T) {
	directory := os.Getenv("DEPLEXO_TEST_REFRESH_DIR")
	if directory == "" {
		return
	}
	certificate, err := base64.StdEncoding.DecodeString(os.Getenv("DEPLEXO_TEST_CERT"))
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(certificate)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
	client, err := api.New(os.Getenv("DEPLEXO_TEST_ORIGIN"), processTransport{base: transport, crash: os.Getenv("DEPLEXO_TEST_REFRESH_CRASH") == "1"})
	if err != nil {
		t.Fatal(err)
	}
	store := &credentials.Store{Directory: directory, Origin: client.Origin(), Profile: "default", Insecure: true}
	m := &Manager{API: client, Store: store, Profile: "default"}
	if _, err := m.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTwoProcessRefreshAndCrashMarker(t *testing.T) {
	var exchanges atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := "https://" + r.Host
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(api.Discovery{Issuer: origin, DeviceEndpoint: origin + "/device", TokenEndpoint: origin + "/token", RevocationEndpoint: origin + "/revoke"})
		case "/token":
			exchanges.Add(1)
			writeTokens(w)
		case "/user/api/v1/profile":
			_, _ = io.WriteString(w, `{"id":"account-one","email":"alex@example.com"}`)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	store := &credentials.Store{Directory: filepath.Join(t.TempDir(), "credentials"), Origin: server.URL, Profile: "default", Insecure: true}
	seed := func() {
		t.Helper()
		if err := store.WithLock(context.Background(), func(v credentials.Vault) error { return v.Save(validSession(server.URL)) }); err != nil {
			t.Fatal(err)
		}
	}
	seed()
	process := func(crash bool) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRefreshProcessHelper$")
		cmd.Env = append(os.Environ(), "DEPLEXO_TEST_REFRESH_DIR="+store.Directory, "DEPLEXO_TEST_ORIGIN="+server.URL, "DEPLEXO_TEST_CERT="+base64.StdEncoding.EncodeToString(server.Certificate().Raw))
		if crash {
			cmd.Env = append(cmd.Env, "DEPLEXO_TEST_REFRESH_CRASH=1")
		}
		return cmd
	}
	first, second := process(false), process(false)
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	if err := second.Start(); err != nil {
		t.Fatal(err)
	}
	if err := first.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := second.Wait(); err != nil {
		t.Fatal(err)
	}
	if exchanges.Load() != 1 {
		t.Fatalf("two processes sent %d refreshes", exchanges.Load())
	}
	seed()
	if err := process(true).Run(); err == nil {
		t.Fatal("helper did not crash")
	}
	client, err := api.New(server.URL, server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{API: client, Store: store, Profile: "default"}
	if _, err := m.Token(context.Background()); output.ExitCode(err) != 3 {
		t.Fatalf("crash marker ignored: %v", err)
	}
	if exchanges.Load() != 1 {
		t.Fatal("replayed a refresh after process crash")
	}
}

type memoryStore struct {
	mu       sync.Mutex
	s        credentials.Session
	missing  bool
	saves    int
	failSave int
	deleted  bool
	loadErr  error
}

func (s *memoryStore) WithLock(ctx context.Context, fn func(credentials.Vault) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(s)
}
func (s *memoryStore) Load() (credentials.Session, error) {
	if s.loadErr != nil {
		return s.s, s.loadErr
	}
	if s.missing {
		return s.s, credentials.ErrNotFound
	}
	return s.s, nil
}
func (s *memoryStore) Save(value credentials.Session) error {
	s.saves++
	if s.saves == s.failSave {
		return errors.New("test persistence failure")
	}
	s.s = value
	s.missing = false
	return nil
}
func (s *memoryStore) Delete() error { s.deleted = true; s.missing = true; return nil }

func managerFor(t *testing.T, store Store, handler http.HandlerFunc) *Manager {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := "https://" + r.Host
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			_ = json.NewEncoder(w).Encode(api.Discovery{Issuer: origin, DeviceEndpoint: origin + "/device", TokenEndpoint: origin + "/token", RevocationEndpoint: origin + "/revoke", Scopes: []string{"profile:read", "app:read", "app:deploy", "logs:read"}})
			return
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	client, err := api.New(server.URL, server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	return &Manager{API: client, Store: store, Profile: "default", wait: func(ctx context.Context, d time.Duration) error { return ctx.Err() }}
}

func validSession(origin string) credentials.Session {
	return credentials.Session{Version: 1, Origin: origin, Profile: "default", AccountID: "account-one", Email: "alex@example.com", AccessToken: "test-old-access", RefreshToken: "test-old-refresh", Scopes: []string{"profile:read"}, ExpiresAt: time.Now().Add(-time.Minute)}
}
func writeTokens(w http.ResponseWriter) {
	_, _ = io.WriteString(w, `{"access_token":"test-new-access","refresh_token":"test-new-refresh","token_type":"Bearer","expires_in":900,"scope":"profile:read"}`)
}

func TestInjectedTokenNeverTouchesStore(t *testing.T) {
	m := &Manager{TokenEnvSet: true, TokenEnv: "test-injected"}
	got, err := m.Token(context.Background(), "profile:read")
	if err != nil || got != "test-injected" {
		t.Fatalf("token %q %v", got, err)
	}
	m.TokenEnv = ""
	if _, err := m.Token(context.Background()); output.ExitCode(err) != 3 {
		t.Fatalf("empty injection %v", err)
	}
}

func TestRefreshPendingBeforeExchangeAndConcurrentCalls(t *testing.T) {
	store := &memoryStore{}
	var exchanges atomic.Int32
	m := managerFor(t, store, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			exchanges.Add(1)
			if !store.s.Pending || store.s.RefreshToken != "" || store.s.AccessToken != "" {
				t.Error("refresh sent before old credential was invalidated")
			}
			writeTokens(w)
		case "/user/api/v1/profile":
			_, _ = io.WriteString(w, `{"id":"account-one","email":"alex@example.com"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	store.s = validSession(m.API.Origin())
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			token, err := m.Token(context.Background(), "profile:read")
			if err != nil || token != "test-new-access" {
				t.Errorf("token %q %v", token, err)
			}
		})
	}
	wg.Wait()
	if exchanges.Load() != 1 || store.s.Pending || store.s.RefreshToken != "test-new-refresh" {
		t.Fatalf("refresh count=%d pending=%v", exchanges.Load(), store.s.Pending)
	}
}

func TestRefreshFailuresCannotReplay(t *testing.T) {
	for _, mode := range []string{"ambiguous", "wrong-account", "save-failure", "missing-scope"} {
		t.Run(mode, func(t *testing.T) {
			store := &memoryStore{}
			var exchanges, revocations int
			m := managerFor(t, store, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/token":
					exchanges++
					if mode == "ambiguous" {
						w.WriteHeader(503)
						return
					}
					if mode == "missing-scope" {
						_, _ = io.WriteString(w, `{"access_token":"test-new-access","refresh_token":"test-new-refresh","token_type":"Bearer","expires_in":900,"scope":"app:read"}`)
					} else {
						writeTokens(w)
					}
				case "/user/api/v1/profile":
					id := "account-one"
					if mode == "wrong-account" {
						id = "another-account"
					}
					_ = json.NewEncoder(w).Encode(api.Profile{ID: id, Email: "alex@example.com"})
				case "/revoke":
					revocations++
					w.WriteHeader(200)
				}
			})
			store.s = validSession(m.API.Origin())
			if mode == "save-failure" {
				store.failSave = 2
			}
			if _, err := m.Token(context.Background()); err == nil {
				t.Fatal("accepted failed refresh")
			}
			if _, err := m.Token(context.Background()); output.ExitCode(err) != 3 {
				t.Fatalf("pending state error: %v", err)
			}
			if exchanges != 1 || !store.s.Pending {
				t.Fatalf("replayed refresh: %d", exchanges)
			}
			if mode != "ambiguous" && revocations != 1 {
				t.Fatal("new credentials not revoked")
			}
		})
	}
}

func TestLoginPersistenceFailureRevokesGrant(t *testing.T) {
	store := &memoryStore{missing: true, failSave: 1}
	var pairing, revoked bool
	m := managerFor(t, store, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device":
			_ = json.NewEncoder(w).Encode(api.Device{DeviceCode: "test-device", UserCode: "ABCD-EFGH", VerificationURI: "https://" + r.Host + "/new-devices", ExpiresIn: 600})
		case "/token":
			if !pairing {
				t.Error("polled before pairing instructions")
			}
			writeTokens(w)
		case "/user/api/v1/profile":
			_, _ = io.WriteString(w, `{"id":"account-one","email":"alex@example.com"}`)
		case "/revoke":
			revoked = true
		}
	})
	_, err := m.Login(context.Background(), []string{"profile:read"}, func(context.Context, api.Device) error { pairing = true; return nil })
	if err == nil || !revoked || !store.missing {
		t.Fatalf("persistence handling: %v revoked=%v missing=%v", err, revoked, store.missing)
	}
}

type timeoutTransport struct{}

func (timeoutTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, testTimeout{} }

type testTimeout struct{}

func (testTimeout) Error() string   { return "timeout" }
func (testTimeout) Timeout() bool   { return true }
func (testTimeout) Temporary() bool { return true }

func TestPollingIntervals(t *testing.T) {
	var polls int
	m := managerFor(t, nil, func(w http.ResponseWriter, r *http.Request) {
		polls++
		if polls == 4 {
			writeTokens(w)
			return
		}
		w.Header().Set("Retry-After", map[int]string{1: "1", 2: "12", 3: "1"}[polls])
		w.WriteHeader(400)
		code := "authorization_pending"
		if polls == 2 || polls == 3 {
			code = "slow_down"
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
	})
	var delays []time.Duration
	m.wait = func(ctx context.Context, d time.Duration) error { delays = append(delays, d); return nil }
	_, err := m.poll(context.Background(), api.Discovery{TokenEndpoint: m.API.Origin() + "/token"}, api.Device{DeviceCode: "test-device"})
	if err != nil || !reflect.DeepEqual(delays, []time.Duration{5 * time.Second, 5 * time.Second, 12 * time.Second, 15 * time.Second}) {
		t.Fatalf("delays=%v err=%v", delays, err)
	}
	client, _ := api.New(api.DefaultOrigin, timeoutTransport{})
	m.API = client
	delays = nil
	m.wait = func(ctx context.Context, d time.Duration) error {
		delays = append(delays, d)
		if len(delays) == 2 {
			return context.Canceled
		}
		return nil
	}
	_, err = m.poll(context.Background(), api.Discovery{TokenEndpoint: api.DefaultOrigin + "/token"}, api.Device{DeviceCode: "test-device", Interval: 600})
	if !errors.Is(err, context.Canceled) || delays[1] < delays[0] {
		t.Fatalf("timeout shortened interval: %v %v", delays, err)
	}
}

func TestLogoutClearsAfterRemoteFailureAndMalformedState(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		store := &memoryStore{}
		m := managerFor(t, store, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
		store.s = validSession(m.API.Origin())
		if malformed {
			store.loadErr = credentials.ErrMalformed
		}
		err := m.Logout(context.Background())
		if err == nil || !store.deleted {
			t.Fatalf("local state not cleared: %v", err)
		}
	}
}

func TestCancelledLogoutClearsLocalStateAndPreservesWarning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &memoryStore{}
	m := managerFor(t, store, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/revoke" {
			_, _ = io.Copy(io.Discard, r.Body)
			cancel()
			<-r.Context().Done()
		}
	})
	store.s = validSession(m.API.Origin())
	err := m.Logout(ctx)
	if !store.deleted || !errors.Is(err, context.Canceled) || output.ExitCode(err) != 130 {
		t.Fatalf("cancelled logout: %v deleted=%v", err, store.deleted)
	}
	for _, jsonOutput := range []bool{false, true} {
		var out bytes.Buffer
		output.Report(&out, err, jsonOutput)
		if !strings.Contains(out.String(), "local sign-in removed") || !strings.Contains(out.String(), "server session may remain") {
			t.Fatalf("lost sign-out warning: %s", out.String())
		}
	}
}

func TestScopeAndOriginBinding(t *testing.T) {
	if _, err := Scopes("app:read", false); output.ExitCode(err) != 2 {
		t.Fatal("profile scope not required")
	}
	if _, err := Scopes("profile:read", true); err == nil {
		t.Fatal("conflicting scope flags accepted")
	}
	store := &memoryStore{}
	m := managerFor(t, store, func(w http.ResponseWriter, r *http.Request) { t.Fatal("unexpected HTTP request") })
	store.s = validSession("https://other.example")
	if _, err := m.Token(context.Background()); output.ExitCode(err) != 3 {
		t.Fatalf("origin mismatch accepted: %v", err)
	}
	store.s = validSession(m.API.Origin())
	store.s.ExpiresAt = time.Now().Add(time.Hour)
	if _, err := m.Token(context.Background(), "app:deploy"); output.ExitCode(err) != 4 || strings.Contains(err.Error(), store.s.AccessToken) {
		t.Fatalf("scope error: %v", err)
	}
}

func TestLogoutDoesNotSendCrossOriginCredential(t *testing.T) {
	store := &memoryStore{}
	var calls atomic.Int32
	m := managerFor(t, store, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		t.Error("cross-origin credential was sent")
	})
	store.s = validSession("https://another.example")
	if err := m.Logout(context.Background()); err == nil || !store.deleted || calls.Load() != 0 {
		t.Fatalf("cross-origin logout: %v deleted=%v", err, store.deleted)
	}
}

func TestLoginDoesNotRevokeCrossOriginPreviousSession(t *testing.T) {
	store := &memoryStore{}
	var revocations atomic.Int32
	m := managerFor(t, store, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device":
			_ = json.NewEncoder(w).Encode(api.Device{DeviceCode: "test-device", UserCode: "ABCD-EFGH", VerificationURI: "https://" + r.Host + "/new-devices", ExpiresIn: 600})
		case "/token":
			writeTokens(w)
		case "/user/api/v1/profile":
			_, _ = io.WriteString(w, `{"id":"account-one","email":"alex@example.com"}`)
		case "/revoke":
			revocations.Add(1)
		}
	})
	store.s = validSession("https://another.example")
	_, err := m.Login(context.Background(), []string{"profile:read"}, func(context.Context, api.Device) error { return nil })
	if err == nil || revocations.Load() != 0 || store.s.Origin != m.API.Origin() {
		t.Fatalf("cross-origin replacement: %v revocations=%d", err, revocations.Load())
	}
}
