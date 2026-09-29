package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestV0991OpenAIChatGPTOAuthManualCallbackLoginAndRefresh(t *testing.T) {
	var tokenForms []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		form := url.Values{}
		for k, v := range r.Form {
			form[k] = append([]string(nil), v...)
		}
		tokenForms = append(tokenForms, form)
		if form.Get("resource") != openAIChatGPTResource {
			t.Fatalf("resource=%q", form.Get("resource"))
		}
		switch form.Get("grant_type") {
		case "authorization_code":
			if form.Get("client_id") != "issued-client" || form.Get("code") != "auth-code" || form.Get("code_verifier") == "" || form.Get("redirect_uri") == "" {
				t.Fatalf("bad auth-code form: %#v", form)
			}
			writeJSON(t, w, map[string]any{"access_token": "access-1", "refresh_token": "refresh-1", "id_token": "id-1", "expires_in": 3600, "scope": openAIChatGPTScope})
		case "refresh_token":
			if form.Get("client_id") != "issued-client" || form.Get("refresh_token") != "refresh-1" {
				t.Fatalf("bad refresh form: %#v", form)
			}
			writeJSON(t, w, map[string]any{"access_token": "access-2", "refresh_token": "refresh-2", "expires_in": 7200, "scope": openAIChatGPTScope})
		default:
			t.Fatalf("grant_type=%q", form.Get("grant_type"))
		}
	}))
	defer server.Close()

	provider := NewOpenAIChatGPTProvider(server.Client())
	provider.authorizeURL = server.URL + "/authorize"
	provider.tokenURL = server.URL + "/token"
	provider.callbackPort = 0
	provider.deviceID = "12345678-1234-1234-1234-123456789abc"

	var auth AuthInfo
	var progress []string
	creds, err := provider.Login(LoginCallbacks{
		OnAuth:     func(info AuthInfo) { auth = info },
		OnProgress: func(message string) { progress = append(progress, message) },
		OnPrompt: func(prompt Prompt) (string, error) {
			if auth.URL == "" {
				t.Fatal("OnPrompt ran before OnAuth")
			}
			authURL, err := url.Parse(auth.URL)
			if err != nil {
				return "", err
			}
			q := authURL.Query()
			if q.Get("client_id") != openAIChatGPTDynamicClientID || q.Get("agent_name_hint") != openAIChatGPTAgentNameHint || q.Get("ext_agent_host_id") != "urn:uuid:12345678-1234-1234-1234-123456789abc" || q.Get("scope") != openAIChatGPTScope || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("nonce") == "" {
				t.Fatalf("bad auth URL query: %s", authURL.RawQuery)
			}
			return q.Get("redirect_uri") + "?code=auth-code&state=" + url.QueryEscape(q.Get("state")) + "&client_id=issued-client", nil
		},
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if creds.Access != "access-1" || creds.Refresh != "refresh-1" || creds.Expires <= time.Now().UnixMilli() || openAIChatGPTCredentialClientID(creds) != "issued-client" || provider.GetAPIKey(creds) != "access-1" {
		t.Fatalf("unexpected creds: %#v", creds)
	}
	if len(progress) == 0 || !strings.Contains(progress[len(progress)-1], "Exchanging authorization code") {
		t.Fatalf("missing exchange progress: %#v", progress)
	}

	refreshed, err := provider.RefreshTokenContext(context.Background(), creds)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refreshed.Access != "access-2" || refreshed.Refresh != "refresh-2" || openAIChatGPTCredentialClientID(refreshed) != "issued-client" || len(tokenForms) != 2 {
		t.Fatalf("unexpected refresh creds=%#v forms=%#v", refreshed, tokenForms)
	}
}

func TestV0991OpenAIChatGPTCallbackServerCompletesAndRejectsStateMismatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	callback, err := startOAuthCallbackServer(ctx, oauthCallbackOptions{ProviderName: "ChatGPT", Host: "127.0.0.1", Port: 0, Path: "/auth/callback", State: "good-state"})
	if err != nil {
		t.Fatalf("callback server: %v", err)
	}
	defer callback.Close()

	badResp, err := http.Get(callback.RedirectURI + "?code=auth-code&state=bad-state&client_id=issued-client")
	if err != nil {
		t.Fatal(err)
	}
	badResp.Body.Close()
	if badResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad state status=%d", badResp.StatusCode)
	}

	done := make(chan struct{})
	var got *url.URL
	var waitErr error
	go func() {
		got, waitErr = callback.Wait()
		close(done)
	}()
	goodResp, err := http.Get(callback.RedirectURI + "?code=auth-code&state=good-state&client_id=issued-client")
	if err != nil {
		t.Fatal(err)
	}
	goodResp.Body.Close()
	if goodResp.StatusCode != http.StatusOK {
		t.Fatalf("good callback status=%d", goodResp.StatusCode)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("callback wait timed out")
	}
	if waitErr != nil || got.Query().Get("code") != "auth-code" || got.Query().Get("client_id") != "issued-client" {
		t.Fatalf("callback result url=%v err=%v", got, waitErr)
	}
}

func TestV0991OpenAIChatGPTTokenValidation(t *testing.T) {
	_, err := openAIChatGPTCredentialFromTokenResponse(map[string]any{"access_token": "access", "refresh_token": "refresh", "expires_in": 3600, "scope": "openid profile"}, "client")
	if err == nil || !strings.Contains(err.Error(), openAIChatGPTDirectTokenScope) {
		t.Fatalf("expected direct-token scope error, got %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"access_token": "access", "refresh_token": "refresh", "expires_in": 3600, "scope": openAIChatGPTScope})
	}))
	defer server.Close()
	provider := NewOpenAIChatGPTProvider(server.Client())
	provider.tokenURL = server.URL
	_, err = provider.requestToken(context.Background(), url.Values{"grant_type": {"authorization_code"}}, "client", true)
	if err == nil || !strings.Contains(err.Error(), "ID token") {
		t.Fatalf("expected ID token validation error, got %v", err)
	}
}

func TestV0991OpenAIChatGPTCredentialJSONPreservesClientIDAndScopes(t *testing.T) {
	creds, err := openAIChatGPTCredentialFromTokenResponse(map[string]any{"access_token": "access", "refresh_token": "refresh", "id_token": "id", "expires_in": 3600, "scope": openAIChatGPTScope}, "client-1")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(creds)
	if err != nil {
		t.Fatal(err)
	}
	var round Credentials
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatal(err)
	}
	if openAIChatGPTCredentialClientID(&round) != "client-1" || round.Access != "access" || round.Refresh != "refresh" {
		t.Fatalf("roundtrip creds=%#v json=%s", round, data)
	}
}
