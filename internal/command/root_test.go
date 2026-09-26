package command

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Deplexo/cli/internal/api"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOfflineCommands(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"version"}, {"version", "--json"}, {"completion", "bash"}, {"completion", "zsh"}, {"completion", "fish"}, {"completion", "powershell"}, {"auth", "login", "--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out, stderr bytes.Buffer
			options := Options{Out: &out, Err: &stderr, ConfigDir: func() (string, error) { t.Fatal("offline command read configuration"); return "", nil }, Transport: roundTrip(func(*http.Request) (*http.Response, error) { t.Fatal("offline command used network"); return nil, nil })}
			if code := Execute(context.Background(), options, args); code != 0 || out.Len() == 0 || stderr.Len() != 0 {
				t.Fatalf("code=%d output=%q stderr=%q", code, out.String(), stderr.String())
			}
		})
	}
}

func TestPairRequiresEnterBeforeOpeningBrowser(t *testing.T) {
	for _, tc := range []struct {
		name, input                            string
		terminal, noInput, noBrowser, wantOpen bool
	}{
		{"enter", "\n", true, false, false, true},
		{"windows-enter", "\r\n", true, false, false, true},
		{"manual", "n\n", true, false, false, false},
		{"eof", "", true, false, false, false},
		{"redirected", "\n", false, false, false, false},
		{"no-input", "\n", true, true, false, false},
		{"no-browser", "\n", true, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			opened := false
			a := &application{noInput: tc.noInput, options: Options{
				In: strings.NewReader(tc.input), Err: &out, IsTerminal: func() bool { return tc.terminal },
				OpenBrowser: func(_ context.Context, url string) error {
					if !strings.Contains(out.String(), "https://deplexo.com/new-devices") || !strings.Contains(out.String(), "ABCD-EFGH") || !strings.Contains(out.String(), "Please press Enter") {
						t.Fatal("browser opened before pairing instructions and prompt")
					}
					if url != "https://deplexo.com/new-devices" {
						t.Fatal("browser URL changed")
					}
					opened = true
					return nil
				},
			}}
			if err := a.pair(context.Background(), api.Device{VerificationURI: "https://deplexo.com/new-devices", UserCode: "ABCD-EFGH"}, tc.noBrowser); err != nil {
				t.Fatal(err)
			}
			if opened != tc.wantOpen {
				t.Fatalf("opened=%v", opened)
			}
			if (!tc.terminal || tc.noInput || tc.noBrowser) && strings.Contains(out.String(), "Please press Enter") {
				t.Fatal("prompted in manual mode")
			}
		})
	}
}

func TestBrowserPromptCancellation(t *testing.T) {
	reader, writer := io.Pipe()
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		open, err := confirmBrowser(ctx, &promptReader{Reader: reader, started: started})
		if open {
			result <- errors.New("opened browser without Enter")
			return
		}
		result <- err
	}()
	<-started
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("lost cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("prompt blocked cancellation")
	}
}

type promptReader struct {
	io.Reader
	started chan struct{}
}

func (r *promptReader) Read(p []byte) (int, error) {
	select {
	case <-r.started:
	default:
		close(r.started)
	}
	return r.Reader.Read(p)
}

func TestDeploymentDiagnosticsRedactCredential(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	diagnostic := `{"id":"` + id + `","buildLogs":"build test-secret","errorMessage":"failed with test-secret"}`
	for _, tc := range []struct {
		args []string
		body string
	}{
		{[]string{"deployments", "logs", id, "--json"}, diagnostic},
		{[]string{"deployments", "logs", id}, diagnostic},
		{[]string{"deployments", "list", "--app", id, "--json"}, `{"data":[` + diagnostic + `]}`},
	} {
		var out, stderr bytes.Buffer
		options := Options{Out: &out, Err: &stderr, ConfigDir: func() (string, error) { return t.TempDir(), nil }, LookupEnv: func(key string) (string, bool) { return "test-secret", key == "DEPLEXO_TOKEN" }, Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
		})}
		if code := Execute(context.Background(), options, tc.args); code != 0 || !strings.Contains(out.String(), "[REDACTED]") || strings.Contains(out.String()+stderr.String(), "test-secret") {
			t.Fatalf("%v: code=%d output=%q stderr=%q", tc.args, code, out.String(), stderr.String())
		}
	}
}

func TestUsageAndTokenPrecedence(t *testing.T) {
	for _, args := range [][]string{{"unknown"}, {"whoami", "--token", "test-secret"}, {"apps", "stop", "--app", "11111111-1111-4111-8111-111111111111"}, {"apps", "get", "--app", "invalid"}, {"auth", "login", "--scopes", "app:read"}} {
		var out, stderr bytes.Buffer
		if code := Execute(context.Background(), Options{Out: &out, Err: &stderr}, args); code != 2 {
			t.Fatalf("%v code=%d error=%s", args, code, stderr.String())
		}
		if strings.Count(stderr.String(), "Error:") != 1 || out.Len() != 0 {
			t.Fatalf("error output: stdout=%q stderr=%q", out.String(), stderr.String())
		}
	}
	for _, tc := range []struct{ status, want int }{{401, 3}, {403, 4}, {500, 1}} {
		var out, stderr bytes.Buffer
		options := Options{Out: &out, Err: &stderr, ConfigDir: func() (string, error) { return t.TempDir(), nil }, LookupEnv: func(name string) (string, bool) {
			if name == "DEPLEXO_TOKEN" {
				return "test-secret", true
			}
			return "", false
		}, Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Authorization") != "Bearer test-secret" || r.URL.Path != "/user/api/v1/profile" {
				t.Error("wrong credential or route")
			}
			return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"message":"test-secret"}}`))}, nil
		})}
		if code := Execute(context.Background(), options, []string{"whoami", "--json"}); code != tc.want {
			t.Fatalf("status=%d code=%d", tc.status, code)
		}
		if strings.Contains(stderr.String(), "test-secret") || out.Len() != 0 {
			t.Fatal("credential leaked")
		}
	}
}

func TestCancellationExit(t *testing.T) {
	var out, stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	options := Options{Out: &out, Err: &stderr, ConfigDir: func() (string, error) { return t.TempDir(), nil }, LookupEnv: func(key string) (string, bool) { return "test-access", key == "DEPLEXO_TOKEN" }, Transport: roundTrip(func(*http.Request) (*http.Response, error) { return nil, context.Canceled })}
	if code := Execute(ctx, options, []string{"whoami"}); code != 130 {
		t.Fatalf("exit=%d error=%s", code, stderr.String())
	}
}

func TestCreateUsesPublicContractOnce(t *testing.T) {
	var out, stderr bytes.Buffer
	var calls int
	options := Options{Out: &out, Err: &stderr, ConfigDir: func() (string, error) { return t.TempDir(), nil }, LookupEnv: func(key string) (string, bool) { return "test-access", key == "DEPLEXO_TOKEN" }, Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/user/api/v1/deploy" || r.Method != "POST" {
			t.Error("wrong creation contract")
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"repoUrl":"https://github.com/example/app"`) {
			t.Errorf("body=%s", body)
		}
		return nil, errors.New("connection interrupted")
	})}
	code := Execute(context.Background(), options, []string{"apps", "create", "--name", "example", "--repo", "https://github.com/example/app"})
	if code != 1 || calls != 1 || !strings.Contains(stderr.String(), "outcome is unknown") {
		t.Fatalf("code=%d calls=%d err=%s", code, calls, stderr.String())
	}
}

func TestOriginPrecedence(t *testing.T) {
	a := &application{origin: "https://flag.example", profile: "chosen", options: Options{ConfigDir: func() (string, error) { return t.TempDir(), nil }, LookupEnv: func(key string) (string, bool) {
		if key == "DEPLEXO_ORIGIN" {
			return "https://env.example", true
		}
		return "", false
	}}}
	m, err := a.manager()
	if err != nil || m.API.Origin() != "https://flag.example" || m.Profile != "chosen" {
		t.Fatalf("precedence %v", err)
	}
	a.origin = ""
	m, err = a.manager()
	if err != nil || m.API.Origin() != "https://env.example" {
		t.Fatal("environment ignored")
	}
	a.options.LookupEnv = func(string) (string, bool) { return "", false }
	m, err = a.manager()
	if err != nil || m.API.Origin() != api.DefaultOrigin {
		t.Fatal("wrong default")
	}
}
