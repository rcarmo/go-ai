// Google Gemini CLI OAuth — authorization code flow for Cloud Code Assist.
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
	googleAuthURL    = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL   = "https://oauth2.googleapis.com/token"
	geminiCLIClientID = "962486aborv6v47vfgk7feun3q" // Cloud Code Assist client
	geminiCLIRedirect = "http://localhost:19140/callback"
	geminiCLIScopes   = "openid https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/generative-language"
)

// GeminiCLIProvider implements Google OAuth for Gemini CLI / Cloud Code Assist.
type GeminiCLIProvider struct{}

func init() {
	RegisterProvider(&GeminiCLIProvider{})
}

func (p *GeminiCLIProvider) ID() string   { return "google-gemini-cli" }
func (p *GeminiCLIProvider) Name() string { return "Google Gemini CLI" }

func (p *GeminiCLIProvider) Login(callbacks LoginCallbacks) (*Credentials, error) {
	// Prompt for project ID
	projectID, err := callbacks.OnPrompt(Prompt{
		Message:     "Google Cloud project ID",
		Placeholder: "my-project-id",
	})
	if err != nil {
		return nil, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, fmt.Errorf("project ID is required")
	}

	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}

	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {geminiCLIClientID},
		"redirect_uri":          {geminiCLIRedirect},
		"scope":                 {geminiCLIScopes},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
	}
	authURL := googleAuthURL + "?" + params.Encode()

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	listener, err := net.Listen("tcp", "localhost:19140")
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
				errCh <- fmt.Errorf("no code: %s", r.URL.Query().Get("error"))
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

	callbacks.OnAuth(AuthInfo{URL: authURL, Instructions: "Complete Google login in your browser"})

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return nil, err
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("authentication timed out")
	}

	creds, err := exchangeGoogleCode(code, verifier)
	if err != nil {
		return nil, err
	}
	creds.Extra = map[string]interface{}{"projectId": projectID}
	return creds, nil
}

func (p *GeminiCLIProvider) RefreshToken(creds *Credentials) (*Credentials, error) {
	newCreds, err := refreshGoogleToken(creds.Refresh)
	if err != nil {
		return nil, err
	}
	newCreds.Extra = creds.Extra // preserve projectId
	return newCreds, nil
}

func (p *GeminiCLIProvider) GetAPIKey(creds *Credentials) string {
	// Return JSON {token, projectId} as expected by the Gemini CLI provider
	projectID := ""
	if creds.Extra != nil {
		if p, ok := creds.Extra["projectId"].(string); ok {
			projectID = p
		}
	}
	b, _ := json.Marshal(map[string]string{
		"token":     creds.Access,
		"projectId": projectID,
	})
	return string(b)
}

func (p *GeminiCLIProvider) ModifyModels(models []*goai.Model, creds *Credentials) []*goai.Model {
	return models
}

func exchangeGoogleCode(code, verifier string) (*Credentials, error) {
	body := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {geminiCLIClientID},
		"code":          {code},
		"redirect_uri":  {geminiCLIRedirect},
		"code_verifier": {verifier},
	}

	resp, err := http.Post(googleTokenURL, "application/x-www-form-urlencoded", strings.NewReader(body.Encode()))
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

func refreshGoogleToken(refreshToken string) (*Credentials, error) {
	body := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {geminiCLIClientID},
		"refresh_token": {refreshToken},
	}

	resp, err := http.Post(googleTokenURL, "application/x-www-form-urlencoded", strings.NewReader(body.Encode()))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("token refresh failed (%d): %s", resp.StatusCode, string(b))
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &Credentials{
		Refresh: refreshToken,
		Access:  result.AccessToken,
		Expires: time.Now().Add(time.Duration(result.ExpiresIn)*time.Second - 5*time.Minute).UnixMilli(),
	}, nil
}
