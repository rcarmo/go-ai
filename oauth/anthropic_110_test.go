package oauth

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestAnthropic110BrowserPreferredPortFailureFallsBack(t *testing.T) {
	for _, failure := range []error{syscall.EADDRINUSE, syscall.EACCES} {
		t.Run(failure.Error(), func(t *testing.T) {
			var authRedirect, tokenRedirect string
			var listens []string
			token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				tokenRedirect = body["redirect_uri"]
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "expires_in": 3600})
			}))
			defer token.Close()
			provider := NewAnthropicProvider(nil)
			provider.tokenURL = token.URL
			provider.listen = func(network, address string) (net.Listener, error) {
				listens = append(listens, address)
				if len(listens) == 1 {
					return nil, failure
				}
				return net.Listen(network, "127.0.0.1:0")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			result, err := provider.loginContext(ctx, LoginCallbacks{OnAuth: func(info AuthInfo) {
				u, parseErr := url.Parse(info.URL)
				if parseErr != nil {
					t.Error(parseErr)
					return
				}
				authRedirect = u.Query().Get("redirect_uri")
				redirect, _ := url.Parse(authRedirect)
				if redirect.Hostname() != "localhost" || redirect.Port() == "53692" || redirect.Port() == "0" {
					t.Error("fallback redirect invalid", authRedirect)
				}
				redirect.Host = net.JoinHostPort("127.0.0.1", redirect.Port())
				q := redirect.Query()
				q.Set("code", "test-code")
				q.Set("state", u.Query().Get("state"))
				redirect.RawQuery = q.Encode()
				response, callErr := http.Get(redirect.String())
				if callErr != nil {
					t.Error(callErr)
					return
				}
				response.Body.Close()
				if response.StatusCode != 200 {
					t.Error(response.Status)
				}
			}})
			if err != nil || result == nil || result.Access != "access" {
				t.Fatal(result, err)
			}
			if len(listens) != 2 || listens[0] != "127.0.0.1:53692" || listens[1] != "127.0.0.1:0" || authRedirect != tokenRedirect || !strings.HasSuffix(authRedirect, "/callback") {
				t.Fatal("redirect not consistent", listens, authRedirect, tokenRedirect)
			}
		})
	}
}
