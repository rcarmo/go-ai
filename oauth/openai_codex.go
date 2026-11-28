// OpenAI Codex OAuth — browser and OpenAI device authorization flows.
package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	goai "github.com/rcarmo/go-ai"
)

const (
	codexDeviceUserCodeURL     = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	codexDeviceTokenURL        = "https://auth.openai.com/api/accounts/deviceauth/token"
	codexDeviceVerificationURI = "https://auth.openai.com/codex/device"
	codexDeviceRedirectURI     = "https://auth.openai.com/deviceauth/callback"
	codexDeviceTimeout         = 15 * time.Minute
	codexBrowserLoginMethod    = "browser"
	codexDeviceCodeLoginMethod = "device_code"
	codexScope                 = "openid profile email offline_access"
)

// OpenAICodexProvider implements the OAuth flows for OpenAI Codex.
type OpenAICodexProvider struct {
	client                            *http.Client
	authorizeURL, tokenURL            string
	deviceUserCodeURL, deviceTokenURL string
	callbackPort                      int
	listen                            func(string, string) (net.Listener, error)
	pollWait                          func(context.Context, time.Duration) error
}

func init() {
	RegisterProvider(&OpenAICodexProvider{})
}

func (p *OpenAICodexProvider) ID() string   { return "openai-codex" }
func (p *OpenAICodexProvider) Name() string { return "OpenAI Codex" }

func (p *OpenAICodexProvider) Login(callbacks LoginCallbacks) (*Credentials, error) {
	method := codexDeviceCodeLoginMethod
	if callbacks.OnSelect != nil {
		selected, err := callbacks.OnSelect(SelectPrompt{
			Message: "Select OpenAI Codex login method:",
			Options: []SelectOption{{Value: codexBrowserLoginMethod, Label: "Browser login (default)"}, {Value: codexDeviceCodeLoginMethod, Label: "Device code login (headless)"}},
			Default: codexBrowserLoginMethod,
		})
		if err != nil {
			return nil, err
		}
		if selected != "" {
			method = selected
		}
	}
	if method == codexBrowserLoginMethod {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		return p.loginBrowser(ctx, callbacks)
	}
	if method != codexDeviceCodeLoginMethod {
		return nil, fmt.Errorf("unknown OpenAI Codex login method %q", method)
	}
	return p.loginDevice(context.Background(), callbacks)
}

func (p *OpenAICodexProvider) RefreshToken(creds *Credentials) (*Credentials, error) {
	return p.RefreshTokenContext(context.Background(), creds)
}

func (p *OpenAICodexProvider) RefreshTokenContext(ctx context.Context, creds *Credentials) (*Credentials, error) {
	if creds == nil || creds.Refresh == "" {
		return nil, fmt.Errorf("OpenAI Codex OAuth refresh token is missing")
	}
	return p.browserToken(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {creds.Refresh},
		"client_id":     {codexBrowserClientID},
	}, creds.Refresh)
}

func (p *OpenAICodexProvider) GetAPIKey(creds *Credentials) string {
	if creds == nil {
		return ""
	}
	return creds.Access
}

func (p *OpenAICodexProvider) ModifyModels(models []*goai.Model, creds *Credentials) []*goai.Model {
	return models
}

type codexDeviceResponse struct {
	DeviceAuthID string
	UserCode     string
	Interval     int
}

func (p *OpenAICodexProvider) loginDevice(ctx context.Context, callbacks LoginCallbacks) (*Credentials, error) {
	device, err := p.startCodexDeviceFlow(ctx)
	if err != nil {
		return nil, err
	}
	if callbacks.OnAuth != nil {
		callbacks.OnAuth(AuthInfo{
			URL:              codexDeviceVerificationURI,
			Instructions:     fmt.Sprintf("Enter code: %s", device.UserCode),
			UserCode:         device.UserCode,
			IntervalSeconds:  device.Interval,
			ExpiresInSeconds: int(codexDeviceTimeout / time.Second),
		})
	}
	tokens, err := p.pollForCodexToken(ctx, device)
	if err != nil {
		return nil, err
	}
	return p.browserToken(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {codexBrowserClientID},
		"code":          {tokens.AuthorizationCode},
		"code_verifier": {tokens.CodeVerifier},
		"redirect_uri":  {codexDeviceRedirectURI},
	}, "")
}

func (p *OpenAICodexProvider) codexClient() *http.Client {
	if p.client != nil {
		return p.client
	}
	return http.DefaultClient
}

func (p *OpenAICodexProvider) startCodexDeviceFlow(ctx context.Context) (*codexDeviceResponse, error) {
	endpoint := p.deviceUserCodeURL
	if endpoint == "" {
		endpoint = codexDeviceUserCodeURL
	}
	reqBody := strings.NewReader(`{"client_id":"` + codexBrowserClientID + `"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.codexClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("OpenAI Codex device code request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("OpenAI Codex device code login is not enabled for this server. Use browser login or verify the server URL")
		}
		if len(body) == 0 {
			return nil, fmt.Errorf("OpenAI Codex device code request failed with status %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("OpenAI Codex device code request failed with status %d: %s", resp.StatusCode, body)
	}
	var raw struct {
		DeviceAuthID string      `json:"device_auth_id"`
		UserCode     string      `json:"user_code"`
		Interval     interface{} `json:"interval"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("invalid OpenAI Codex device code response: %s", body)
	}
	interval := 0
	validInterval := false
	switch value := raw.Interval.(type) {
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		interval, validInterval = parsed, err == nil
	case float64:
		interval, validInterval = int(value), value >= 0 && value == float64(int(value))
	}
	if raw.DeviceAuthID == "" || raw.UserCode == "" || !validInterval {
		return nil, fmt.Errorf("invalid OpenAI Codex device code response: %s", body)
	}
	return &codexDeviceResponse{DeviceAuthID: raw.DeviceAuthID, UserCode: raw.UserCode, Interval: interval}, nil
}

type codexDeviceTokens struct {
	AuthorizationCode string
	CodeVerifier      string
}

func (p *OpenAICodexProvider) pollForCodexToken(ctx context.Context, device *codexDeviceResponse) (codexDeviceTokens, error) {
	deadline := time.Now().Add(codexDeviceTimeout)
	interval := explicitDeviceCodePollInterval(device.Interval)
	endpoint := p.deviceTokenURL
	if endpoint == "" {
		endpoint = codexDeviceTokenURL
	}
	for time.Now().Before(deadline) {
		bodyBytes, _ := json.Marshal(map[string]string{"device_auth_id": device.DeviceAuthID, "user_code": device.UserCode})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(bodyBytes)))
		if err != nil {
			return codexDeviceTokens{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := p.codexClient().Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return codexDeviceTokens{}, oauthFlowError("Login cancelled")
			}
			return codexDeviceTokens{}, err
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if readErr != nil {
			return codexDeviceTokens{}, readErr
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var success struct {
				AuthorizationCode string `json:"authorization_code"`
				CodeVerifier      string `json:"code_verifier"`
			}
			if err := json.Unmarshal(body, &success); err != nil || success.AuthorizationCode == "" || success.CodeVerifier == "" {
				return codexDeviceTokens{}, fmt.Errorf("invalid OpenAI Codex device auth token response: %s", body)
			}
			return codexDeviceTokens{AuthorizationCode: success.AuthorizationCode, CodeVerifier: success.CodeVerifier}, nil
		}
		pending := resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound
		var failure struct {
			Error interface{} `json:"error"`
		}
		_ = json.Unmarshal(body, &failure)
		errorCode := ""
		switch value := failure.Error.(type) {
		case string:
			errorCode = value
		case map[string]interface{}:
			errorCode, _ = value["code"].(string)
		}
		if errorCode == "deviceauth_authorization_pending" {
			pending = true
		}
		if errorCode == "slow_down" {
			interval += 5 * time.Second
			pending = true
		}
		if !pending {
			if len(body) == 0 {
				return codexDeviceTokens{}, fmt.Errorf("OpenAI Codex device auth failed with status %d", resp.StatusCode)
			}
			return codexDeviceTokens{}, fmt.Errorf("OpenAI Codex device auth failed with status %d: %s", resp.StatusCode, body)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		wait := interval
		if wait > remaining {
			wait = remaining
		}
		waitFunc := p.pollWait
		if waitFunc == nil {
			waitFunc = func(waitCtx context.Context, delay time.Duration) error {
				timer := time.NewTimer(delay)
				defer timer.Stop()
				select {
				case <-waitCtx.Done():
					return waitCtx.Err()
				case <-timer.C:
					return nil
				}
			}
		}
		if err := waitFunc(ctx, wait); err != nil {
			if ctx.Err() != nil {
				return codexDeviceTokens{}, oauthFlowError("Login cancelled")
			}
			return codexDeviceTokens{}, err
		}
	}
	return codexDeviceTokens{}, oauthFlowError("Device flow timed out")
}
