package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

type unchangedRoundTripFunc func(*http.Request) (*http.Response, error)

func (f unchangedRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func unchangedJSON(status int, value any) *http.Response {
	body, _ := json.Marshal(value)
	return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d", status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}
}

func unchangedCodexAccessToken(account string) string {
	payload, _ := json.Marshal(map[string]any{"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": account}})
	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func TestOAuthDeviceCodePublicPollingUpstream(t *testing.T) {
	interval := 1
	expires := 3
	var calls atomic.Int32
	value, err := PollOAuthDeviceCodeFlow(OAuthDeviceCodePollOptions[string]{
		IntervalSeconds:  &interval,
		ExpiresInSeconds: &expires,
		Poll: func(context.Context) (OAuthDeviceCodePollResult[string], error) {
			if calls.Add(1) == 1 {
				return OAuthDeviceCodePollResult[string]{Status: "slow_down", IntervalSeconds: 1}, nil
			}
			return OAuthDeviceCodePollResult[string]{Status: "complete", Value: "token"}, nil
		},
	})
	if err != nil || value != "token" || calls.Load() != 2 {
		t.Fatalf("value=%q err=%v calls=%d", value, err, calls.Load())
	}
	zero := 0
	if _, err := PollOAuthDeviceCodeFlow(OAuthDeviceCodePollOptions[string]{IntervalSeconds: &interval, ExpiresInSeconds: &zero, Poll: func(context.Context) (OAuthDeviceCodePollResult[string], error) {
		return OAuthDeviceCodePollResult[string]{Status: "complete", Value: "late"}, nil
	}}); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expiry error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PollOAuthDeviceCodeFlow(OAuthDeviceCodePollOptions[string]{IntervalSeconds: &interval, Signal: ctx, Poll: func(context.Context) (OAuthDeviceCodePollResult[string], error) {
		return OAuthDeviceCodePollResult[string]{Status: "pending"}, nil
	}}); err == nil || err.Error() != "Login cancelled" {
		t.Fatalf("cancel error = %v", err)
	}
}

func TestOAuthCallbackClaimsFirstRequestUpstream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	complete := make(chan struct{})
	claimed := make(chan struct{})
	server, err := startOAuthCallbackServer(ctx, oauthCallbackOptions{
		ProviderName: "Example",
		Host:         "127.0.0.1",
		Port:         0,
		Path:         "/callback",
		State:        "expected-state",
		Complete: func(context.Context, *url.URL) error {
			close(claimed)
			<-complete
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	firstDone := make(chan *http.Response, 1)
	go func() {
		response, _ := http.Get(server.RedirectURI + "?code=c&state=expected-state")
		firstDone <- response
	}()
	select {
	case <-claimed:
	case <-ctx.Done():
		close(complete)
		t.Fatal(ctx.Err())
	}
	for {
		select {
		case <-time.After(10 * time.Millisecond):
			request, err := http.NewRequest(http.MethodGet, server.RedirectURI+"?code=c2&state=expected-state", nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusConflict {
				_ = response.Body.Close()
				close(complete)
				<-firstDone
				t.Fatalf("second callback status = %d, want 409", response.StatusCode)
			}
			_ = response.Body.Close()
			close(complete)
			first := <-firstDone
			_ = first.Body.Close()
			if first.StatusCode != http.StatusOK {
				t.Fatalf("first status = %d", first.StatusCode)
			}
			if _, err := server.Wait(); err != nil {
				t.Fatal(err)
			}
			return
		case <-ctx.Done():
			close(complete)
			t.Fatal(ctx.Err())
		}
	}
}

func TestGitHubCopilotVerificationURLUpstream(t *testing.T) {
	oldClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = oldClient })
	for _, tc := range []struct {
		name string
		uri  string
		want string
		bad  bool
	}{
		{name: "untrusted", uri: "$(id>/tmp/pwned)", bad: true},
		{name: "normalized", uri: "https://github.com/login/\x1b]8;;evil", want: "https://github.com/login/%1B]8;;evil"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			http.DefaultClient = &http.Client{Transport: unchangedRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return unchangedJSON(http.StatusOK, map[string]any{"device_code": "device", "user_code": "ABCD", "verification_uri": tc.uri, "interval": 1, "expires_in": 900}), nil
			})}
			device, err := startDeviceFlow("github.com")
			if tc.bad {
				if err == nil || !strings.Contains(strings.ToLower(err.Error()), "untrusted") {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if device.VerificationURI != tc.want {
				t.Fatalf("URI = %q, want %q", device.VerificationURI, tc.want)
			}
		})
	}
}

func TestGitHubCopilotAuthDetailsAndClassifierCatalogUpstream(t *testing.T) {
	models := []string{"chat-model", "decision-model"}
	withCopilotPolicyTestHooks(t, "https://api.individual.githubcopilot.com", nil)
	copilotPolicyListModels = func() []*goai.Model { return []*goai.Model{{ID: models[0], Provider: goai.ProviderGitHubCopilot}} }
	catalog := parseGitHubCopilotModelCatalog([]map[string]any{
		{"id": models[0], "model_picker_enabled": true, "capabilities": map[string]any{"type": "chat", "supports": map[string]any{"tool_calls": true}}},
		{"id": models[1], "model_picker_enabled": false, "capabilities": map[string]any{"type": "decisions", "supports": map[string]any{}}},
	}, true)
	if !reflect.DeepEqual(catalog.AvailableModelIDs, []string{models[0]}) {
		t.Fatalf("models = %#v", catalog.AvailableModelIDs)
	}
	classifierField := reflect.ValueOf(catalog).FieldByName("AvailableClassifierModelIDs")
	if !classifierField.IsValid() || !reflect.DeepEqual(classifierField.Interface(), []string{models[1]}) {
		t.Fatalf("classifiers = %#v", classifierField)
	}
	runtime, err := RuntimeForGitHubCopilot(&Credentials{
		Access:  "tid=test;exp=9999999999;proxy-ep=proxy.individual.githubcopilot.com;",
		Expires: time.Now().Add(time.Hour).UnixMilli(),
		Extra: map[string]any{
			"availableModelIds":           []string{"gpt-5.4"},
			"availableClassifierModelIds": []string{"gpt-6-luna-decisions"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.ClassifierModels) != 1 || runtime.ClassifierModels[0].ID != "gpt-6-luna-decisions" {
		t.Fatalf("runtime classifiers = %#v", runtime.ClassifierModels)
	}
}

func TestKimiCodingHostAndRefreshRecoveryUpstream(t *testing.T) {
	t.Setenv("KIMI_CODE_OAUTH_HOST", "https://auth.example.com/")
	var refreshes atomic.Int32
	var seen []string
	var mu sync.Mutex
	client := &http.Client{Transport: unchangedRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		seen = append(seen, req.URL.String())
		mu.Unlock()
		if req.URL.Path == "/api/oauth/token" && refreshes.Add(1) == 1 {
			return unchangedJSON(http.StatusTooManyRequests, map[string]any{"error": "temporarily_unavailable"}), nil
		}
		return unchangedJSON(http.StatusOK, map[string]any{"access_token": "new-access", "refresh_token": "new-refresh", "expires_in": 60}), nil
	})}
	provider := NewKimiCodingProvider(client)
	provider.pollWait = func(context.Context, time.Duration) error { return nil }
	creds, err := provider.RefreshTokenContext(context.Background(), &Credentials{Refresh: "old-refresh"})
	if err != nil {
		t.Fatal(err)
	}
	if creds.Access != "new-access" || refreshes.Load() != 2 {
		t.Fatalf("creds=%#v refreshes=%d", creds, refreshes.Load())
	}
	if len(seen) != 2 || seen[0] != "https://auth.example.com/api/oauth/token" || seen[1] != seen[0] {
		t.Fatalf("URLs=%#v", seen)
	}

	refreshes.Store(0)
	_, err = provider.RefreshTokenContext(context.Background(), &Credentials{Refresh: "old-refresh"})
	if err != nil {
		t.Fatal(err)
	}
	provider.client = &http.Client{Transport: unchangedRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return unchangedJSON(http.StatusBadRequest, map[string]any{"error": "invalid_grant"}), nil
	})}
	_, err = provider.RefreshTokenContext(context.Background(), &Credentials{Refresh: "old-refresh"})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "unauthorized") {
		t.Fatalf("invalid grant err = %v", err)
	}
}

func TestOpenAICodexDeviceFlowUpstream(t *testing.T) {
	transport := unchangedRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://auth.openai.com/api/accounts/deviceauth/usercode":
			var body map[string]any
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body["client_id"] != codexBrowserClientID {
				t.Fatalf("usercode body=%#v err=%v", body, err)
			}
			return unchangedJSON(http.StatusOK, map[string]any{"device_auth_id": "device", "user_code": "ABCD-1234", "interval": "1"}), nil
		case "https://auth.openai.com/api/accounts/deviceauth/token":
			return unchangedJSON(http.StatusForbidden, map[string]any{"error": map[string]any{"code": "deviceauth_authorization_pending"}}), nil
		case "https://auth.openai.com/oauth/token":
			return unchangedJSON(http.StatusOK, map[string]any{"access_token": unchangedCodexAccessToken("account-1"), "refresh_token": "refresh", "expires_in": 3600}), nil
		default:
			return unchangedJSON(http.StatusInternalServerError, map[string]string{"error": "unexpected " + req.URL.String()}), nil
		}
	})
	client := &http.Client{Transport: transport}
	oldDefault := http.DefaultClient
	http.DefaultClient = client
	t.Cleanup(func() { http.DefaultClient = oldDefault })
	provider := &OpenAICodexProvider{client: client}
	var auth AuthInfo
	calls := atomic.Int32{}
	provider.client = &http.Client{Transport: unchangedRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == "https://auth.openai.com/api/accounts/deviceauth/token" {
			if calls.Add(1) == 1 {
				return unchangedJSON(http.StatusForbidden, map[string]any{"error": map[string]any{"code": "deviceauth_authorization_pending"}}), nil
			}
			return unchangedJSON(http.StatusOK, map[string]any{"authorization_code": "oauth-code", "code_verifier": "verifier"}), nil
		}
		return transport(req)
	})}
	creds, err := provider.Login(LoginCallbacks{
		OnSelect: func(SelectPrompt) (string, error) { return "device_code", nil },
		OnAuth:   func(info AuthInfo) { auth = info },
	})
	if err != nil {
		t.Fatal(err)
	}
	if auth.URL != "https://auth.openai.com/codex/device" {
		t.Fatalf("auth URL = %q", auth.URL)
	}
	for _, pair := range []struct {
		name string
		want any
	}{{"UserCode", "ABCD-1234"}, {"IntervalSeconds", 1}, {"ExpiresInSeconds", 900}} {
		field := reflect.ValueOf(auth).FieldByName(pair.name)
		if !field.IsValid() || field.Interface() != pair.want {
			t.Fatalf("auth %s = %#v", pair.name, field)
		}
	}
	if creds.Extra["accountId"] != "account-1" || calls.Load() != 2 {
		t.Fatalf("creds=%#v calls=%d", creds, calls.Load())
	}
}

func TestOpenAICodexPollErrorIncludesBodyUpstream(t *testing.T) {
	body := `{"error":"server_error","error_description":"try again later"}`
	client := &http.Client{Transport: unchangedRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/deviceauth/usercode") {
			return unchangedJSON(http.StatusOK, map[string]any{"device_auth_id": "device", "user_code": "ABCD", "interval": 1}), nil
		}
		response := unchangedJSON(http.StatusInternalServerError, map[string]any{})
		response.Body = io.NopCloser(strings.NewReader(body))
		return response, nil
	})}
	provider := &OpenAICodexProvider{client: client}
	oldDefault := http.DefaultClient
	http.DefaultClient = client
	t.Cleanup(func() { http.DefaultClient = oldDefault })
	_, err := provider.Login(LoginCallbacks{OnSelect: func(SelectPrompt) (string, error) { return "device_code", nil }})
	if err == nil || !strings.Contains(err.Error(), "status 500") || !strings.Contains(err.Error(), body) {
		t.Fatalf("err = %v", err)
	}
}
