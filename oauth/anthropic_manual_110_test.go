package oauth

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"syscall"
	"testing"
	"time"
)

func TestAnthropic110NoCallbackServerManualRedirectAndState(t *testing.T) {
	for _, badState := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "mismatch"}[badState], func(t *testing.T) {
			exchanges := 0
			var redirect, state string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				exchanges++
				var body map[string]any
				if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
					t.Error(e)
				}
				if body["redirect_uri"] != redirect || body["state"] != state || body["code"] != "manual-code" {
					t.Error(body)
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":3600}`))
			}))
			defer server.Close()
			p := NewAnthropicProvider(server.Client())
			p.tokenURL = server.URL
			p.listen = func(string, string) (net.Listener, error) { return nil, syscall.EACCES }
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			credentials, e := p.loginContext(ctx, LoginCallbacks{OnAuth: func(info AuthInfo) {
				u, e := url.Parse(info.URL)
				if e != nil {
					t.Fatal(e)
				}
				redirect = u.Query().Get("redirect_uri")
				state = u.Query().Get("state")
			}, OnPromptContext: func(ctx context.Context, prompt Prompt) (string, error) {
				if prompt.Placeholder != redirect || redirect != anthropicBrowserRedirect {
					t.Fatal(prompt, redirect)
				}
				pasted := state
				if badState {
					pasted = "wrong"
				}
				return redirect + "?" + url.Values{"code": {"manual-code"}, "state": {pasted}}.Encode(), nil
			}})
			if badState {
				if e == nil || exchanges != 0 {
					t.Fatal(credentials, e, exchanges)
				}
			} else if e != nil || credentials.Access != "access" || exchanges != 1 {
				t.Fatal(credentials, e, exchanges)
			}
		})
	}
}
