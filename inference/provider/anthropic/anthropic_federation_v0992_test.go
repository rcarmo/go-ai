package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

const testAssertion = "header.payload.signature"

func writeIdentityToken(t *testing.T, content string) string {
	t.Helper()
	path := t.TempDir() + "/identity.jwt"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func federationEnv(path string) goai.ProviderEnv {
	return goai.ProviderEnv{
		anthropicFederationRuleIDEnv:  "fdrl_test",
		anthropicOrganizationIDEnv:    "org-test",
		anthropicIdentityTokenFileEnv: path,
		anthropicServiceAccountIDEnv:  "svac_test",
		anthropicWorkspaceIDEnv:       "wrkspc_test",
	}
}

func federationModel(baseURL string) *goai.Model {
	return &goai.Model{ID: "claude-test", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, BaseURL: baseURL, Input: []string{"text"}, ContextWindow: 1000, MaxTokens: 100}
}

func simpleAnthropicContext() *goai.Context {
	return &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
}

func writeFederationSSE(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"))
	_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n"))
	_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
}

func completeFederation(t *testing.T, model *goai.Model, opts *goai.StreamOptions) (*goai.Message, error) {
	t.Helper()
	return goai.Complete(context.Background(), model, simpleAnthropicContext(), opts)
}

func TestAnthropicFederationExactExchangeTrimBearerAndCacheReuse(t *testing.T) {
	resetAnthropicFederationCacheForTest()
	identityPath := writeIdentityToken(t, "  \n"+testAssertion+"\t\n")
	var tokenRequests atomic.Int32
	var capturedTokenPath, capturedTokenQuery, capturedTokenAuth string
	var capturedBody map[string]string
	var messageAuths []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/oauth/token":
			tokenRequests.Add(1)
			capturedTokenPath = r.URL.Path
			capturedTokenQuery = r.URL.RawQuery
			capturedTokenAuth = r.Header.Get("Authorization")
			if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
				t.Fatalf("decode token body: %v", err)
			}
			if got := r.Header.Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type=%q", got)
			}
			if got := r.Header.Get("anthropic-beta"); got != anthropicFederationOAuthBeta+","+anthropicFederationOIDCBeta {
				t.Fatalf("anthropic-beta=%q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "federated-token", "expires_in": 3600})
		case "/v1/messages":
			mu.Lock()
			messageAuths = append(messageAuths, r.Header.Get("Authorization"))
			mu.Unlock()
			writeFederationSSE(w)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	model := federationModel(server.URL)
	opts := &goai.StreamOptions{Env: federationEnv(identityPath), CacheRetention: goai.CacheRetentionNone}
	for i := 0; i < 2; i++ {
		msg, err := completeFederation(t, model, opts)
		if err != nil || msg.StopReason != goai.StopReasonStop {
			t.Fatalf("attempt %d msg=%#v err=%v", i, msg, err)
		}
	}
	if tokenRequests.Load() != 1 {
		t.Fatalf("token requests=%d, want cache reuse with one exchange", tokenRequests.Load())
	}
	if capturedTokenPath != "/v1/oauth/token" || capturedTokenQuery != "" || capturedTokenAuth != "" {
		t.Fatalf("token path/query/auth=%q/%q/%q", capturedTokenPath, capturedTokenQuery, capturedTokenAuth)
	}
	wantBody := map[string]string{
		"grant_type":         anthropicFederationGrantType,
		"assertion":          testAssertion,
		"federation_rule_id": "fdrl_test",
		"organization_id":    "org-test",
		"service_account_id": "svac_test",
		"workspace_id":       "wrkspc_test",
	}
	if fmt.Sprint(capturedBody) != fmt.Sprint(wantBody) {
		t.Fatalf("token body=%#v want %#v", capturedBody, wantBody)
	}
	if strings.Contains(capturedTokenQuery, testAssertion) || strings.Contains(fmt.Sprint(capturedTokenAuth), testAssertion) {
		t.Fatalf("assertion leaked outside token body")
	}
	if len(messageAuths) != 2 || messageAuths[0] != "Bearer federated-token" || messageAuths[1] != "Bearer federated-token" {
		t.Fatalf("message auths=%#v", messageAuths)
	}
}

func TestAnthropicFederationPrecedencePreventsTokenFileReadOrExchange(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts *goai.StreamOptions
		want string
	}{
		{name: "explicit api key", opts: &goai.StreamOptions{APIKey: "api-key", Env: federationEnv("/definitely/missing.jwt")}, want: "api-key"},
		{name: "auth token env", opts: &goai.StreamOptions{Env: func() goai.ProviderEnv {
			e := federationEnv("/definitely/missing.jwt")
			e["ANTHROPIC_AUTH_TOKEN"] = "auth-token"
			return e
		}()}, want: "Bearer auth-token"},
		{name: "explicit auth header", opts: &goai.StreamOptions{Headers: map[string]string{"Authorization": "Bearer header-token"}, Env: federationEnv("/definitely/missing.jwt")}, want: "Bearer header-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetAnthropicFederationCacheForTest()
			var tokenRequests atomic.Int32
			var messageAuth, messageAPIKey string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/oauth/token":
					tokenRequests.Add(1)
					t.Fatalf("token endpoint should not be called")
				case "/v1/messages":
					messageAuth = r.Header.Get("Authorization")
					messageAPIKey = r.Header.Get("X-Api-Key")
					writeFederationSSE(w)
				}
			}))
			defer server.Close()
			msg, err := completeFederation(t, federationModel(server.URL), tc.opts)
			if err != nil || msg.StopReason != goai.StopReasonStop {
				t.Fatalf("msg=%#v err=%v", msg, err)
			}
			if tokenRequests.Load() != 0 {
				t.Fatalf("token requests=%d", tokenRequests.Load())
			}
			if strings.HasPrefix(tc.want, "Bearer ") {
				if messageAuth != tc.want || messageAPIKey != "" {
					t.Fatalf("Authorization=%q X-Api-Key=%q", messageAuth, messageAPIKey)
				}
			} else if messageAPIKey != tc.want || messageAuth != "" {
				t.Fatalf("Authorization=%q X-Api-Key=%q", messageAuth, messageAPIKey)
			}
		})
	}
}

func TestAnthropicFederationCoalescesConcurrentExchangesAndRefreshesOnExpiry(t *testing.T) {
	resetAnthropicFederationCacheForTest()
	identityPath := writeIdentityToken(t, testAssertion)
	var tokenRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/oauth/token":
			n := tokenRequests.Add(1)
			time.Sleep(50 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": fmt.Sprintf("federated-token-%d", n), "expires_in": 3600})
		case "/v1/messages":
			writeFederationSSE(w)
		}
	}))
	defer server.Close()
	model := federationModel(server.URL)
	opts := &goai.StreamOptions{Env: federationEnv(identityPath)}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			msg, err := completeFederation(t, model, opts)
			if err != nil || msg.StopReason != goai.StopReasonStop {
				t.Errorf("msg=%#v err=%v", msg, err)
			}
		}()
	}
	wg.Wait()
	if tokenRequests.Load() != 1 {
		t.Fatalf("concurrent token requests=%d, want one coalesced exchange", tokenRequests.Load())
	}

	resetAnthropicFederationCacheForTest()
	tokenRequests.Store(0)
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/oauth/token":
			tokenRequests.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "short-token", "expires_in": 1})
		case "/v1/messages":
			writeFederationSSE(w)
		}
	})
	for i := 0; i < 2; i++ {
		msg, err := completeFederation(t, model, opts)
		if err != nil || msg.StopReason != goai.StopReasonStop {
			t.Fatalf("short attempt %d msg=%#v err=%v", i, msg, err)
		}
	}
	if tokenRequests.Load() != 2 {
		t.Fatalf("expired token requests=%d, want refresh each call", tokenRequests.Load())
	}
}

func TestAnthropicFederationErrorsAndRedaction(t *testing.T) {
	cases := []struct {
		name     string
		handler  http.HandlerFunc
		contains string
	}{
		{name: "missing access token", contains: "missing access_token", handler: func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"expires_in": 3600})
		}},
		{name: "unsupported token type", contains: "unsupported token_type", handler: func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "token", "expires_in": 3600, "token_type": "mac"})
		}},
		{name: "malformed json", contains: "non-JSON", handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("not json")) }},
		{name: "echoed assertion redacted", contains: "invalid_grant", handler: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant", "error_description": "bad assertion " + testAssertion, "assertion": testAssertion})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetAnthropicFederationCacheForTest()
			identityPath := writeIdentityToken(t, testAssertion)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/oauth/token" {
					tc.handler(w, r)
					return
				}
				writeFederationSSE(w)
			}))
			defer server.Close()
			_, err := completeFederation(t, federationModel(server.URL), &goai.StreamOptions{Env: federationEnv(identityPath)})
			if err == nil || !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("err=%v, want contains %q", err, tc.contains)
			}
			if strings.Contains(err.Error(), testAssertion) {
				t.Fatalf("assertion leaked in error: %v", err)
			}
		})
	}
}

func TestAnthropicFederationTransportErrorCancellationAndResetIsolation(t *testing.T) {
	resetAnthropicFederationCacheForTest()
	identityPath := writeIdentityToken(t, testAssertion)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth/token" {
			panic("boom")
		}
	}))
	url := server.URL
	server.Close()
	_, err := completeFederation(t, federationModel(url), &goai.StreamOptions{Env: federationEnv(identityPath)})
	if err == nil || !strings.Contains(err.Error(), "failed to reach token endpoint") || strings.Contains(err.Error(), testAssertion) {
		t.Fatalf("transport err=%v", err)
	}

	resetAnthropicFederationCacheForTest()
	cancelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth/token" {
			time.Sleep(200 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "late", "expires_in": 3600})
		}
	}))
	defer cancelServer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = goai.Complete(ctx, federationModel(cancelServer.URL), simpleAnthropicContext(), &goai.StreamOptions{Env: federationEnv(identityPath)})
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("cancel err=%v", err)
	}

	resetAnthropicFederationCacheForTest()
	var tokenRequests atomic.Int32
	resetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/oauth/token":
			tokenRequests.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "token", "expires_in": 3600})
		case "/v1/messages":
			writeFederationSSE(w)
		}
	}))
	defer resetServer.Close()
	model := federationModel(resetServer.URL)
	opts := &goai.StreamOptions{Env: federationEnv(identityPath)}
	for i := 0; i < 2; i++ {
		if _, err := completeFederation(t, model, opts); err != nil {
			t.Fatal(err)
		}
	}
	if tokenRequests.Load() != 1 {
		t.Fatalf("before reset token requests=%d", tokenRequests.Load())
	}
	resetAnthropicFederationCacheForTest()
	if _, err := completeFederation(t, model, opts); err != nil {
		t.Fatal(err)
	}
	if tokenRequests.Load() != 2 {
		t.Fatalf("after reset token requests=%d", tokenRequests.Load())
	}
}
