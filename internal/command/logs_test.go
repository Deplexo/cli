package command

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestLogFollowBackoffAndRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var times []time.Time
		codes := []int{429, 503, 200, 429, 403}
		options := Options{Out: io.Discard, Err: io.Discard,
			ConfigDir: func() (string, error) { return t.TempDir(), nil },
			LookupEnv: func(key string) (string, bool) { return "test-access", key == "DEPLEXO_TOKEN" },
			Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				i := len(times)
				times = append(times, time.Now())
				if i >= len(codes) {
					t.Fatal("retried terminal failure")
				}
				body := `{"error":"unavailable"}`
				headers := make(http.Header)
				if i == 0 {
					headers.Set("Retry-After", "10")
				}
				if codes[i] == 200 {
					body = `{"lines":[],"nextSince":"cursor"}`
				}
				if i > 2 && r.URL.Query().Get("since") != "cursor" {
					t.Fatal("lost resume cursor")
				}
				return &http.Response{StatusCode: codes[i], Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			}),
		}
		if code := Execute(context.Background(), options, []string{"logs", "--app", "11111111-1111-4111-8111-111111111111", "--follow"}); code != 4 {
			t.Fatalf("exit=%d", code)
		}
		if len(times) != 5 {
			t.Fatalf("requests=%d", len(times))
		}
		bounds := [][2]time.Duration{{10 * time.Second, 11 * time.Second}, {6 * time.Second, 7500 * time.Millisecond}, {3 * time.Second, 3 * time.Second}, {3 * time.Second, 3750 * time.Millisecond}}
		for i, b := range bounds {
			d := times[i+1].Sub(times[i])
			if d < b[0] || d > b[1] {
				t.Fatalf("wait %d = %s", i, d)
			}
		}
	})
}

func TestLogFollowCancellationDuringCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		calls := 0
		options := Options{Out: io.Discard, Err: io.Discard, ConfigDir: func() (string, error) { return t.TempDir(), nil }, LookupEnv: func(k string) (string, bool) { return "test-access", k == "DEPLEXO_TOKEN" }, Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"60"}}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
		})}
		Execute(ctx, options, []string{"logs", "--app", "11111111-1111-4111-8111-111111111111", "--follow"})
		if calls != 1 {
			t.Fatalf("requests after cancellation: %d", calls)
		}
	})
}
