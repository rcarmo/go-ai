package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestMetaOAuthLoginMintsAPIKey(t *testing.T) {
	var sawDevice, sawToken, sawMint bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device":
			sawDevice = true
			body, _ := url.ParseQuery(readBody(t, r))
			if body.Get("client_id") != metaClientID {
				t.Fatalf("device client_id=%q", body.Get("client_id"))
			}
			writeJSON(t, w, map[string]interface{}{"device_code": "device-1", "user_code": "ABCD-1234", "verification_uri_complete": metaServerURL(r) + "/verify?code=ABCD-1234", "expires_in": 60, "interval": 1})
		case "/token":
			sawToken = true
			body, _ := url.ParseQuery(readBody(t, r))
			if body.Get("grant_type") != radiusDeviceGrant || body.Get("client_id") != metaClientID || body.Get("device_code") != "device-1" {
				t.Fatalf("unexpected token form: %#v", body)
			}
			writeJSON(t, w, map[string]interface{}{"access_token": "identity-token"})
		case "/mint":
			sawMint = true
			if got := r.Header.Get("Authorization"); got != "Bearer identity-token" {
				t.Fatalf("mint authorization=%q", got)
			}
			if got := r.Header.Get("x-api-version"); got != "1.0.0" {
				t.Fatalf("x-api-version=%q", got)
			}
			writeJSON(t, w, map[string]interface{}{"api_key": "LLM|minted-key"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := NewMetaProvider(server.Client())
	provider.deviceAuthorizationURL = server.URL + "/device"
	provider.deviceTokenURL = server.URL + "/token"
	provider.apiKeyMintURL = server.URL + "/mint"
	provider.pollWait = func(context.Context, time.Duration) error { return nil }
	creds, err := provider.Login(LoginCallbacks{})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if !sawDevice || !sawToken || !sawMint {
		t.Fatalf("saw device/token/mint = %v/%v/%v", sawDevice, sawToken, sawMint)
	}
	if creds.Refresh != "identity-token" || creds.Access != "LLM|minted-key" || creds.Expires <= time.Now().UnixMilli() {
		t.Fatalf("credentials=%#v", creds)
	}
}

func TestMetaOAuthRefreshRemintsAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer identity-token" {
			t.Fatalf("authorization=%q", got)
		}
		writeJSON(t, w, map[string]interface{}{"api_key": "LLM|new-key"})
	}))
	defer server.Close()

	provider := NewMetaProvider(server.Client())
	provider.apiKeyMintURL = server.URL
	creds, err := provider.RefreshToken(&Credentials{Refresh: "identity-token", Access: "old"})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if creds.Refresh != "identity-token" || creds.Access != "LLM|new-key" {
		t.Fatalf("credentials=%#v", creds)
	}
}

func TestMetaOAuthMintSetupErrorIncludesActionURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]interface{}{"action_url": "http://" + r.Host + "/setup"})
	}))
	defer server.Close()

	provider := NewMetaProvider(server.Client())
	provider.apiKeyMintURL = server.URL
	_, err := provider.RefreshToken(&Credentials{Refresh: "identity-token"})
	if err == nil || !containsString(err.Error(), "Complete setup at") {
		t.Fatalf("expected setup error, got %v", err)
	}
}

func TestMetaOAuthUsesMintedKeyAsAPIKey(t *testing.T) {
	provider := NewMetaProvider(nil)
	if got := provider.GetAPIKey(&Credentials{Refresh: "identity-token", Access: "LLM|key"}); got != "LLM|key" {
		t.Fatalf("api key=%q", got)
	}
}

func metaServerURL(r *http.Request) string { return "http://" + r.Host }

func containsString(s, substr string) bool { return strings.Contains(s, substr) }
