package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func withCopilotPolicyTestHooks(t *testing.T, serverURL string, models []*goai.Model) {
	t.Helper()
	oldClient := copilotPolicyHTTPClient
	oldList := copilotPolicyListModels
	oldBase := copilotPolicyBaseURL
	oldWait := githubCopilotDevicePollWait
	t.Cleanup(func() {
		copilotPolicyHTTPClient = oldClient
		copilotPolicyListModels = oldList
		copilotPolicyBaseURL = oldBase
		githubCopilotDevicePollWait = oldWait
	})
	copilotPolicyHTTPClient = http.DefaultClient
	githubCopilotDevicePollWait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	copilotPolicyBaseURL = func(string, string) string { return serverURL }
	copilotPolicyListModels = func() []*goai.Model { return models }
}

func TestV0843GitHubCopilotCatalogFiltersPolicyModels(t *testing.T) {
	withCopilotPolicyTestHooks(t, "https://api.individual.githubcopilot.com", []*goai.Model{{ID: "known", Provider: goai.ProviderGitHubCopilot}})
	catalog := parseGitHubCopilotModelCatalog([]map[string]interface{}{
		{"id": "visible", "model_picker_enabled": true, "policy": map[string]interface{}{"state": "enabled"}, "capabilities": map[string]interface{}{"supports": map[string]interface{}{"tool_calls": true}}},
		{"id": "known", "model_picker_enabled": true, "policy": map[string]interface{}{"state": "unconfigured"}, "capabilities": map[string]interface{}{"supports": map[string]interface{}{"tool_calls": true}}},
		{"id": "unknown", "model_picker_enabled": true, "policy": map[string]interface{}{"state": "unconfigured"}, "capabilities": map[string]interface{}{"supports": map[string]interface{}{"tool_calls": true}}},
		{"id": "no-tools", "model_picker_enabled": true, "policy": map[string]interface{}{"state": "unconfigured"}, "capabilities": map[string]interface{}{"supports": map[string]interface{}{"tool_calls": false}}},
		{"id": "disabled", "model_picker_enabled": true, "policy": map[string]interface{}{"state": "disabled"}},
	}, true)
	if !reflect.DeepEqual(catalog.AvailableModelIDs, []string{"visible", "known", "unknown"}) {
		t.Fatalf("available=%#v", catalog.AvailableModelIDs)
	}
	if !reflect.DeepEqual(catalog.PolicyModelIDs, []string{"known"}) {
		t.Fatalf("policy=%#v", catalog.PolicyModelIDs)
	}
}

func TestV0843GitHubCopilotPolicyRetriesRetryAfter(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/known/policy" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("throttle"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	withCopilotPolicyTestHooks(t, server.URL, nil)
	ok, err := EnableGitHubCopilotModelContext(context.Background(), "token", "known", "")
	if err != nil || !ok || attempts.Load() != 2 {
		t.Fatalf("ok=%v err=%v attempts=%d", ok, err, attempts.Load())
	}
}

func TestV0843GitHubCopilotPolicyContinuesAfterTransportFailure(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/models/") : len(r.URL.Path)-len("/policy")]
		seen = append(seen, id)
		if id == "bad" {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("no hijacker")
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Fatal(err)
			}
			_ = conn.Close()
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	withCopilotPolicyTestHooks(t, server.URL, nil)
	got := EnableGitHubCopilotModels(context.Background(), "token", "", []string{"bad", "good"})
	if !reflect.DeepEqual(got, []string{"good"}) {
		t.Fatalf("enabled=%#v seen=%#v", got, seen)
	}
}

func TestV0843GitHubCopilotPolicyDelayBudgetStopsImmediately(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, "later")
	}))
	defer server.Close()
	withCopilotPolicyTestHooks(t, server.URL, nil)
	ok, err := EnableGitHubCopilotModelContext(context.Background(), "token", "known", "")
	if err == nil || ok || !strings.Contains(err.Error(), "rate limited") || attempts.Load() != 1 {
		t.Fatalf("expected immediate budget rate limit after one attempt, ok=%v err=%v attempts=%d", ok, err, attempts.Load())
	}
}

func TestV0843GitHubCopilotLoginWorkflowStopsPolicyAndReturnsCredentials(t *testing.T) {
	var policyIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/login/device/code":
			writeCopilotJSON(t, w, map[string]any{"device_code": "device-code", "user_code": "ABCD-EFGH", "verification_uri": "https://github.com/login/device", "interval": 1, "expires_in": 900})
		case r.URL.Path == "/login/oauth/access_token":
			writeCopilotJSON(t, w, map[string]any{"access_token": "github-token"})
		case r.URL.Path == "/copilot_internal/v2/token":
			writeCopilotJSON(t, w, map[string]any{"token": "tid=test;exp=9999999999;proxy-ep=proxy.individual.githubcopilot.com;", "expires_at": float64(9999999999)})
		case r.URL.Path == "/models":
			writeCopilotJSON(t, w, map[string]any{"data": []map[string]any{
				{"id": "gpt-4.1", "model_picker_enabled": true, "policy": map[string]any{"state": "unconfigured"}, "capabilities": map[string]any{"supports": map[string]any{"tool_calls": true}}},
				{"id": "claude-sonnet-4.5", "model_picker_enabled": true, "policy": map[string]any{"state": "unconfigured"}, "capabilities": map[string]any{"supports": map[string]any{"tool_calls": true}}},
				{"id": "remote-only-model", "model_picker_enabled": true, "policy": map[string]any{"state": "unconfigured"}, "capabilities": map[string]any{"supports": map[string]any{"tool_calls": true}}},
				{"id": "gpt-5.4", "model_picker_enabled": true, "policy": map[string]any{"state": "unconfigured"}, "capabilities": map[string]any{"supports": map[string]any{"tool_calls": false}}},
			}})
		case strings.HasPrefix(r.URL.Path, "/models/") && strings.HasSuffix(r.URL.Path, "/policy"):
			id, err := url.PathUnescape(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/models/"), "/policy"))
			if err != nil {
				t.Fatal(err)
			}
			policyIDs = append(policyIDs, id)
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = fmt.Fprint(w, "later")
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	oldDefaultClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = oldDefaultClient })
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	http.DefaultClient = &http.Client{Transport: rewriteToServerTransport{target: endpoint, base: http.DefaultTransport}}
	withCopilotPolicyTestHooks(t, server.URL, []*goai.Model{{ID: "gpt-4.1", Provider: goai.ProviderGitHubCopilot}, {ID: "claude-sonnet-4.5", Provider: goai.ProviderGitHubCopilot}, {ID: "gpt-5.4", Provider: goai.ProviderGitHubCopilot}})

	var auth AuthInfo
	var progress []string
	creds, err := (&GitHubCopilotProvider{}).Login(LoginCallbacks{
		OnPrompt:   func(Prompt) (string, error) { return "", nil },
		OnAuth:     func(info AuthInfo) { auth = info },
		OnProgress: func(message string) { progress = append(progress, message) },
	})
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if auth.URL != "https://github.com/login/device" || !strings.Contains(auth.Instructions, "ABCD-EFGH") {
		t.Fatalf("auth info=%#v", auth)
	}
	if creds.Access == "" || creds.Refresh != "github-token" {
		t.Fatalf("credentials not returned for persistence: %#v", creds)
	}
	if !reflect.DeepEqual(policyIDs, []string{"gpt-4.1"}) {
		t.Fatalf("policyIDs=%#v", policyIDs)
	}
	if !reflect.DeepEqual(progress, []string{"Enabling models..."}) {
		t.Fatalf("progress=%#v", progress)
	}
	available, ok := creds.Extra["availableModelIds"].([]string)
	if !ok || !reflect.DeepEqual(available, []string{"gpt-4.1", "claude-sonnet-4.5", "remote-only-model"}) {
		t.Fatalf("availableModelIds=%#v", creds.Extra["availableModelIds"])
	}
}

type rewriteToServerTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (t rewriteToServerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = t.target.Scheme
	clone.URL.Host = t.target.Host
	clone.Host = t.target.Host
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}

func writeCopilotJSON(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatal(err)
	}
}
