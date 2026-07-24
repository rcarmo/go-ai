package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenRouterOAuthKeyExchange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["code"] != "code-1" || body["code_verifier"] == "" || body["code_challenge_method"] != "S256" {
			t.Fatalf("unexpected body: %#v", body)
		}
		writeJSON(t, w, map[string]string{"key": "sk-or-test"})
	}))
	defer server.Close()
	p := NewOpenRouterProvider(server.Client())
	p.tokenURL = server.URL
	creds, err := p.exchangeAuthorizationCode(context.Background(), "code-1", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	if creds.Access != "sk-or-test" || p.GetAPIKey(creds) != "sk-or-test" {
		t.Fatalf("creds=%#v", creds)
	}
}

func TestOpenRouterOAuthKeyExchangeErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(t, w, map[string]string{"error": "bad_code"})
	}))
	defer server.Close()
	p := NewOpenRouterProvider(server.Client())
	p.tokenURL = server.URL
	_, err := p.exchangeAuthorizationCode(context.Background(), "code", "verifier")
	if err == nil || !strings.Contains(err.Error(), "OpenRouter OAuth key exchange failed (HTTP 400): bad_code") {
		t.Fatalf("err=%v", err)
	}
}

func TestKimiCodingOAuthDeviceFlowAndRefresh(t *testing.T) {
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/oauth/device_authorization":
			writeJSON(t, w, map[string]interface{}{"device_code": "dev", "user_code": "USER", "verification_uri": "https://auth.kimi.com/verify", "verification_uri_complete": "https://auth.kimi.com/verify?user_code=USER", "interval": 1, "expires_in": 60})
		case "/api/oauth/token":
			_ = r.ParseForm()
			if r.Form.Get("client_id") != kimiCodeClientID {
				t.Fatalf("client_id=%q", r.Form.Get("client_id"))
			}
			if r.Form.Get("grant_type") == "refresh_token" {
				writeJSON(t, w, map[string]interface{}{"access_token": "access-refresh", "refresh_token": "refresh-2", "expires_in": 3600})
				return
			}
			polls++
			if polls == 1 {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(t, w, map[string]string{"error": "authorization_pending"})
				return
			}
			writeJSON(t, w, map[string]interface{}{"access_token": "access-1", "refresh_token": "refresh-1", "expires_in": 3600})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	p := NewKimiCodingProvider(server.Client())
	p.oauthURL = server.URL
	p.pollWait = func(context.Context, time.Duration) error { return nil }
	var auth AuthInfo
	creds, err := p.Login(LoginCallbacks{OnAuth: func(info AuthInfo) { auth = info }})
	if err != nil {
		t.Fatal(err)
	}
	if auth.URL == "" || creds.Access != "access-1" || creds.Refresh != "refresh-1" {
		t.Fatalf("auth=%#v creds=%#v", auth, creds)
	}
	refreshed, err := p.RefreshToken(creds)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Access != "access-refresh" || refreshed.Refresh != "refresh-2" {
		t.Fatalf("refreshed=%#v", refreshed)
	}
}

func TestKimiCodingOAuthRejectsUntrustedVerificationURI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]interface{}{"device_code": "dev", "user_code": "USER", "verification_uri": "file:///tmp/x", "verification_uri_complete": "file:///tmp/x", "interval": 1, "expires_in": 60})
	}))
	defer server.Close()
	p := NewKimiCodingProvider(server.Client())
	p.oauthURL = server.URL
	_, err := p.Login(LoginCallbacks{})
	if err == nil || !strings.Contains(err.Error(), "invalid Kimi Code device authorization response") {
		t.Fatalf("err=%v", err)
	}
}
