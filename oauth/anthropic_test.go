package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type anthropicTokenTestTransport struct {
	t           *testing.T
	destination *url.URL
}

func (transport anthropicTokenTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.String() != "https://platform.claude.com/v1/oauth/token" {
		transport.t.Errorf("unexpected production token endpoint: %s", request.URL)
	}
	forward := request.Clone(request.Context())
	forward.URL.Scheme = transport.destination.Scheme
	forward.URL.Host = transport.destination.Host
	forward.Host = transport.destination.Host
	return http.DefaultTransport.RoundTrip(forward)
}

func newAnthropicTestProvider(t *testing.T, tokenServerURL string) *AnthropicProvider {
	t.Helper()
	destination, err := url.Parse(tokenServerURL)
	if err != nil {
		t.Fatal(err)
	}
	p := NewAnthropicProvider(&http.Client{Transport: anthropicTokenTestTransport{t: t, destination: destination}})
	p.callbackPort = 0
	p.timeout = time.Second
	return p
}

func readAnthropicTokenJSON(t *testing.T, request *http.Request) map[string]string {
	t.Helper()
	if request.Method != http.MethodPost || request.URL.Path != "/v1/oauth/token" {
		t.Errorf("unexpected token request: %s %s", request.Method, request.URL.Path)
	}
	for _, header := range []string{"Content-Type", "Accept"} {
		if request.Header.Get(header) != "application/json" {
			t.Errorf("%s=%q, want application/json", header, request.Header.Get(header))
		}
	}
	var body map[string]string
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		t.Errorf("decode token JSON: %v", err)
	}
	return body
}

func TestAnthropicOAuthProductionDefaultsMatchUpstreamV100(t *testing.T) {
	p := NewAnthropicProvider(nil)
	if p.authorizeURL != "https://claude.ai/oauth/authorize" || p.tokenURL != "https://platform.claude.com/v1/oauth/token" || p.callbackPort != 53692 || p.callbackHost != "127.0.0.1" {
		t.Fatalf("provider defaults authorize=%q token=%q host=%q port=%d", p.authorizeURL, p.tokenURL, p.callbackHost, p.callbackPort)
	}
	authURL, err := p.authorizationURL(anthropicBrowserRedirect, "state", "challenge")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme+"://"+u.Host+u.Path != "https://claude.ai/oauth/authorize" || q.Get("code") != "true" || q.Get("client_id") != "9d1c250a-e61b-44d9-88ed-5944d1962f5e" || q.Get("redirect_uri") != "http://localhost:53692/callback" || q.Get("scope") != "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload" {
		t.Fatalf("auth URL=%s", authURL)
	}
}

func TestAnthropicOAuthBrowserUsesSharedCallbackAndExchangesCode(t *testing.T) {
	var form map[string]string
	token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		form = readAnthropicTokenJSON(t, r)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "refresh_token": "refresh", "expires_in": 3600})
	}))
	defer token.Close()
	p := newAnthropicTestProvider(t, token.URL)
	var authURL string
	creds, err := p.Login(LoginCallbacks{OnAuth: func(info AuthInfo) {
		authURL = info.URL
		u, err := url.Parse(info.URL)
		if err != nil {
			t.Errorf("parse auth URL: %v", err)
			return
		}
		redirect := u.Query().Get("redirect_uri")
		state := u.Query().Get("state")
		resp, err := http.Get(redirect + "?code=browser-code&state=" + url.QueryEscape(state))
		if err != nil {
			t.Errorf("callback GET: %v", err)
			return
		}
		defer resp.Body.Close()
		page, _ := io.ReadAll(resp.Body)
		for _, expected := range []string{`<svg`, `viewBox="0 0 800 800"`, `fill="#F09082"`, `fill="#4D9ABF"`, `fill="#F1BE58"`, "Signed in to Anthropic."} {
			if !strings.Contains(string(page), expected) {
				t.Errorf("callback page missing %q", expected)
			}
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if creds.Access != "access" || creds.Refresh != "refresh" {
		t.Fatalf("creds=%#v", creds)
	}
	if !strings.Contains(authURL, "state=") {
		t.Fatalf("auth URL missing state: %s", authURL)
	}
	parsedAuth, _ := url.Parse(authURL)
	want := map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     "9d1c250a-e61b-44d9-88ed-5944d1962f5e",
		"code":          "browser-code",
		"state":         parsedAuth.Query().Get("state"),
		"redirect_uri":  parsedAuth.Query().Get("redirect_uri"),
		"code_verifier": parsedAuth.Query().Get("state"),
	}
	if want["state"] == "" || !reflect.DeepEqual(form, want) {
		t.Fatalf("token JSON=%v, want %v", form, want)
	}
}

func TestAnthropicOAuthCopyCodeUsesPlatformRedirectAndSelection(t *testing.T) {
	var form map[string]string
	token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		form = readAnthropicTokenJSON(t, r)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "refresh_token": "refresh", "expires_in": 3600})
	}))
	defer token.Close()
	p := newAnthropicTestProvider(t, token.URL)
	var authURL string
	var selectedState string
	creds, err := p.Login(LoginCallbacks{
		OnSelect: func(prompt SelectPrompt) (string, error) {
			if prompt.Default != anthropicBrowserLoginMethod || len(prompt.Options) != 2 || prompt.Options[0].Label != "Browser login (default)" || prompt.Options[1].Label != "Copy code login (headless)" {
				t.Fatalf("select prompt=%#v", prompt)
			}
			return anthropicCopyCodeLoginMethod, nil
		},
		OnAuth: func(info AuthInfo) {
			authURL = info.URL
			u, _ := url.Parse(info.URL)
			selectedState = u.Query().Get("state")
		},
		OnPrompt: func(prompt Prompt) (string, error) {
			if prompt.Placeholder != "code#state" {
				t.Fatalf("placeholder=%q", prompt.Placeholder)
			}
			return " pasted-code # " + selectedState + " \n", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if creds.Access != "access" {
		t.Fatalf("creds=%#v", creds)
	}
	u, _ := url.Parse(authURL)
	if got := u.Query().Get("redirect_uri"); got != anthropicCopyCodeRedirect {
		t.Fatalf("redirect_uri=%q", got)
	}
	want := map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     "9d1c250a-e61b-44d9-88ed-5944d1962f5e",
		"code":          "pasted-code",
		"state":         selectedState,
		"redirect_uri":  "https://platform.claude.com/oauth/code/callback",
		"code_verifier": selectedState,
	}
	if selectedState == "" || !reflect.DeepEqual(form, want) {
		t.Fatalf("token JSON=%v, want %v", form, want)
	}
}

func TestAnthropicOAuthRefreshJSONShapeAndFallback(t *testing.T) {
	for _, tc := range []struct {
		name            string
		returnedRefresh string
		wantRefresh     string
	}{
		{name: "rotate", returnedRefresh: "new-refresh", wantRefresh: "new-refresh"},
		{name: "preserve when absent", wantRefresh: "old-refresh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var captured map[string]string
			token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured = readAnthropicTokenJSON(t, r)
				response := map[string]any{"access_token": "new-access", "expires_in": 3600}
				if tc.returnedRefresh != "" {
					response["refresh_token"] = tc.returnedRefresh
				}
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer token.Close()
			p := newAnthropicTestProvider(t, token.URL)
			credentials, err := p.RefreshTokenContext(t.Context(), &Credentials{Refresh: "old-refresh"})
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]string{
				"grant_type":    "refresh_token",
				"client_id":     "9d1c250a-e61b-44d9-88ed-5944d1962f5e",
				"refresh_token": "old-refresh",
			}
			if !reflect.DeepEqual(captured, want) {
				t.Fatalf("refresh JSON=%v, want exact keys/values %v", captured, want)
			}
			if credentials.Access != "new-access" || credentials.Refresh != tc.wantRefresh || credentials.Expires <= time.Now().UnixMilli() {
				t.Fatalf("credentials=%#v", credentials)
			}
		})
	}
}

func TestAnthropicOAuthCopyCodeAcceptedInputShapes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		input     func(state string) string
		wantCode  string
		wantState func(state string) string
	}{
		{name: "raw code", input: func(state string) string { return "raw-code" }, wantCode: "raw-code", wantState: func(state string) string { return state }},
		{name: "code hash state", input: func(state string) string { return "hash-code#" + state }, wantCode: "hash-code", wantState: func(state string) string { return state }},
		{name: "query string", input: func(state string) string { return "code=query-code&state=" + url.QueryEscape(state) }, wantCode: "query-code", wantState: func(state string) string { return state }},
		{name: "callback URL", input: func(state string) string {
			return "https://platform.claude.com/oauth/code/callback?code=url-code&state=" + url.QueryEscape(state)
		}, wantCode: "url-code", wantState: func(state string) string { return state }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var form map[string]string
			token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				form = readAnthropicTokenJSON(t, r)
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "refresh_token": "refresh", "expires_in": 3600})
			}))
			defer token.Close()
			p := newAnthropicTestProvider(t, token.URL)
			var state string
			_, err := p.Login(LoginCallbacks{
				OnSelect: func(prompt SelectPrompt) (string, error) { return anthropicCopyCodeLoginMethod, nil },
				OnAuth:   func(info AuthInfo) { u, _ := url.Parse(info.URL); state = u.Query().Get("state") },
				OnPrompt: func(prompt Prompt) (string, error) { return tc.input(state), nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			if form["code"] != tc.wantCode || form["state"] != tc.wantState(state) || form["state"] != form["code_verifier"] {
				t.Fatalf("token form=%v", form)
			}
		})
	}
}

func TestAnthropicOAuthCopyCodeRejectsMismatchedStateWithoutTokenRequest(t *testing.T) {
	called := false
	token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer token.Close()
	p := newAnthropicTestProvider(t, token.URL)
	var state string
	_, err := p.Login(LoginCallbacks{
		OnSelect: func(prompt SelectPrompt) (string, error) { return anthropicCopyCodeLoginMethod, nil },
		OnAuth: func(info AuthInfo) {
			u, _ := url.Parse(info.URL)
			state = u.Query().Get("state")
		},
		OnPrompt: func(prompt Prompt) (string, error) { return "secret-code#wrong-state", nil },
	})
	if err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("err=%v", err)
	}
	if called {
		t.Fatal("token endpoint was called for invalid copy-code input")
	}
	for _, secret := range []string{"secret-code", state, "wrong-state"} {
		if secret != "" && strings.Contains(err.Error(), secret) {
			t.Fatalf("secret %q leaked in error: %v", secret, err)
		}
	}
}

func TestAnthropicOAuthCopyCodeRequiresPromptAndNoSecretLeakage(t *testing.T) {
	p := newAnthropicTestProvider(t, "http://127.0.0.1:1/token")
	_, err := p.Login(LoginCallbacks{OnSelect: func(prompt SelectPrompt) (string, error) { return anthropicCopyCodeLoginMethod, nil }})
	if err == nil || !strings.Contains(err.Error(), "requires a prompt") {
		t.Fatalf("err=%v", err)
	}
	if strings.Contains(err.Error(), "code_verifier") || strings.Contains(err.Error(), "access_token") || strings.Contains(err.Error(), "refresh_token") {
		t.Fatalf("secret leaked in error: %v", err)
	}
}

func TestAnthropicOAuthTokenErrorRedactsEndpointEchoedSecrets(t *testing.T) {
	var verifier string
	token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		form := readAnthropicTokenJSON(t, r)
		verifier = form["code_verifier"]
		code := form["code"]
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_grant",
			"error_description": "code=" + code + " code_verifier=" + verifier + " access_token=sentinel-access refresh_token=sentinel-refresh raw-body-sentinel",
			"access_token":      "sentinel-access",
			"refresh_token":     "sentinel-refresh",
		})
	}))
	defer token.Close()
	p := newAnthropicTestProvider(t, token.URL)
	var selectedState string
	_, err := p.Login(LoginCallbacks{
		OnSelect: func(prompt SelectPrompt) (string, error) { return anthropicCopyCodeLoginMethod, nil },
		OnAuth: func(info AuthInfo) {
			u, _ := url.Parse(info.URL)
			selectedState = u.Query().Get("state")
		},
		OnPrompt: func(prompt Prompt) (string, error) { return "sentinel-code#" + selectedState, nil },
	})
	if err == nil || !strings.Contains(err.Error(), "invalid_grant") || !strings.Contains(err.Error(), "token exchange failed (400)") {
		t.Fatalf("err=%v", err)
	}
	for _, secret := range []string{"sentinel-code", verifier, "sentinel-access", "sentinel-refresh"} {
		if secret != "" && strings.Contains(err.Error(), secret) {
			t.Fatalf("secret %q leaked in error: %v", secret, err)
		}
	}
	if strings.Contains(err.Error(), "raw-body-sentinel") {
		t.Fatalf("arbitrary raw body leaked in error: %v", err)
	}
}

func TestAnthropicOAuthSelectionErrors(t *testing.T) {
	p := newAnthropicTestProvider(t, "http://127.0.0.1:1/token")
	_, err := p.Login(LoginCallbacks{OnSelect: func(prompt SelectPrompt) (string, error) { return "bad-method", nil }})
	if err == nil || !strings.Contains(err.Error(), "unsupported Anthropic OAuth login method") {
		t.Fatalf("unknown selection err=%v", err)
	}
	_, err = p.Login(LoginCallbacks{OnSelect: func(prompt SelectPrompt) (string, error) { return "", context.Canceled }})
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("selection cancel err=%v", err)
	}
}

func TestAnthropicOAuthCallbackStateAndCancellation(t *testing.T) {
	callback, err := startOAuthCallbackServer(context.Background(), oauthCallbackOptions{ProviderName: "Anthropic", Host: "127.0.0.1", Port: 0, Path: "/callback", State: "good"})
	if err != nil {
		t.Fatal(err)
	}
	defer callback.Close()
	resp, err := http.Get(callback.RedirectURI + "?code=abc&state=bad")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	resp.Body.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = startOAuthCallbackServer(ctx, oauthCallbackOptions{ProviderName: "Anthropic"})
	if err == nil || !strings.Contains(err.Error(), "login cancelled") {
		t.Fatalf("cancel err=%v", err)
	}
}

func TestAnthropicOAuthBrowserCompletesBeforeSuccessAndExchangesOnce(t *testing.T) {
	for _, failExchange := range []bool{false, true} {
		t.Run(fmt.Sprint(failExchange), func(t *testing.T) {
			var requests atomic.Int32
			token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = readAnthropicTokenJSON(t, r)
				requests.Add(1)
				if failExchange {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"invalid_grant","access_token":"secret-access"}`))
					return
				}
				_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":3600}`))
			}))
			defer token.Close()
			p := newAnthropicTestProvider(t, token.URL)
			creds, err := p.Login(LoginCallbacks{OnAuth: func(info AuthInfo) {
				u, _ := url.Parse(info.URL)
				callback := u.Query().Get("redirect_uri") + "?code=callback-code&state=" + url.QueryEscape(u.Query().Get("state"))
				for i := 0; i < 2; i++ {
					response, httpErr := http.Get(callback)
					if httpErr != nil {
						t.Error(httpErr)
						return
					}
					page, _ := io.ReadAll(response.Body)
					response.Body.Close()
					if failExchange {
						if response.StatusCode != http.StatusBadGateway || strings.Contains(string(page), "Signed in") || strings.Contains(string(page), "secret-access") || strings.Contains(string(page), "callback-code") {
							t.Errorf("failed exchange response status=%d body=%s", response.StatusCode, page)
						}
					} else if response.StatusCode != http.StatusOK || !strings.Contains(string(page), "Signed in to Anthropic.") {
						t.Errorf("success response status=%d body=%s", response.StatusCode, page)
					}
					if requests.Load() != 1 {
						t.Errorf("exchanges=%d, want 1 before response", requests.Load())
					}
				}
			}})
			if failExchange {
				if err == nil || creds != nil {
					t.Fatalf("creds=%v err=%v", creds, err)
				}
			} else if err != nil || creds == nil || creds.Access != "access" {
				t.Fatalf("creds=%v err=%v", creds, err)
			}
		})
	}
}

func TestAnthropicOAuthZeroValueLoginAndRefreshUseProductionDefaults(t *testing.T) {
	var captured []map[string]string
	token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = append(captured, readAnthropicTokenJSON(t, r))
		_, _ = w.Write([]byte(`{"access_token":"access","expires_in":3600}`))
	}))
	defer token.Close()
	destination, _ := url.Parse(token.URL)
	original := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: anthropicTokenTestTransport{t: t, destination: destination}}
	defer func() { http.DefaultClient = original }()
	p := &AnthropicProvider{}
	var state string
	credentials, err := p.Login(LoginCallbacks{
		OnSelect: func(SelectPrompt) (string, error) { return anthropicCopyCodeLoginMethod, nil },
		OnAuth: func(info AuthInfo) {
			u, _ := url.Parse(info.URL)
			if u.Host != "claude.ai" || u.Query().Get("code") != "true" {
				t.Errorf("auth URL=%s", info.URL)
			}
			state = u.Query().Get("state")
		},
		OnPrompt: func(Prompt) (string, error) { return "zero-code", nil },
	})
	if err != nil || credentials == nil || credentials.Access != "access" {
		t.Fatalf("creds=%v err=%v", credentials, err)
	}
	credentials, err = p.RefreshToken(&Credentials{Refresh: "old-refresh"})
	if err != nil || credentials.Refresh != "old-refresh" {
		t.Fatalf("refresh creds=%v err=%v", credentials, err)
	}
	if len(captured) != 2 || captured[0]["state"] != state || captured[0]["code_verifier"] != state || captured[1]["grant_type"] != "refresh_token" {
		t.Fatalf("captured=%v", captured)
	}
	if !reflect.DeepEqual(p, &AnthropicProvider{}) {
		t.Fatal("zero-value provider mutated")
	}
}

func TestAnthropicOAuthBrowserListenerInjectionKeepsOfficialRedirect(t *testing.T) {
	token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := readAnthropicTokenJSON(t, r)
		if body["redirect_uri"] != "http://localhost:53692/callback" {
			t.Errorf("redirect=%s", body["redirect_uri"])
		}
		_, _ = w.Write([]byte(`{"access_token":"access","expires_in":3600}`))
	}))
	defer token.Close()
	destination, _ := url.Parse(token.URL)
	p := NewAnthropicProvider(&http.Client{Transport: anthropicTokenTestTransport{t: t, destination: destination}})
	var actualAddress string
	p.listen = func(network, address string) (net.Listener, error) {
		if address != "127.0.0.1:53692" {
			t.Errorf("listen address=%s", address)
		}
		listener, err := net.Listen(network, "127.0.0.1:0")
		if err == nil {
			actualAddress = listener.Addr().String()
		}
		return listener, err
	}
	credentials, err := p.Login(LoginCallbacks{OnAuth: func(info AuthInfo) {
		u, _ := url.Parse(info.URL)
		if u.Query().Get("redirect_uri") != "http://localhost:53692/callback" {
			t.Errorf("auth redirect=%s", info.URL)
		}
		response, err := http.Get("http://" + actualAddress + "/callback?code=browser-code&state=" + url.QueryEscape(u.Query().Get("state")))
		if err != nil {
			t.Error(err)
			return
		}
		response.Body.Close()
	}})
	if err != nil || credentials == nil {
		t.Fatalf("creds=%v err=%v", credentials, err)
	}
}
