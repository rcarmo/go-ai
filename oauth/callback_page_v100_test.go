package oauth

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	oauthCallbackLogoPathA = `<path fill="#F09082" d="M165.29 165.29H517.36V400H400V282.65H165.29Z"/>`
	oauthCallbackLogoPathB = `<path fill="#4D9ABF" d="M165.29 282.65H282.65V400H400V517.36H282.65V634.72H165.29Z"/>`
	oauthCallbackLogoPathC = `<path fill="#F1BE58" d="M517.36 400H634.72V634.72H517.36Z"/>`
)

func TestV100OAuthCallbackPageSuccessResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	body, resp, callbackURL := fetchOAuthCallbackPage(t, ctx, "Anthropic")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("content-type=%q", got)
	}
	if callbackURL.Query().Get("code") != "auth-code" {
		t.Fatalf("callback code=%q", callbackURL.Query().Get("code"))
	}
	for _, want := range []string{
		`<title>Authentication successful</title>`,
		`<h1>Authentication successful</h1>`,
		`<p>Signed in to Anthropic.</p>`,
		`viewBox="0 0 800 800"`,
		oauthCallbackLogoPathA,
		oauthCallbackLogoPathB,
		oauthCallbackLogoPathC,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("response missing %q\nbody=%s", want, body)
		}
	}
}

func TestV100OAuthCallbackPageEscapesProviderName(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	providerName := `<img src=x onerror=alert(1)>&"'`
	body, resp, _ := fetchOAuthCallbackPage(t, ctx, providerName)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("content-type=%q", got)
	}
	if !strings.Contains(body, `Signed in to &lt;img src=x onerror=alert(1)&gt;&amp;&#34;&#39;.`) {
		t.Fatalf("escaped provider not found in body=%s", body)
	}
	if strings.Contains(body, providerName) || strings.Contains(body, `Signed in to <img`) {
		t.Fatalf("raw provider markup leaked in body=%s", body)
	}
}

func fetchOAuthCallbackPage(t *testing.T, ctx context.Context, providerName string) (string, *http.Response, *url.URL) {
	t.Helper()

	callback, err := startOAuthCallbackServer(ctx, oauthCallbackOptions{ProviderName: providerName, Host: "127.0.0.1", Port: 0, Path: "/auth/callback", State: "good-state"})
	if err != nil {
		t.Fatalf("callback server: %v", err)
	}
	defer callback.Close()

	waitDone := make(chan struct{})
	var callbackURL *url.URL
	var waitErr error
	go func() {
		callbackURL, waitErr = callback.Wait()
		close(waitDone)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, callback.RedirectURI+"?code=auth-code&state=good-state", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http get: %v", err)
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	select {
	case <-waitDone:
	case <-ctx.Done():
		t.Fatalf("callback wait: %v", ctx.Err())
	}
	if waitErr != nil {
		t.Fatalf("callback wait err: %v", waitErr)
	}
	return string(bodyBytes), resp, callbackURL
}
