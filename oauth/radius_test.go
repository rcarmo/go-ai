package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestRadiusOAuthDiscoversDeviceAuthorizationAndReportsAuthURL(t *testing.T) {
	server := newRadiusTestServer(t, radiusServerOptions{})
	provider := NewRadiusProvider(RadiusProviderOptions{Gateway: server.URL, Client: server.Client()})
	oauthCfg, err := provider.loadOAuthConfig(t.Context())
	if err != nil {
		t.Fatalf("load oauth config: %v", err)
	}
	device, err := provider.requestDeviceAuthorization(t.Context(), oauthCfg)
	if err != nil {
		t.Fatalf("device authorization: %v", err)
	}
	if device.DeviceCode != "device-1" || device.UserCode != "USER-1" || device.VerificationURIComplete != server.URL+"/verify?user_code=USER-1" {
		t.Fatalf("unexpected device response: %#v", device)
	}
}

func TestRadiusOAuthRefreshLoadsGatewayConfigAndAddsPiMessagesModels(t *testing.T) {
	server := newRadiusTestServer(t, radiusServerOptions{})
	provider := NewRadiusProvider(RadiusProviderOptions{Gateway: server.URL, Client: server.Client()})
	creds, err := provider.RefreshToken(&Credentials{Refresh: "old-refresh"})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if creds.Access != "access-1" || creds.Refresh != "refresh-1" || creds.Expires <= time.Now().UnixMilli() {
		t.Fatalf("unexpected credentials: %#v", creds)
	}
	if got := provider.GetAPIKey(creds); got != "access-1" {
		t.Fatalf("api key=%q", got)
	}

	models := provider.ModifyModels([]*goai.Model{{ID: "custom", Provider: "radius", Api: goai.ApiPiMessages}}, creds)
	if len(models) != 3 {
		t.Fatalf("expected custom + 2 gateway models, got %#v", models)
	}
	model := findModel(models, "radius-small")
	if model == nil || model.Api != goai.ApiPiMessages || model.Provider != "radius" || model.BaseURL != server.URL+"/v1" || !model.Reasoning || model.ContextWindow != 128000 || model.Cost.Input != 1.25 {
		t.Fatalf("unexpected injected model: %#v", model)
	}
	if dup := provider.ModifyModels(models, creds); len(dup) != len(models) {
		t.Fatalf("expected no duplicate Radius models, got %d -> %d", len(models), len(dup))
	}
}

func TestRadiusOAuthRefreshRetainsPreviousConfigOnTransientConfigFailure(t *testing.T) {
	server := newRadiusTestServer(t, radiusServerOptions{configStatus: http.StatusBadGateway})
	provider := NewRadiusProvider(RadiusProviderOptions{Gateway: server.URL, Client: server.Client()})
	previous := &Credentials{Refresh: "old-refresh", Extra: map[string]interface{}{"gatewayConfig": RadiusGatewayConfig{BaseURL: "https://cached.example/v1", Models: []RadiusGatewayModel{{ID: "cached", Name: "Cached", Reasoning: false, Input: []string{"text"}, Cost: goai.ModelCost{Input: 1}, ContextWindow: 10, MaxTokens: 5}}}}}
	creds, err := provider.RefreshToken(previous)
	if err != nil {
		t.Fatalf("refresh should retain previous config: %v", err)
	}
	models := provider.ModifyModels(nil, creds)
	if len(models) != 1 || models[0].ID != "cached" || models[0].BaseURL != "https://cached.example/v1" {
		t.Fatalf("expected cached model, got %#v", models)
	}
}

func TestRadiusOAuthTypedTokenError(t *testing.T) {
	server := newRadiusTestServer(t, radiusServerOptions{tokenStatus: http.StatusBadRequest, tokenError: "access_denied", tokenDescription: "nope"})
	provider := NewRadiusProvider(RadiusProviderOptions{Gateway: server.URL, Client: server.Client()})
	_, err := provider.RefreshToken(&Credentials{Refresh: "old-refresh"})
	if err == nil {
		t.Fatal("expected error")
	}
	var oauthErr *OAuthResponseError
	if !errors.As(err, &oauthErr) || oauthErr.Status != http.StatusBadRequest || oauthErr.OAuthError != "access_denied" || oauthErr.Detail != "nope" {
		t.Fatalf("unexpected typed error: %#v (%v)", oauthErr, err)
	}
}

type radiusServerOptions struct {
	configStatus     int
	tokenStatus      int
	tokenError       string
	tokenDescription string
}

func newRadiusTestServer(t *testing.T, opts radiusServerOptions) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var server *httptest.Server
	mux.HandleFunc("/v1/oauth", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]interface{}{
			"issuer":                      server.URL,
			"authorizationEndpoint":       server.URL + "/authorize",
			"tokenEndpoint":               server.URL + "/token",
			"deviceAuthorizationEndpoint": server.URL + "/device",
			"verificationEndpoint":        server.URL + "/verify",
			"clientId":                    "radius-client",
			"scope":                       "openid offline_access radius",
			"deviceCodeGrantType":         radiusDeviceGrant,
		})
	})
	mux.HandleFunc("/device", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.Form.Get("client_id") != "radius-client" || r.Form.Get("scope") == "" {
			t.Fatalf("unexpected device form: %v %#v", err, r.Form)
		}
		writeJSON(t, w, map[string]interface{}{"device_code": "device-1", "user_code": "USER-1", "verification_uri": server.URL + "/verify", "verification_uri_complete": server.URL + "/verify?user_code=USER-1", "expires_in": 600, "interval": 5})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if opts.tokenStatus != 0 {
			w.WriteHeader(opts.tokenStatus)
			writeJSON(t, w, map[string]string{"error": opts.tokenError, "error_description": opts.tokenDescription})
			return
		}
		body, _ := url.ParseQuery(readBody(t, r))
		if body.Get("client_id") != "radius-client" || body.Get("grant_type") != "refresh_token" || body.Get("refresh_token") == "" {
			t.Fatalf("unexpected token form: %#v", body)
		}
		writeJSON(t, w, map[string]interface{}{"access_token": "access-1", "refresh_token": "refresh-1", "expires_in": 3600, "scope": "radius"})
	})
	mux.HandleFunc("/v1/config", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-1" {
			t.Fatalf("authorization=%q", got)
		}
		if opts.configStatus != 0 {
			w.WriteHeader(opts.configStatus)
			_, _ = w.Write([]byte("temporary"))
			return
		}
		writeJSON(t, w, RadiusGatewayConfig{BaseURL: server.URL + "/v1", Models: []RadiusGatewayModel{{ID: "radius-small", Name: "Radius Small", Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ThinkingOff: nil}, Input: []string{"text", "image"}, Cost: goai.ModelCost{Input: 1.25, Output: 5, CacheRead: 0.25}, ContextWindow: 128000, MaxTokens: 32000}, {ID: "radius-text", Name: "Radius Text", Reasoning: false, Input: []string{"text"}, Cost: goai.ModelCost{Input: 0.5, Output: 2}, ContextWindow: 64000, MaxTokens: 16000}, {ID: "", Name: "Invalid", Input: []string{"text"}}}})
	})
	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func writeJSON(t *testing.T, w http.ResponseWriter, v interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("write json: %v", err)
	}
}

func readBody(t *testing.T, r *http.Request) string {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func findModel(models []*goai.Model, id string) *goai.Model {
	for _, model := range models {
		if model.ID == id {
			return model
		}
	}
	return nil
}

func TestRadiusOAuthLoginWithDeviceCodePollsToSuccess(t *testing.T) {
	server := newRadiusPollServer(t, []radiusPollResponse{{status: http.StatusOK, body: map[string]interface{}{"access_token": "access-login", "refresh_token": "refresh-login", "expires_in": 3600}}})
	provider := NewRadiusProvider(RadiusProviderOptions{Gateway: server.URL, Client: server.Client()})
	provider.pollWait = immediateRadiusPollWait
	oauthCfg, err := provider.loadOAuthConfig(t.Context())
	if err != nil {
		t.Fatalf("load oauth: %v", err)
	}
	var auth AuthInfo
	creds, err := provider.loginWithDeviceCode(t.Context(), oauthCfg, LoginCallbacks{OnAuth: func(info AuthInfo) { auth = info }})
	if err != nil {
		t.Fatalf("loginWithDeviceCode: %v", err)
	}
	if creds.Access != "access-login" || auth.URL != server.URL+"/verify?user_code=USER-1" || !strings.Contains(auth.Instructions, "USER-1") {
		t.Fatalf("unexpected login result creds=%#v auth=%#v", creds, auth)
	}
}

func TestRadiusOAuthPollDeviceTokenPendingThenSuccess(t *testing.T) {
	server := newRadiusPollServer(t, []radiusPollResponse{{status: http.StatusBadRequest, body: map[string]string{"error": "authorization_pending"}}, {status: http.StatusOK, body: map[string]interface{}{"access_token": "access-after-pending", "refresh_token": "refresh", "expires_in": 3600}}})
	provider, oauthCfg := newRadiusPollProvider(t, server)
	creds, err := provider.pollDeviceToken(t.Context(), oauthCfg, radiusTestDevice())
	if err != nil {
		t.Fatalf("pollDeviceToken: %v", err)
	}
	if creds.Access != "access-after-pending" {
		t.Fatalf("unexpected creds: %#v", creds)
	}
}

func TestRadiusOAuthPollDeviceTokenSlowDownThenSuccess(t *testing.T) {
	server := newRadiusPollServer(t, []radiusPollResponse{{status: http.StatusBadRequest, body: map[string]string{"error": "slow_down"}}, {status: http.StatusOK, body: map[string]interface{}{"access_token": "access-after-slow", "refresh_token": "refresh", "expires_in": 3600}}})
	provider, oauthCfg := newRadiusPollProvider(t, server)
	var intervals []time.Duration
	provider.pollWait = func(ctx context.Context, d time.Duration) error {
		intervals = append(intervals, d)
		return nil
	}
	creds, err := provider.pollDeviceToken(t.Context(), oauthCfg, radiusTestDevice())
	if err != nil {
		t.Fatalf("pollDeviceToken: %v", err)
	}
	if creds.Access != "access-after-slow" || len(intervals) != 2 || intervals[0] != 5*time.Second || intervals[1] != 10*time.Second {
		t.Fatalf("unexpected slow_down result creds=%#v intervals=%v", creds, intervals)
	}
}

func TestRadiusOAuthPollDeviceTokenTerminalErrors(t *testing.T) {
	for _, tc := range []struct{ name, oauthError, want string }{
		{"expired", "expired_token", "device authorization expired"},
		{"denied", "access_denied", "device authorization was denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newRadiusPollServer(t, []radiusPollResponse{{status: http.StatusBadRequest, body: map[string]string{"error": tc.oauthError, "error_description": "terminal"}}})
			provider, oauthCfg := newRadiusPollProvider(t, server)
			_, err := provider.pollDeviceToken(t.Context(), oauthCfg, radiusTestDevice())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestRadiusOAuthPollDeviceTokenContextCancellationAndDeadline(t *testing.T) {
	t.Run("cancelled before request", func(t *testing.T) {
		server := newRadiusPollServer(t, []radiusPollResponse{{status: http.StatusOK, body: map[string]interface{}{"access_token": "never", "expires_in": 3600}}})
		provider, oauthCfg := newRadiusPollProvider(t, server)
		ctx, cancel := context.WithCancel(t.Context())
		provider.pollWait = func(context.Context, time.Duration) error {
			cancel()
			return ctx.Err()
		}
		_, err := provider.pollDeviceToken(ctx, oauthCfg, radiusTestDevice())
		if err == nil || !errors.Is(err, context.Canceled) || server.polls() != 0 {
			t.Fatalf("expected cancellation before polling request, polls=%d err=%v", server.polls(), err)
		}
	})
	t.Run("deadline", func(t *testing.T) {
		server := newRadiusPollServer(t, []radiusPollResponse{{status: http.StatusOK, body: map[string]interface{}{"access_token": "never", "expires_in": 3600}}})
		provider, oauthCfg := newRadiusPollProvider(t, server)
		ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
		defer cancel()
		provider.pollWait = waitRadiusPollInterval
		_, err := provider.pollDeviceToken(ctx, oauthCfg, radiusTestDevice())
		if err == nil || !errors.Is(err, context.DeadlineExceeded) || server.polls() != 0 {
			t.Fatalf("expected deadline before polling request, polls=%d err=%v", server.polls(), err)
		}
	})
}

type radiusPollResponse struct {
	status int
	body   interface{}
}

type radiusPollServer struct {
	*httptest.Server
	pollCount int
}

func newRadiusPollServer(t *testing.T, responses []radiusPollResponse) *radiusPollServer {
	t.Helper()
	state := &radiusPollServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/oauth", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]interface{}{"issuer": state.URL, "authorizationEndpoint": state.URL + "/authorize", "tokenEndpoint": state.URL + "/token", "deviceAuthorizationEndpoint": state.URL + "/device", "verificationEndpoint": state.URL + "/verify", "clientId": "radius-client", "scope": "openid radius", "deviceCodeGrantType": radiusDeviceGrant})
	})
	mux.HandleFunc("/device", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]interface{}{"device_code": "device-1", "user_code": "USER-1", "verification_uri": state.URL + "/verify", "verification_uri_complete": state.URL + "/verify?user_code=USER-1", "expires_in": 600, "interval": 5})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		idx := state.pollCount
		state.pollCount++
		if idx >= len(responses) {
			idx = len(responses) - 1
		}
		resp := responses[idx]
		if resp.status != 0 {
			w.WriteHeader(resp.status)
		}
		writeJSON(t, w, resp.body)
	})
	state.Server = httptest.NewServer(mux)
	t.Cleanup(state.Close)
	return state
}

func (s *radiusPollServer) polls() int { return s.pollCount }

func newRadiusPollProvider(t *testing.T, server *radiusPollServer) (*RadiusProvider, *radiusOAuthConfig) {
	t.Helper()
	provider := NewRadiusProvider(RadiusProviderOptions{Gateway: server.URL, Client: server.Client()})
	provider.pollWait = immediateRadiusPollWait
	oauthCfg, err := provider.loadOAuthConfig(t.Context())
	if err != nil {
		t.Fatalf("load oauth config: %v", err)
	}
	return provider, oauthCfg
}

func radiusTestDevice() *radiusDeviceAuthorization {
	return &radiusDeviceAuthorization{DeviceCode: "device-1", UserCode: "USER-1", ExpiresIn: 600, Interval: 1}
}

func immediateRadiusPollWait(context.Context, time.Duration) error { return nil }
