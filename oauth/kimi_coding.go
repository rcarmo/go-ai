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
	kimiCodeClientID = "17e5f671-d194-4dfb-9706-5516cb48c098"
	kimiCodeHost     = "https://auth.kimi.com"
)

type KimiCodingProvider struct {
	client   *http.Client
	oauthURL string
	pollWait func(context.Context, time.Duration) error
}

func init() { RegisterProvider(NewKimiCodingProvider(nil)) }

func NewKimiCodingProvider(client *http.Client) *KimiCodingProvider {
	if client == nil {
		client = http.DefaultClient
	}
	return &KimiCodingProvider{client: client, oauthURL: kimiCodeHost, pollWait: waitRadiusPollInterval}
}

func (p *KimiCodingProvider) ID() string   { return string(goai.ProviderKimiCoding) }
func (p *KimiCodingProvider) Name() string { return "Kimi Code" }
func (p *KimiCodingProvider) GetAPIKey(creds *Credentials) string {
	if creds == nil {
		return ""
	}
	return creds.Access
}
func (p *KimiCodingProvider) ModifyModels(models []*goai.Model, creds *Credentials) []*goai.Model {
	return models
}

func (p *KimiCodingProvider) Login(callbacks LoginCallbacks) (*Credentials, error) {
	device, err := p.startDevice(context.Background())
	if err != nil {
		return nil, err
	}
	if callbacks.OnAuth != nil {
		callbacks.OnAuth(AuthInfo{URL: device.VerificationURIComplete, Instructions: fmt.Sprintf("Enter code: %s", device.UserCode)})
	}
	return p.pollDevice(context.Background(), device)
}

func (p *KimiCodingProvider) RefreshToken(creds *Credentials) (*Credentials, error) {
	if creds == nil || creds.Refresh == "" {
		return nil, fmt.Errorf("kimi code OAuth refresh token is missing")
	}
	body, status, err := p.postForm(context.Background(), "/api/oauth/token", url.Values{"client_id": {kimiCodeClientID}, "grant_type": {"refresh_token"}, "refresh_token": {creds.Refresh}})
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("kimi code token refresh failed (HTTP %d): %s", status, oauthErrorDetail(body))
	}
	return kimiTokenCredentials(body, "refresh")
}

type kimiDevice struct {
	DeviceCode, UserCode, VerificationURI, VerificationURIComplete string
	Interval, ExpiresIn                                            int
}

func (p *KimiCodingProvider) startDevice(ctx context.Context) (*kimiDevice, error) {
	body, status, err := p.postForm(ctx, "/api/oauth/device_authorization", url.Values{"client_id": {kimiCodeClientID}})
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("kimi code device authorization failed (HTTP %d): %s", status, oauthErrorDetail(body))
	}
	dev := &kimiDevice{}
	var ok bool
	if dev.DeviceCode, ok = body["device_code"].(string); !ok || dev.DeviceCode == "" {
		return nil, fmt.Errorf("invalid Kimi Code device authorization response")
	}
	if dev.UserCode, ok = body["user_code"].(string); !ok || dev.UserCode == "" {
		return nil, fmt.Errorf("invalid Kimi Code device authorization response")
	}
	if dev.VerificationURI, ok = body["verification_uri"].(string); !ok || !trustedHTTPURL(dev.VerificationURI) {
		return nil, fmt.Errorf("invalid Kimi Code device authorization response")
	}
	if dev.VerificationURIComplete, ok = body["verification_uri_complete"].(string); !ok || !trustedHTTPURL(dev.VerificationURIComplete) {
		return nil, fmt.Errorf("invalid Kimi Code device authorization response")
	}
	dev.Interval = intNumber(body["interval"], 5)
	dev.ExpiresIn = intNumber(body["expires_in"], 15*60)
	return dev, nil
}

func (p *KimiCodingProvider) pollDevice(ctx context.Context, dev *kimiDevice) (*Credentials, error) {
	deadline := time.Now().Add(time.Duration(dev.ExpiresIn) * time.Second)
	interval := normalizeDeviceCodePollInterval(dev.Interval)
	for time.Now().Before(deadline) {
		wait := p.pollWait
		if wait == nil {
			wait = waitRadiusPollInterval
		}
		if err := wait(ctx, interval); err != nil {
			return nil, fmt.Errorf("Login cancelled: %w", err)
		}
		body, status, err := p.postForm(ctx, "/api/oauth/token", url.Values{"client_id": {kimiCodeClientID}, "device_code": {dev.DeviceCode}, "grant_type": {radiusDeviceGrant}})
		if err != nil {
			return nil, err
		}
		if status >= 200 && status < 300 {
			return kimiTokenCredentials(body, "poll")
		}
		switch body["error"] {
		case "authorization_pending":
			continue
		case "slow_down":
			interval = normalizeDeviceCodePollInterval(intNumber(body["interval"], int(interval/time.Second)+5))
			continue
		case "expired_token":
			return nil, fmt.Errorf("kimi code device authorization expired; please restart login")
		case "access_denied":
			return nil, fmt.Errorf("kimi code login was denied")
		default:
			return nil, fmt.Errorf("kimi code device token request failed (HTTP %d): %s", status, oauthErrorDetail(body))
		}
	}
	return nil, fmt.Errorf("device flow timed out")
}

func (p *KimiCodingProvider) postForm(ctx context.Context, path string, form url.Values) (map[string]interface{}, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.oauthURL, "/")+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	var out map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, resp.StatusCode, err
	}
	if out == nil {
		out = map[string]interface{}{}
	}
	return out, resp.StatusCode, nil
}

func kimiTokenCredentials(body map[string]interface{}, op string) (*Credentials, error) {
	access, ok := body["access_token"].(string)
	if !ok || access == "" {
		return nil, fmt.Errorf("kimi code token %s response missing fields", op)
	}
	refresh, ok := body["refresh_token"].(string)
	if !ok || refresh == "" {
		return nil, fmt.Errorf("kimi code token %s response missing fields", op)
	}
	expires := intNumber(body["expires_in"], 0)
	if expires <= 0 {
		return nil, fmt.Errorf("kimi code token %s response missing fields", op)
	}
	return &Credentials{Access: access, Refresh: refresh, Expires: time.Now().Add(time.Duration(expires) * time.Second).UnixMilli()}, nil
}

func trustedHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}
func intNumber(v interface{}, fallback int) int {
	if f, ok := v.(float64); ok && f > 0 {
		return int(f)
	}
	if i, ok := v.(int); ok && i > 0 {
		return i
	}
	return fallback
}
func oauthErrorDetail(body map[string]interface{}) string {
	if s, _ := body["error_description"].(string); s != "" {
		return s
	}
	if s, _ := body["message"].(string); s != "" {
		return s
	}
	if s, _ := body["error"].(string); s != "" {
		return s
	}
	return ""
}
