// Anthropic OAuth provider — authorization code flow with local callback server.
package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	goai "github.com/rcarmo/go-ai"
)

const (
	anthropicAuthURL  = "https://console.anthropic.com/oauth/authorize"
	anthropicTokenURL = "https://console.anthropic.com/oauth/token"
	anthropicClientID = "9d9e5f78-76ca-4484-be3c-e13fb3a3378c"
	anthropicRedirect = "http://localhost:19139/callback"
)

// AnthropicProvider implements the OAuth flow for Anthropic (Claude Pro/Max).
type AnthropicProvider struct{}

func init() {
	RegisterProvider(&AnthropicProvider{})
}

func (p *AnthropicProvider) ID() string   { return "anthropic" }
func (p *AnthropicProvider) Name() string { return "Anthropic" }

func (p *AnthropicProvider) Login(callbacks LoginCallbacks) (*Credentials, error) {
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}

	// Build authorization URL
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {anthropicClientID},
		"redirect_uri":          {anthropicRedirect},
		"scope":                 {"user:inference"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	authURL := anthropicAuthURL + "?" + params.Encode()

	// Start local callback server
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	listener, err := net.Listen("tcp", "localhost:19139")
	if err != nil {
		return nil, fmt.Errorf("callback server: %w", err)
	}

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			code := r.URL.Query().Get("code")
			if code != "" {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprintf(w, "<html><body><h2>Authentication successful</h2><p>You can close this window.</p></body></html>")
				codeCh <- code
			} else {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprintf(w, "<html><body><h2>Authentication failed</h2></body></html>")
				errCh <- fmt.Errorf("no code in callback: %s", r.URL.Query().Get("error"))
			}
		}),
	}

	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	// Direct user to auth URL
	callbacks.OnAuth(AuthInfo{URL: authURL, Instructions: "Complete login in your browser"})

	// Wait for code
	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return nil, err
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("authentication timed out")
	}

	// Exchange code for tokens
	return exchangeAnthropicCode(code, verifier)
}

func (p *AnthropicProvider) RefreshToken(creds *Credentials) (*Credentials, error) {
	return refreshAnthropicToken(creds.Refresh)
}

func (p *AnthropicProvider) GetAPIKey(creds *Credentials) string {
	return creds.Access
}

func (p *AnthropicProvider) ModifyModels(models []*goai.Model, creds *Credentials) []*goai.Model {
	return models
}

func exchangeAnthropicCode(code, verifier string) (*Credentials, error) {
	body := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {anthropicClientID},
		"code":          {code},
		"redirect_uri":  {anthropicRedirect},
		"code_verifier": {verifier},
	}

	resp, err := http.Post(anthropicTokenURL, "application/x-www-form-urlencoded", strings.NewReader(body.Encode()))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, string(b))
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &Credentials{
		Refresh: result.RefreshToken,
		Access:  result.AccessToken,
		Expires: time.Now().Add(time.Duration(result.ExpiresIn)*time.Second - 5*time.Minute).UnixMilli(),
	}, nil
}

func refreshAnthropicToken(refreshToken string) (*Credentials, error) {
	body := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {anthropicClientID},
		"refresh_token": {refreshToken},
	}

	resp, err := http.Post(anthropicTokenURL, "application/x-www-form-urlencoded", strings.NewReader(body.Encode()))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("token refresh failed (%d): %s", resp.StatusCode, string(b))
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	refresh := result.RefreshToken
	if refresh == "" {
		refresh = refreshToken
	}

	return &Credentials{
		Refresh: refresh,
		Access:  result.AccessToken,
		Expires: time.Now().Add(time.Duration(result.ExpiresIn)*time.Second - 5*time.Minute).UnixMilli(),
	}, nil
}
