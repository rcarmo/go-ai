package oauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestXAIOAuthDeviceFlowPendingSlowDownSuccess(t *testing.T) {
	server := newXAITestServer(t, []xaiTokenReply{
		{status: http.StatusBadRequest, body: map[string]interface{}{"error": "authorization_pending"}},
		{status: http.StatusBadRequest, body: map[string]interface{}{"error": "slow_down", "interval": 10}},
		{status: http.StatusOK, body: xaiTokenBody(map[string]interface{}{})},
	})
	provider := newTestXAIProvider(server)
	var intervals []time.Duration
	provider.pollWait = func(ctx context.Context, d time.Duration) error { intervals = append(intervals, d); return nil }
	var auth AuthInfo
	creds, err := provider.Login(LoginCallbacks{OnAuth: func(info AuthInfo) { auth = info }})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if auth.URL != "https://accounts.x.ai/oauth2/device" || !strings.Contains(auth.Instructions, "ABCD-1234") {
		t.Fatalf("unexpected auth info: %#v", auth)
	}
	if creds.Access != "access-token" || creds.Refresh != "refresh-token" || len(intervals) != 3 || intervals[0] != 5*time.Second || intervals[1] != 5*time.Second || intervals[2] != 10*time.Second {
		t.Fatalf("unexpected creds/intervals: %#v %v", creds, intervals)
	}
}

func TestXAIOAuthIntervalZeroUsesDefaultPollInterval(t *testing.T) {
	server := newXAITestServer(t, []xaiTokenReply{{status: http.StatusOK, body: xaiTokenBody(map[string]interface{}{})}})
	server.deviceOverrides = map[string]interface{}{"interval": 0}
	provider := newTestXAIProvider(server)
	var intervals []time.Duration
	provider.pollWait = func(ctx context.Context, d time.Duration) error { intervals = append(intervals, d); return nil }
	_, err := provider.Login(LoginCallbacks{})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if len(intervals) != 1 || intervals[0] != 5*time.Second {
		t.Fatalf("intervals=%v", intervals)
	}
}

func TestXAIOAuthRejectsNonHTTPSVerificationURI(t *testing.T) {
	for _, raw := range []string{"http://accounts.x.ai/oauth2/device", "file:///etc/passwd", "not a url"} {
		t.Run(raw, func(t *testing.T) {
			server := newXAITestServer(t, nil)
			server.deviceOverrides = map[string]interface{}{"verification_uri": raw}
			provider := newTestXAIProvider(server)
			_, err := provider.Login(LoginCallbacks{})
			if err == nil || !strings.Contains(err.Error(), "untrusted verification URI") {
				t.Fatalf("expected untrusted URI error, got %v", err)
			}
		})
	}
}

func TestXAIOAuthTerminalDeviceErrors(t *testing.T) {
	for _, tc := range []struct{ code, want string }{{"access_denied", "xAI device authorization was denied"}, {"authorization_denied", "xAI device authorization was denied"}, {"expired_token", "xAI device code expired"}} {
		t.Run(tc.code, func(t *testing.T) {
			server := newXAITestServer(t, []xaiTokenReply{{status: http.StatusBadRequest, body: map[string]interface{}{"error": tc.code}}})
			provider := newTestXAIProvider(server)
			provider.pollWait = immediateRadiusPollWait
			_, err := provider.Login(LoginCallbacks{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestXAIOAuthCancelsBeforeFirstTokenPoll(t *testing.T) {
	server := newXAITestServer(t, []xaiTokenReply{{status: http.StatusOK, body: xaiTokenBody(map[string]interface{}{})}})
	provider := newTestXAIProvider(server)
	ctx, cancel := context.WithCancel(t.Context())
	provider.pollWait = func(context.Context, time.Duration) error { cancel(); return ctx.Err() }
	_, err := provider.pollForTokens(ctx, &xaiDeviceCode{DeviceCode: "device-code", UserCode: "ABCD", VerificationURI: "https://accounts.x.ai/oauth2/device", Interval: 5, ExpiresIn: 900})
	if err == nil || !errors.Is(err, context.Canceled) || server.tokenRequests != 0 {
		t.Fatalf("expected cancellation before token request, requests=%d err=%v", server.tokenRequests, err)
	}
}

func TestXAIOAuthRefreshPreservesUnrotatedRefreshAndDefaultLifetime(t *testing.T) {
	server := newXAITestServer(t, []xaiTokenReply{
		{status: http.StatusOK, body: xaiTokenBody(map[string]interface{}{"access_token": "new-access", "refresh_token": "new-refresh"})},
		{status: http.StatusOK, body: xaiTokenBody(map[string]interface{}{"access_token": "newer-access", "refresh_token": nil, "expires_in": nil})},
	})
	provider := newTestXAIProvider(server)
	rotated, err := provider.RefreshToken(&Credentials{Refresh: "old-refresh"})
	if err != nil {
		t.Fatalf("refresh rotated: %v", err)
	}
	preserved, err := provider.RefreshToken(&Credentials{Refresh: "keep-refresh"})
	if err != nil {
		t.Fatalf("refresh preserved: %v", err)
	}
	if rotated.Access != "new-access" || rotated.Refresh != "new-refresh" || preserved.Access != "newer-access" || preserved.Refresh != "keep-refresh" {
		t.Fatalf("unexpected refresh results: %#v %#v", rotated, preserved)
	}
}

func TestXAIOAuthRefreshErrors(t *testing.T) {
	t.Run("missing access", func(t *testing.T) {
		server := newXAITestServer(t, []xaiTokenReply{{status: http.StatusOK, body: xaiTokenBody(map[string]interface{}{"access_token": nil})}})
		_, err := newTestXAIProvider(server).RefreshToken(&Credentials{Refresh: "old"})
		if err == nil || !strings.Contains(err.Error(), "invalid xAI OAuth response field: access_token") {
			t.Fatalf("unexpected err: %v", err)
		}
	})
	t.Run("upstream failure", func(t *testing.T) {
		server := newXAITestServer(t, []xaiTokenReply{{status: http.StatusBadRequest, body: map[string]interface{}{"error": "invalid_grant", "error_description": "refresh token revoked"}}})
		_, err := newTestXAIProvider(server).RefreshToken(&Credentials{Refresh: "old"})
		if err == nil || !strings.Contains(err.Error(), "xAI OAuth token refresh failed (HTTP 400): invalid_grant: refresh token revoked") {
			t.Fatalf("unexpected err: %v", err)
		}
	})
}

type xaiTokenReply struct {
	status int
	body   map[string]interface{}
}
type xaiTestServer struct {
	*httptest.Server
	tokenReplies    []xaiTokenReply
	tokenRequests   int
	deviceOverrides map[string]interface{}
}

func newXAITestServer(t *testing.T, replies []xaiTokenReply) *xaiTestServer {
	t.Helper()
	state := &xaiTestServer{tokenReplies: replies}
	mux := http.NewServeMux()
	mux.HandleFunc("/device/code", func(w http.ResponseWriter, r *http.Request) {
		form := readXAIForm(t, r)
		if form.Get("client_id") != xaiClientID || form.Get("scope") != xaiScope || form.Get("referrer") != "pi" {
			t.Fatalf("unexpected device form: %s", form.Encode())
		}
		body := map[string]interface{}{"device_code": "device-code", "user_code": "ABCD-1234", "verification_uri": "https://accounts.x.ai/oauth2/device", "expires_in": 900, "interval": 5}
		for k, v := range state.deviceOverrides {
			body[k] = v
		}
		writeJSON(t, w, body)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		form := readXAIForm(t, r)
		if form.Get("client_id") != xaiClientID {
			t.Fatalf("missing client id: %s", form.Encode())
		}
		idx := state.tokenRequests
		state.tokenRequests++
		if idx >= len(state.tokenReplies) {
			idx = len(state.tokenReplies) - 1
		}
		reply := state.tokenReplies[idx]
		if reply.status != 0 {
			w.WriteHeader(reply.status)
		}
		writeJSON(t, w, reply.body)
	})
	state.Server = httptest.NewServer(mux)
	t.Cleanup(state.Close)
	return state
}

func newTestXAIProvider(server *xaiTestServer) *XAIProvider {
	p := NewXAIProvider(server.Client())
	p.deviceCodeURL = server.URL + "/device/code"
	p.tokenURL = server.URL + "/token"
	return p
}

func xaiTokenBody(overrides map[string]interface{}) map[string]interface{} {
	body := map[string]interface{}{"access_token": "access-token", "refresh_token": "refresh-token", "expires_in": float64(21600), "token_type": "Bearer"}
	for k, v := range overrides {
		if v == nil {
			delete(body, k)
		} else {
			body[k] = v
		}
	}
	return body
}

func readXAIForm(t *testing.T, r *http.Request) url.Values {
	t.Helper()
	if err := r.ParseForm(); err != nil {
		t.Fatalf("parse form: %v", err)
	}
	return r.Form
}
