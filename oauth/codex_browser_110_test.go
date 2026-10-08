package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"syscall"
	"testing"
	"time"
)

func TestCodex110BrowserOriginatorPKCEManualAndRefresh(t *testing.T) {
	claims, _ := json.Marshal(map[string]any{"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "account"}})
	access := "header." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		r.ParseForm()
		if r.Form.Get("client_id") != codexBrowserClientID {
			t.Error(r.Form)
		}
		if requests == 1 && (r.Form.Get("code") != "manual" || r.Form.Get("redirect_uri") != codexBrowserRedirect || r.Form.Get("code_verifier") == "") {
			t.Error(r.Form)
		}
		if requests == 2 && r.Form.Get("grant_type") != "refresh_token" {
			t.Error(r.Form)
		}
		json.NewEncoder(w).Encode(map[string]any{"access_token": access, "refresh_token": "refresh", "expires_in": 3600})
	}))
	defer server.Close()
	p := &OpenAICodexProvider{client: server.Client(), tokenURL: server.URL, listen: func(string, string) (net.Listener, error) { return nil, syscall.EADDRINUSE }}
	for _, name := range []*string{nil, func() *string { s := "host"; return &s }(), func() *string { s := ""; return &s }()} {
		raw, e := p.browserAuthorizationURL("state", "challenge", name)
		if e != nil {
			t.Fatal(e)
		}
		u, _ := url.Parse(raw)
		want := "pi"
		if name != nil {
			want = *name
		}
		if u.Query().Get("originator") != want || u.Query().Get("state") != "state" || u.Query().Get("code_challenge") != "challenge" {
			t.Fatal(u)
		}
	}
	var state string
	name := "my-agent"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	creds, e := p.loginBrowser(ctx, LoginCallbacks{AgentName: &name, OnAuth: func(info AuthInfo) {
		u, _ := url.Parse(info.URL)
		state = u.Query().Get("state")
		if u.Query().Get("originator") != name {
			t.Error(u)
		}
	}, OnPromptContext: func(context.Context, Prompt) (string, error) {
		return codexBrowserRedirect + "?" + url.Values{"code": {"manual"}, "state": {state}}.Encode(), nil
	}})
	if e != nil || creds.Extra["accountId"] != "account" || requests != 1 {
		t.Fatal(creds, e, requests)
	}
	if _, e = p.RefreshTokenContext(ctx, creds); e != nil || requests != 2 {
		t.Fatal(e, requests)
	}
}
