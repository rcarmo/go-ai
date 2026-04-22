// OpenAI Codex OAuth — device code flow for ChatGPT Plus/Pro subscriptions.
package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	goai "github.com/rcarmo/go-ai"
)

const (
	codexDeviceCodeURL  = "https://auth0.openai.com/oauth/device/code"
	codexAccessTokenURL = "https://auth0.openai.com/oauth/token"
	codexClientID       = "DRivsnm2Mu42T3KOpqdtwB3NYviHYzwD"
	codexAudience       = "https://api.openai.com/v1"
	codexScope          = "openid profile email offline_access"
)

// OpenAICodexProvider implements the OAuth device flow for OpenAI Codex.
type OpenAICodexProvider struct{}

func init() {
	RegisterProvider(&OpenAICodexProvider{})
}

func (p *OpenAICodexProvider) ID() string   { return "openai-codex" }
func (p *OpenAICodexProvider) Name() string { return "OpenAI Codex" }

func (p *OpenAICodexProvider) Login(callbacks LoginCallbacks) (*Credentials, error) {
	// Start device flow
	device, err := startCodexDeviceFlow()
	if err != nil {
		return nil, fmt.Errorf("device flow: %w", err)
	}

	callbacks.OnAuth(AuthInfo{
		URL:          device.VerificationURI,
		Instructions: fmt.Sprintf("Enter code: %s", device.UserCode),
	})

	// Poll for access token
	ctx := context.Background()
	return pollForCodexToken(ctx, device.DeviceCode, device.Interval, device.ExpiresIn)
}

func (p *OpenAICodexProvider) RefreshToken(creds *Credentials) (*Credentials, error) {
	return refreshCodexToken(creds.Refresh)
}

func (p *OpenAICodexProvider) GetAPIKey(creds *Credentials) string {
	return creds.Access
}

func (p *OpenAICodexProvider) ModifyModels(models []*goai.Model, creds *Credentials) []*goai.Model {
	return models
}

type codexDeviceResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri_complete"`
	Interval        int    `json:"interval"`
	ExpiresIn       int    `json:"expires_in"`
}

func startCodexDeviceFlow() (*codexDeviceResponse, error) {
	body := url.Values{
		"client_id": {codexClientID},
		"scope":     {codexScope},
		"audience":  {codexAudience},
	}

	resp, err := http.Post(codexDeviceCodeURL, "application/x-www-form-urlencoded", strings.NewReader(body.Encode()))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}

	var result codexDeviceResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.VerificationURI == "" {
		// Fallback to non-complete URI
		var raw map[string]interface{}
		json.Unmarshal([]byte(`{}`), &raw)
	}
	return &result, nil
}

func pollForCodexToken(ctx context.Context, deviceCode string, intervalSecs, expiresIn int) (*Credentials, error) {
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	interval := time.Duration(intervalSecs) * time.Second
	if interval < time.Second {
		interval = 5 * time.Second
	}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}

		body := url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {deviceCode},
			"client_id":   {codexClientID},
		}

		resp, err := http.Post(codexAccessTokenURL, "application/x-www-form-urlencoded", strings.NewReader(body.Encode()))
		if err != nil {
			continue
		}

		var raw map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&raw)
		resp.Body.Close()

		if token, ok := raw["access_token"].(string); ok {
			refresh, _ := raw["refresh_token"].(string)
			expiresIn, _ := raw["expires_in"].(float64)
			return &Credentials{
				Refresh: refresh,
				Access:  token,
				Expires: time.Now().Add(time.Duration(expiresIn)*time.Second - 5*time.Minute).UnixMilli(),
			}, nil
		}

		if errStr, ok := raw["error"].(string); ok {
			switch errStr {
			case "authorization_pending":
				continue
			case "slow_down":
				interval = interval * 14 / 10
				continue
			default:
				desc, _ := raw["error_description"].(string)
				return nil, fmt.Errorf("device flow: %s: %s", errStr, desc)
			}
		}
	}

	return nil, fmt.Errorf("device flow timed out")
}

func refreshCodexToken(refreshToken string) (*Credentials, error) {
	body := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {codexClientID},
		"refresh_token": {refreshToken},
	}

	resp, err := http.Post(codexAccessTokenURL, "application/x-www-form-urlencoded", strings.NewReader(body.Encode()))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("token refresh failed (%d): %s", resp.StatusCode, string(b))
	}

	var result struct {
		AccessToken  string  `json:"access_token"`
		RefreshToken string  `json:"refresh_token"`
		ExpiresIn    float64 `json:"expires_in"`
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
