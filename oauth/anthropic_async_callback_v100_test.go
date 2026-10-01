package oauth

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// Port of the auditor's external async-browser lifecycle fixture. OnAuth launches
// the browser request then returns without waiting for its HTTP response.
func TestV100AnthropicAsyncBrowserCallbackFlushesBeforeLoginCloses(t *testing.T) {
	for _, failExchange := range []bool{false, true} {
		t.Run(fmt.Sprint(failExchange), func(t *testing.T) {
			for iteration := 0; iteration < 30; iteration++ {
				var exchanges atomic.Int32
				token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					form := readAnthropicTokenJSON(t, r)
					exchanges.Add(1)
					if failExchange {
						w.WriteHeader(http.StatusBadRequest)
						_, _ = fmt.Fprintf(w, `{"error":"invalid_grant","error_description":"code=%s state=%s access_token=sentinel-access"}`, form["code"], form["state"])
						return
					}
					_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":3600}`))
				}))
				p := newAnthropicTestProvider(t, token.URL)
				type receipt struct {
					status int
					body   string
					err    error
				}
				done := make(chan receipt, 1)
				credentials, err := p.Login(LoginCallbacks{OnAuth: func(info AuthInfo) {
					go func() {
						u, _ := url.Parse(info.URL)
						response, err := http.Get(u.Query().Get("redirect_uri") + "?code=async-code&state=" + url.QueryEscape(u.Query().Get("state")))
						if err != nil {
							done <- receipt{err: err}
							return
						}
						defer response.Body.Close()
						body, readErr := io.ReadAll(response.Body)
						done <- receipt{status: response.StatusCode, body: string(body), err: readErr}
					}()
				}})
				page := <-done
				token.Close()
				if page.err != nil {
					t.Fatalf("iteration=%d browser read failed: %v", iteration, page.err)
				}
				if exchanges.Load() != 1 {
					t.Fatalf("iteration=%d exchanges=%d", iteration, exchanges.Load())
				}
				if failExchange {
					if err == nil || credentials != nil || page.status != http.StatusBadGateway || strings.Contains(page.body, "Signed in") || strings.Contains(page.body, "async-code") || strings.Contains(page.body, "sentinel-access") {
						t.Fatalf("iteration=%d failed login creds=%v err=%v status=%d body=%q", iteration, credentials, err, page.status, page.body)
					}
				} else if err != nil || credentials == nil || page.status != http.StatusOK || !strings.Contains(page.body, "Signed in to Anthropic.") {
					t.Fatalf("iteration=%d login=%v creds=%v status=%d body=%q", iteration, err, credentials, page.status, page.body)
				}
			}
		})
	}
}
