package command

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Deplexo/cli/internal/config"
)

func updateOptions(t *testing.T, out, stderr *bytes.Buffer, lookup func(string) (string, bool), transport http.RoundTripper) Options {
	t.Helper()
	dir := t.TempDir()
	return Options{Version: "v0.1.0", In: strings.NewReader("\n"), Out: out, Err: stderr, IsTerminal: func() bool { return true }, IsOutputTerminal: func(io.Writer) bool { return true }, ConfigDir: func() (string, error) { return dir, nil }, LookupEnv: lookup, UpdateTransport: transport, Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"account","email":"test@example.com"}`))}, nil
	})}
}

func TestAutomaticUpdateEligibility(t *testing.T) {
	for _, scenario := range []string{"root-help", "help", "version", "completion", "json", "no-input", "ci", "disabled", "redirected", "noninteractive", "dev"} {
		t.Run(scenario, func(t *testing.T) {
			var out, stderr bytes.Buffer
			opts := updateOptions(t, &out, &stderr, func(key string) (string, bool) {
				if key == "DEPLEXO_TOKEN" {
					return "test-access", true
				}
				if scenario == "ci" && key == "CI" || scenario == "disabled" && key == "DEPLEXO_NO_UPDATE_CHECK" {
					return "1", true
				}
				return "", false
			}, roundTrip(func(*http.Request) (*http.Response, error) {
				t.Fatal("ineligible command checked for updates")
				return nil, nil
			}))
			args := []string{"whoami"}
			switch scenario {
			case "root-help":
				args = nil
			case "help":
				args = []string{"--help"}
			case "version":
				args = []string{"version"}
			case "completion":
				args = []string{"completion", "bash"}
			case "json":
				args = append(args, "--json")
			case "no-input":
				args = append(args, "--no-input")
			case "redirected":
				opts.IsOutputTerminal = func(w io.Writer) bool { return w != &out }
			case "noninteractive":
				opts.IsTerminal = func() bool { return false }
			case "dev":
				opts.Version = "dev"
			}
			if code := Execute(context.Background(), opts, args); code != 0 {
				t.Fatalf("%s: %d %s", scenario, code, stderr.String())
			}
		})
	}
}

func TestUpdateSkipAndFailureCadence(t *testing.T) {
	for _, failure := range []bool{false, true} {
		var out, stderr bytes.Buffer
		calls := 0
		opts := updateOptions(t, &out, &stderr, func(key string) (string, bool) {
			if key == "DEPLEXO_TOKEN" {
				return "test-access", true
			}
			return "", false
		}, roundTrip(func(*http.Request) (*http.Response, error) {
			calls++
			if failure {
				return nil, errors.New("offline")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"tag_name":"v0.2.0"}`))}, nil
		}))
		base, _ := opts.ConfigDir()
		directory := filepath.Join(base, "deplexo")
		if err := config.SaveUpdateState(directory, config.UpdateState{Latest: "v0.3.0", CheckedAt: time.Now().Add(-25 * time.Hour)}); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if code := Execute(context.Background(), opts, []string{"whoami"}); code != 0 {
				t.Fatalf("command failed: %s", stderr.String())
			}
		}
		if calls != 1 {
			t.Fatalf("checked %d times in one day, failure=%v", calls, failure)
		}
		wantPrompts := 1
		if failure {
			wantPrompts = 0
		}
		if strings.Count(stderr.String(), "Upgrade now?") != wantPrompts {
			t.Fatalf("wrong prompts: %s", stderr.String())
		}
		if strings.Contains(out.String(), "Upgrade") || strings.Contains(stderr.String(), "v0.3.0") {
			t.Fatal("cached candidate or upgrade text leaked into command output")
		}
	}
}

func TestExplicitUpgradeCancellationAndConsent(t *testing.T) {
	var out, stderr bytes.Buffer
	opts := updateOptions(t, &out, &stderr, func(string) (string, bool) { return "", false }, roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Context().Err() != nil {
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"tag_name":"v0.2.0"}`))}, nil
	}))
	opts.Executable = func() (string, error) { t.Fatal("unapproved upgrade accessed the executable"); return "", nil }
	if code := Execute(context.Background(), opts, []string{"upgrade", "--no-input"}); code != 2 {
		t.Fatalf("missing consent: %d %s", code, stderr.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stderr.Reset()
	if code := Execute(ctx, opts, []string{"upgrade", "--yes"}); code != 130 || strings.Count(stderr.String(), "Error:") != 1 {
		t.Fatalf("cancellation: %d %s", code, stderr.String())
	}
}
