package oauth

import (
	"bytes"
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
	metaClientID               = "1031625952748946"
	metaDeviceAuthorizationURL = "https://auth.meta.com/oidc/device/authorization/"
	metaDeviceTokenURL         = "https://auth.meta.com/oidc/device/token/"
	metaAPIKeyMintURL          = "https://api.meta.ai/muse-code/key"
	metaAPIKeyLifetime         = 24 * time.Hour
	metaRequestTimeout         = 30 * time.Second
)

type MetaProvider struct {
	client                 *http.Client
	deviceAuthorizationURL string
	deviceTokenURL         string
	apiKeyMintURL          string
	pollWait               func(context.Context, time.Duration) error
}

func init() { RegisterProvider(NewMetaProvider(nil)) }

func NewMetaProvider(client *http.Client) *MetaProvider {
	if client == nil {
		client = http.DefaultClient
	}
	return &MetaProvider{client: client, deviceAuthorizationURL: metaDeviceAuthorizationURL, deviceTokenURL: metaDeviceTokenURL, apiKeyMintURL: metaAPIKeyMintURL, pollWait: waitRadiusPollInterval}
}

func (p *MetaProvider) ID() string   { return string(goai.ProviderMeta) }
func (p *MetaProvider) Name() string { return "Meta (Muse subscription)" }

func (p *MetaProvider) Login(callbacks LoginCallbacks) (*Credentials, error) {
	device, err := p.requestDeviceAuthorization(context.Background())
	if err != nil {
		return nil, err
	}
	if callbacks.OnAuth != nil {
		callbacks.OnAuth(AuthInfo{URL: device.VerificationURI, Instructions: fmt.Sprintf("Enter code: %s", device.UserCode)})
	}
	identityToken, err := p.pollForIdentityToken(context.Background(), device)
	if err != nil {
		return nil, err
	}
	if callbacks.OnProgress != nil {
		callbacks.OnProgress("Enabling Meta Model API access...")
	}
	return p.mintAPIKey(context.Background(), identityToken)
}

func (p *MetaProvider) RefreshToken(creds *Credentials) (*Credentials, error) {
	return p.RefreshTokenContext(context.Background(), creds)
}

func (p *MetaProvider) RefreshTokenContext(ctx context.Context, creds *Credentials) (*Credentials, error) {
	if creds == nil || creds.Refresh == "" {
		return nil, fmt.Errorf("meta OAuth identity token is missing")
	}
	return p.mintAPIKey(ctx, creds.Refresh)
}

func (p *MetaProvider) GetAPIKey(creds *Credentials) string {
	if creds == nil {
		return ""
	}
	return creds.Access
}

func (p *MetaProvider) ModifyModels(models []*goai.Model, creds *Credentials) []*goai.Model {
	return models
}

type metaDeviceAuthorization struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	Interval        int
	ExpiresIn       int
}

func (p *MetaProvider) requestDeviceAuthorization(ctx context.Context) (*metaDeviceAuthorization, error) {
	body, status, err := p.postForm(ctx, p.deviceAuthorizationURL, url.Values{"client_id": {metaClientID}})
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, metaOAuthFailure("device authorization", status, body)
	}
	deviceCode, err := requiredMetaString(body, "device_code")
	if err != nil {
		return nil, err
	}
	userCode, err := requiredMetaString(body, "user_code")
	if err != nil {
		return nil, err
	}
	verification, err := trustedMetaHTTPURL(firstNonEmptyMetaString(body, "verification_uri_complete", "verification_uri"))
	if err != nil {
		return nil, err
	}
	expiresIn, err := positiveMetaNumber(body, "expires_in")
	if err != nil {
		return nil, err
	}
	interval := 0
	if v, ok := numericJSON(body["interval"]); ok && v > 0 {
		interval = int(v)
	}
	return &metaDeviceAuthorization{DeviceCode: deviceCode, UserCode: userCode, VerificationURI: verification, Interval: interval, ExpiresIn: int(expiresIn)}, nil
}

func (p *MetaProvider) pollForIdentityToken(ctx context.Context, device *metaDeviceAuthorization) (string, error) {
	deadline := time.Now().Add(time.Duration(device.ExpiresIn) * time.Second)
	interval := normalizeDeviceCodePollInterval(device.Interval)
	for time.Now().Before(deadline) {
		wait := p.pollWait
		if wait == nil {
			wait = waitRadiusPollInterval
		}
		if err := wait(ctx, interval); err != nil {
			return "", fmt.Errorf("login cancelled: %w", err)
		}
		body, status, err := p.postForm(ctx, p.deviceTokenURL, url.Values{"grant_type": {radiusDeviceGrant}, "client_id": {metaClientID}, "device_code": {device.DeviceCode}})
		if err != nil {
			return "", err
		}
		if status >= 200 && status < 300 {
			return requiredMetaString(body, "access_token")
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
		case "access_denied":
			return "", fmt.Errorf("meta login was denied")
		case "expired_token":
			return "", fmt.Errorf("meta device authorization expired")
		default:
			return "", metaOAuthFailure("device token request", status, body)
		}
	}
	return "", fmt.Errorf("meta device flow timed out")
}

func (p *MetaProvider) mintAPIKey(ctx context.Context, identityToken string) (*Credentials, error) {
	body, status, err := p.postJSON(ctx, p.apiKeyMintURL, identityToken)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return nil, fmt.Errorf("meta session expired (status %d). Run `/login meta` to sign in again%s", status, metaErrorDetail(body))
	}
	if status < 200 || status >= 300 {
		return nil, metaOAuthFailure("API key mint", status, body)
	}
	apiKey, ok := body["api_key"].(string)
	if !ok || apiKey == "" {
		if action, err := trustedMetaHTTPURL(body["action_url"]); err == nil && action != "" {
			return nil, fmt.Errorf("meta did not issue an API key. Complete setup at %s", action)
		}
		return nil, fmt.Errorf("meta did not issue an API key")
	}
	return &Credentials{Refresh: identityToken, Access: apiKey, Expires: time.Now().Add(metaAPIKeyLifetime).UnixMilli()}, nil
}

func (p *MetaProvider) postForm(ctx context.Context, endpoint string, body url.Values) (map[string]interface{}, int, error) {
	reqCtx, cancel := metaRequestContext(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return p.doJSON(req)
}

func (p *MetaProvider) postJSON(ctx context.Context, endpoint string, bearer string) (map[string]interface{}, int, error) {
	reqCtx, cancel := metaRequestContext(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewBufferString("{}"))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-version", "1.0.0")
	return p.doJSON(req)
}

func (p *MetaProvider) doJSON(req *http.Request) (map[string]interface{}, int, error) {
	resp, err := p.client.Do(req)
	if err != nil {
		if req.Context().Err() != nil {
			return nil, 0, fmt.Errorf("login cancelled")
		}
		return nil, 0, err
	}
	defer resp.Body.Close()
	var decoded map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		if req.Context().Err() != nil {
			return nil, resp.StatusCode, fmt.Errorf("login cancelled")
		}
		return nil, resp.StatusCode, fmt.Errorf("meta OAuth returned invalid JSON (HTTP %d)", resp.StatusCode)
	}
	if decoded == nil {
		decoded = map[string]interface{}{}
	}
	return decoded, resp.StatusCode, nil
}

func metaRequestContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, metaRequestTimeout)
}

func requiredMetaString(body map[string]interface{}, field string) (string, error) {
	value, ok := body[field].(string)
	if !ok || value == "" {
		return "", fmt.Errorf("invalid meta OAuth response field: %s", field)
	}
	return value, nil
}

func firstNonEmptyMetaString(body map[string]interface{}, fields ...string) string {
	for _, field := range fields {
		if value, _ := body[field].(string); value != "" {
			return value
		}
	}
	return ""
}

func positiveMetaNumber(body map[string]interface{}, field string) (float64, error) {
	value, ok := numericJSON(body[field])
	if !ok || value <= 0 {
		return 0, fmt.Errorf("invalid meta OAuth response field: %s", field)
	}
	return value, nil
}

func trustedMetaHTTPURL(raw interface{}) (string, error) {
	value, ok := raw.(string)
	if !ok {
		value = fmt.Sprint(raw)
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", fmt.Errorf("untrusted verification URI in meta OAuth response")
	}
	return parsed.String(), nil
}

func metaOAuthFailure(action string, status int, body map[string]interface{}) error {
	detail := metaErrorDetail(body)
	if detail != "" {
		return fmt.Errorf("meta OAuth %s failed with status %d%s", action, status, detail)
	}
	return fmt.Errorf("meta OAuth %s failed with status %d", action, status)
}

func metaErrorDetail(body map[string]interface{}) string {
	for _, key := range []string{"error_description", "detail", "message", "error"} {
		value, _ := body[key].(string)
		if strings.TrimSpace(value) != "" {
			return ": " + strings.TrimSpace(value)
		}
	}
	return ""
}
