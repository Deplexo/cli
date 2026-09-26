package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAppInventoryPublicContract(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/user/api/v1/me" || r.Header.Get("Authorization") != "Bearer test-access" {
			t.Fatalf("unexpected inventory request: %s %s", r.Method, r.URL)
		}
		_, _ = io.WriteString(w, `{"apps":[{"id":"11111111-1111-4111-8111-111111111111","name":"example","status":"running","subdomain":"example"}],"user":{"email":"private@example.com"},"plan":{"id":"private-plan"}}`)
	})
	result, err := client.Apps(context.Background(), "test-access")
	if err != nil || len(result.Apps) != 1 || result.Apps[0].Name != "example" {
		t.Fatalf("inventory: %+v %v", result, err)
	}
	data, err := json.Marshal(result)
	if err != nil || strings.Contains(string(data), "private") {
		t.Fatalf("inventory included account details: %s %v", data, err)
	}
}

func TestAppInventoryEmptyAndMalformed(t *testing.T) {
	for _, body := range []string{`{"apps":[]}`, `{}`, `{"apps":null}`, `{"apps":[{"id":"invalid","status":"running"}]}`, `{"apps":"wrong"}`} {
		t.Run(body, func(t *testing.T) {
			client, _ := testClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
			result, err := client.Apps(context.Background(), "test-access")
			if body == `{"apps":[]}` {
				if err != nil || result.Apps == nil || len(result.Apps) != 0 {
					t.Fatalf("empty inventory: %+v %v", result, err)
				}
			} else if err == nil {
				t.Fatal("malformed inventory accepted")
			}
		})
	}
}

func TestDeployPublicContractAndAmbiguousResults(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	for _, tc := range []struct {
		name, body string
		status     int
		ambiguous  bool
	}{
		{"accepted", `{"appId":"` + id + `","deploymentId":"22222222-2222-4222-8222-222222222222","jobId":42,"status":"queued"}`, 202, false},
		{"missing-identity", `{"appId":"` + id + `","status":"queued"}`, 202, true},
		{"wrong-app", `{"appId":"33333333-3333-4333-8333-333333333333","deploymentId":"22222222-2222-4222-8222-222222222222","status":"queued"}`, 202, true},
		{"invalid-response", `{`, 202, true},
		{"server-error", `{}`, 500, true},
		{"conflict", `{}`, 409, false},
		{"forbidden", `{}`, 403, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				body, _ := io.ReadAll(r.Body)
				if r.Method != "POST" || r.URL.Path != "/user/api/v1/apps/"+id+"/restart" || string(body) != "{}" || r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("wrong deployment contract: %s %s %s", r.Method, r.URL, body)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			result, err := client.Deploy(context.Background(), "test-access", id)
			var mutation *MutationError
			if calls != 1 || errors.As(err, &mutation) != tc.ambiguous {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
			if tc.name == "accepted" {
				if err != nil || result.DeploymentID == "" || result.AppID != id {
					t.Fatalf("accepted result: %+v %v", result, err)
				}
			} else if err == nil {
				t.Fatal("failure accepted as success")
			}
		})
	}
}
