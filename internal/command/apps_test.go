package command

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Deplexo/cli/internal/api"
	"github.com/Deplexo/cli/internal/auth"
	"github.com/Deplexo/cli/internal/config"
	"github.com/Deplexo/cli/internal/credentials"
)

func TestListAppsFormatsOnlyAppInventory(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		var out, stderr bytes.Buffer
		options := Options{Out: &out, Err: &stderr, ConfigDir: func() (string, error) { return t.TempDir(), nil },
			LookupEnv: func(key string) (string, bool) {
				if key == "DEPLEXO_TOKEN" {
					return "test-access", true
				}
				return "", false
			}, Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/user/api/v1/me" || r.Method != "GET" {
					t.Errorf("unexpected list request: %s %s", r.Method, r.URL)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"apps":[{"id":"11111111-1111-4111-8111-111111111111","name":"test\u001b[31m","status":"running"}],"user":{"email":"private@example.com"}}`))}, nil
			})}
		args := []string{"apps", "list", "--color", "always"}
		if jsonOutput {
			args = append(args, "--json")
		}
		if code := Execute(context.Background(), options, args); code != 0 || stderr.Len() != 0 || strings.Contains(out.String(), "private@example.com") {
			t.Fatalf("list: code=%d out=%q err=%q", code, out.String(), stderr.String())
		}
		if jsonOutput {
			var result api.Apps
			if err := json.Unmarshal(out.Bytes(), &result); err != nil || len(result.Apps) != 1 || result.Apps[0].Name != "test\x1b[31m" {
				t.Fatalf("JSON contract changed: %s %v", out.String(), err)
			}
		} else if !strings.Contains(out.String(), "NAME") || !strings.Contains(out.String(), `test\u001b[31m`) || !strings.Contains(out.String(), "\x1b[32mrunning") {
			t.Fatalf("human formatting: %q", out.String())
		}
	}
}

func TestDeployUsesLinkedAppAndReturnsDeploymentIdentity(t *testing.T) {
	dir := t.TempDir()
	id := "11111111-1111-4111-8111-111111111111"
	if err := config.Link(dir, id); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	calls := 0
	options := Options{Out: &out, Err: &stderr, ConfigDir: func() (string, error) { return t.TempDir(), nil }, WorkingDir: func() (string, error) { return dir, nil },
		LookupEnv: func(key string) (string, bool) {
			if key == "DEPLEXO_TOKEN" {
				return "test-access", true
			}
			return "", false
		}, Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.Method != "POST" || r.URL.Path != "/user/api/v1/apps/"+id+"/restart" {
				t.Errorf("unexpected deploy request: %s %s", r.Method, r.URL)
			}
			return &http.Response{StatusCode: 202, Body: io.NopCloser(strings.NewReader(`{"appId":"` + id + `","deploymentId":"22222222-2222-4222-8222-222222222222","jobId":42,"status":"queued"}`))}, nil
		})}
	if code := Execute(context.Background(), options, []string{"deploy", "--json", "--color", "always"}); code != 0 || calls != 1 || stderr.Len() != 0 {
		t.Fatalf("deploy: code=%d calls=%d err=%q", code, calls, stderr.String())
	}
	var result api.Redeployment
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.DeploymentID != "22222222-2222-4222-8222-222222222222" || result.Status != "queued" {
		t.Fatalf("deployment identity: %s %v", out.String(), err)
	}
	calls = 0
	if code := Execute(context.Background(), options, []string{"deploy", "."}); code != 2 || calls != 0 {
		t.Fatal("unsupported source path triggered a mutation")
	}
}

func TestDeployChecksStoredScopeBeforeMutation(t *testing.T) {
	var out, stderr bytes.Buffer
	dir := t.TempDir()
	options := Options{Out: &out, Err: &stderr, ConfigDir: func() (string, error) { return dir, nil },
		LookupEnv: func(string) (string, bool) { return "", false },
		Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			t.Fatal("missing scope reached the API")
			return nil, nil
		})}
	_, a := newRoot(options)
	a.insecure = true
	m, err := a.manager()
	if err != nil {
		t.Fatal(err)
	}
	err = m.Store.WithLock(context.Background(), func(v credentials.Vault) error {
		return v.Save(credentials.Session{Version: 1, Origin: api.DefaultOrigin, Profile: "default", AccountID: "test-account", AccessToken: "test-access", RefreshToken: "test-refresh", Scopes: []string{"profile:read", "app:deploy"}, ExpiresAt: time.Now().Add(time.Hour)})
	})
	if err != nil {
		t.Fatal(err)
	}
	if code := Execute(context.Background(), options, []string{"deploy", "--app", "11111111-1111-4111-8111-111111111111", "--insecure-storage"}); code != 4 || !strings.Contains(stderr.String(), "app:restart") {
		t.Fatalf("scope failure: %d %s", code, stderr.String())
	}
	scopes, err := auth.Scopes("", false)
	if err != nil || !strings.Contains(strings.Join(scopes, " "), "app:restart") {
		t.Fatal("new sign-ins cannot redeploy")
	}
	scopes, err = auth.Scopes("", true)
	if err != nil || strings.Contains(strings.Join(scopes, " "), "app:restart") {
		t.Fatal("read-only sign-in requests mutation permission")
	}
}

func TestStyledHelpKeepsCompletionAndJSONPlain(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"apps", "--help"}, {"version", "--json"}, {"completion", "bash"}, {"completion", "powershell"}, {"deploy", "--help"}} {
		var out, stderr bytes.Buffer
		options := Options{Out: &out, Err: &stderr, LookupEnv: func(string) (string, bool) { return "", false }, ConfigDir: func() (string, error) { t.Fatal("offline command used configuration"); return "", nil }}
		if code := Execute(context.Background(), options, append(args, "--color", "always")); code != 0 {
			t.Fatalf("%v: %d %s", args, code, stderr.String())
		}
		wantColor := args[len(args)-1] == "--help"
		if strings.Contains(out.String(), "\x1b[") != wantColor {
			t.Fatalf("color leaked or missing: %v %q", args, out.String())
		}
	}
}
