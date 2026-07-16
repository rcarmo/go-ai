package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	goai "github.com/rcarmo/go-ai"
)

const (
	xaiClientID                    = "b1a00492-073a-47ea-816f-4c329264a828"
	xaiScope                       = "openid profile email offline_access grok-cli:access api:access"
	xaiDeviceCodeURL               = "https://auth.x.ai/oauth2/device/code"
	xaiTokenURL                    = "https://auth.x.ai/oauth2/token"
	xaiRefreshSkew                 = 5 * time.Minute
	xaiDefaultTokenLifetimeSeconds = 3600
)

type XAIProvider struct {
	client        *http.Client
	deviceCodeURL string
	tokenURL      string
	pollWait      func(context.Context, time.Duration) error
}

func init() { RegisterProvider(NewXAIProvider(nil)) }

func NewXAIProvider(client *http.Client) *XAIProvider {
	if client == nil {
		client = http.DefaultClient
	}
	return &XAIProvider{client: client, deviceCodeURL: xaiDeviceCodeURL, tokenURL: xaiTokenURL, pollWait: waitRadiusPollInterval}
}

func (p *XAIProvider) ID() string   { return string(goai.ProviderXAI) }
func (p *XAIProvider) Name() string { return "xAI (Grok/X subscription)" }

func (p *XAIProvider) Login(callbacks LoginCallbacks) (*Credentials, error) {
	device, err := p.requestDeviceCode(context.Background())
	if err != nil {
		return nil, err
	}
	if callbacks.OnAuth != nil {
		callbacks.OnAuth(AuthInfo{URL: device.VerificationURI, Instructions: fmt.Sprintf("Enter code: %s", device.UserCode)})
	}
	return p.pollForTokens(context.Background(), device)
}

func (p *XAIProvider) RefreshToken(creds *Credentials) (*Credentials, error) {
	if creds == nil || creds.Refresh == "" {
		return nil, fmt.Errorf("xAI OAuth refresh token is missing")
	}
	return p.refreshToken(context.Background(), creds.Refresh)
}

func (p *XAIProvider) GetAPIKey(creds *Credentials) string {
	if creds == nil {
		return ""
	}
	return creds.Access
}

func (p *XAIProvider) ModifyModels(models []*goai.Model, creds *Credentials) []*goai.Model {
	return models
}

type xaiDeviceCode struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	Interval        int
	ExpiresIn       int
}

func (p *XAIProvider) requestDeviceCode(ctx context.Context) (*xaiDeviceCode, error) {
	body := url.Values{"client_id": {xaiClientID}, "scope": {xaiScope}, "referrer": {"pi"}}
	respBody, status, err := p.postForm(ctx, p.deviceCodeURL, body)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, xaiOAuthFailure("device authorization", status, respBody)
	}
	deviceCode, err := requiredXAIString(respBody, "device_code")
	if err != nil {
		return nil, err
	}
	userCode, err := requiredXAIString(respBody, "user_code")
	if err != nil {
		return nil, err
	}
	verification, err := requiredXAIString(respBody, "verification_uri")
	if err != nil {
		return nil, err
	}
	verification, err = validateXAIVerificationURI(verification)
	if err != nil {
		return nil, err
	}
	expiresIn, err := positiveXAINumber(respBody, "expires_in")
	if err != nil {
		return nil, err
	}
	interval := 0
	if v, ok := numericJSON(respBody["interval"]); ok && v > 0 {
		interval = int(v)
	}
	return &xaiDeviceCode{DeviceCode: deviceCode, UserCode: userCode, VerificationURI: verification, Interval: interval, ExpiresIn: int(expiresIn)}, nil
}

func (p *XAIProvider) pollForTokens(ctx context.Context, device *xaiDeviceCode) (*Credentials, error) {
	deadline := time.Now().Add(time.Duration(device.ExpiresIn) * time.Second)
	interval := normalizeDeviceCodePollInterval(device.Interval)
	for time.Now().Before(deadline) {
		wait := p.pollWait
		if wait == nil {
			wait = waitRadiusPollInterval
		}
		if err := wait(ctx, interval); err != nil {
			return nil, fmt.Errorf("Login cancelled: %w", err)
		}
		body, status, err := p.postForm(ctx, p.tokenURL, url.Values{"grant_type": {radiusDeviceGrant}, "client_id": {xaiClientID}, "device_code": {device.DeviceCode}})
		if err != nil {
			return nil, err
		}
		if status >= 200 && status < 300 {
			return xaiCredentialsFromToken(body, "")
		}
		errorCode, _ := body["error"].(string)
		switch errorCode {
		case "authorization_pending":
			continue
		case "slow_down":
			if v, ok := numericJSON(body["interval"]); ok && v > 0 {
				interval = normalizeDeviceCodePollInterval(int(v))
			} else {
				interval += 5 * time.Second
			}
			continue
		case "access_denied", "authorization_denied":
			return nil, fmt.Errorf("xAI device authorization was denied")
		case "expired_token":
			return nil, fmt.Errorf("xAI device code expired")
		default:
			return nil, xaiOAuthFailure("device token polling", status, body)
		}
	}
	return nil, fmt.Errorf("device flow timed out")
}

func (p *XAIProvider) refreshToken(ctx context.Context, refreshToken string) (*Credentials, error) {
	body, status, err := p.postForm(ctx, p.tokenURL, url.Values{"grant_type": {"refresh_token"}, "client_id": {xaiClientID}, "refresh_token": {refreshToken}})
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, xaiOAuthFailure("token refresh", status, body)
	}
	return xaiCredentialsFromToken(body, refreshToken)
}

func (p *XAIProvider) postForm(ctx context.Context, endpoint string, body url.Values) (map[string]interface{}, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, fmt.Errorf("Login cancelled")
		}
		return nil, 0, err
	}
	defer resp.Body.Close()
	var decoded map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		if ctx.Err() != nil {
			return nil, resp.StatusCode, fmt.Errorf("Login cancelled")
		}
		return nil, resp.StatusCode, fmt.Errorf("xAI OAuth returned invalid JSON (HTTP %d)", resp.StatusCode)
	}
	if decoded == nil {
		decoded = map[string]interface{}{}
	}
	return decoded, resp.StatusCode, nil
}

func xaiCredentialsFromToken(body map[string]interface{}, previousRefreshToken string) (*Credentials, error) {
	access, err := requiredXAIString(body, "access_token")
	if err != nil {
		return nil, err
	}
	refresh := previousRefreshToken
	if body["refresh_token"] != nil {
		refresh, err = requiredXAIString(body, "refresh_token")
		if err != nil {
			return nil, err
		}
	}
	if refresh == "" {
		refresh, err = requiredXAIString(body, "refresh_token")
		if err != nil {
			return nil, err
		}
	}
	expiresIn := float64(xaiDefaultTokenLifetimeSeconds)
	if body["expires_in"] != nil {
		expiresIn, err = positiveXAINumber(body, "expires_in")
		if err != nil {
			return nil, err
		}
	}
	return &Credentials{Access: access, Refresh: refresh, Expires: time.Now().Add(time.Duration(expiresIn)*time.Second - xaiRefreshSkew).UnixMilli()}, nil
}

func requiredXAIString(body map[string]interface{}, field string) (string, error) {
	value, ok := body[field].(string)
	if !ok || value == "" {
		return "", fmt.Errorf("invalid xAI OAuth response field: %s", field)
	}
	return value, nil
}

func positiveXAINumber(body map[string]interface{}, field string) (float64, error) {
	value, ok := numericJSON(body[field])
	if !ok || value <= 0 {
		return 0, fmt.Errorf("invalid xAI OAuth response field: %s", field)
	}
	return value, nil
}

func numericJSON(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func validateXAIVerificationURI(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", fmt.Errorf("untrusted verification URI in xAI OAuth response")
	}
	return parsed.String(), nil
}

func xaiOAuthFailure(action string, status int, body map[string]interface{}) error {
	errorCode, _ := body["error"].(string)
	desc, _ := body["error_description"].(string)
	detail := strings.Join(nonEmptyStrings(errorCode, desc), ": ")
	if detail != "" {
		return fmt.Errorf("xAI OAuth %s failed (HTTP %d): %s", action, status, detail)
	}
	return fmt.Errorf("xAI OAuth %s failed (HTTP %d)", action, status)
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
