package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	goai "github.com/rcarmo/go-ai"
)

const (
	DefaultRadiusGateway = "https://radius.pi.dev"
	radiusExpirySkew     = time.Minute
	radiusDeviceGrant    = "urn:ietf:params:oauth:grant-type:device_code"
)

// RadiusProvider implements OAuth for Radius pi-messages gateways.
type RadiusProvider struct {
	id      string
	name    string
	gateway string
	client  *http.Client
}

// RadiusProviderOptions configures a Radius OAuth provider.
type RadiusProviderOptions struct {
	ID      string
	Name    string
	Gateway string
	Client  *http.Client
}

// RadiusGatewayModel is the model shape served by Radius /v1/config.
type RadiusGatewayModel struct {
	ID               string                              `json:"id"`
	Name             string                              `json:"name"`
	Reasoning        bool                                `json:"reasoning"`
	ThinkingLevelMap map[goai.ModelThinkingLevel]*string `json:"thinkingLevelMap,omitempty"`
	Input            []string                            `json:"input"`
	Cost             goai.ModelCost                      `json:"cost"`
	ContextWindow    int                                 `json:"contextWindow"`
	MaxTokens        int                                 `json:"maxTokens"`
}

// RadiusGatewayConfig is cached on credentials after login/refresh.
type RadiusGatewayConfig struct {
	BaseURL string               `json:"baseUrl"`
	Models  []RadiusGatewayModel `json:"models"`
}

type radiusOAuthConfig struct {
	Issuer                            string `json:"issuer"`
	AuthorizationEndpoint             string `json:"authorizationEndpoint"`
	TokenEndpoint                     string `json:"tokenEndpoint"`
	DeviceAuthorizationEndpoint       string `json:"deviceAuthorizationEndpoint"`
	DeviceAuthorizationEventsEndpoint string `json:"deviceAuthorizationEventsEndpoint"`
	VerificationEndpoint              string `json:"verificationEndpoint"`
	ClientID                          string `json:"clientId"`
	Scope                             string `json:"scope"`
	DeviceCodeGrantType               string `json:"deviceCodeGrantType"`
}

type radiusDeviceAuthorization struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// OAuthResponseError preserves Radius OAuth error status and code.
type OAuthResponseError struct {
	Status     int
	OAuthError string
	Detail     string
	Message    string
}

func (e *OAuthResponseError) Error() string {
	detail := e.Detail
	if e.OAuthError != "" {
		if detail != "" {
			detail = e.OAuthError + ": " + detail
		} else {
			detail = e.OAuthError
		}
	}
	if detail == "" {
		detail = fmt.Sprint(e.Status)
	}
	return e.Message + ": " + detail
}

func init() { RegisterProvider(NewRadiusProvider(RadiusProviderOptions{})) }

// NewRadiusProvider creates a Radius OAuth provider. Zero options register the default radius gateway.
func NewRadiusProvider(options RadiusProviderOptions) *RadiusProvider {
	id := strings.TrimSpace(options.ID)
	if id == "" {
		id = "radius"
	}
	name := strings.TrimSpace(options.Name)
	if name == "" {
		name = "Radius"
	}
	gateway := normalizeRadiusGatewayURL(options.Gateway)
	if gateway == "" {
		gateway = DefaultRadiusGateway
	}
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	return &RadiusProvider{id: id, name: name, gateway: gateway, client: client}
}

func (p *RadiusProvider) ID() string   { return p.id }
func (p *RadiusProvider) Name() string { return p.name }

func (p *RadiusProvider) Login(callbacks LoginCallbacks) (*Credentials, error) {
	oauthCfg, err := p.loadOAuthConfig(context.Background())
	if err != nil {
		return nil, err
	}
	creds, err := p.loginWithDeviceCode(context.Background(), oauthCfg, callbacks)
	if err != nil {
		return nil, err
	}
	return p.attachGatewayConfig(context.Background(), creds, nil)
}

func (p *RadiusProvider) RefreshToken(creds *Credentials) (*Credentials, error) {
	if creds == nil || creds.Refresh == "" {
		return nil, fmt.Errorf("radius OAuth refresh token is missing")
	}
	oauthCfg, err := p.loadOAuthConfig(context.Background())
	if err != nil {
		return nil, err
	}
	refreshed, err := p.requestToken(context.Background(), oauthCfg, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {oauthCfg.ClientID},
		"refresh_token": {creds.Refresh},
	})
	if err != nil {
		return nil, err
	}
	return p.attachGatewayConfig(context.Background(), refreshed, creds)
}

func (p *RadiusProvider) GetAPIKey(creds *Credentials) string {
	if creds == nil {
		return ""
	}
	return creds.Access
}

func (p *RadiusProvider) ModifyModels(models []*goai.Model, creds *Credentials) []*goai.Model {
	config, ok := radiusCredentialConfig(creds)
	if !ok {
		return models
	}
	existing := map[string]bool{}
	for _, model := range models {
		if model != nil && model.Provider == goai.Provider(p.id) {
			existing[model.ID] = true
		}
	}
	out := append([]*goai.Model{}, models...)
	for _, gatewayModel := range config.Models {
		if gatewayModel.ID == "" || existing[gatewayModel.ID] {
			continue
		}
		existing[gatewayModel.ID] = true
		out = append(out, &goai.Model{
			ID:               gatewayModel.ID,
			Name:             gatewayModel.Name,
			Api:              goai.ApiPiMessages,
			Provider:         goai.Provider(p.id),
			BaseURL:          config.BaseURL,
			Reasoning:        gatewayModel.Reasoning,
			ThinkingLevelMap: gatewayModel.ThinkingLevelMap,
			Input:            append([]string{}, gatewayModel.Input...),
			Cost:             gatewayModel.Cost,
			ContextWindow:    gatewayModel.ContextWindow,
			MaxTokens:        gatewayModel.MaxTokens,
		})
	}
	return out
}

func (p *RadiusProvider) loadOAuthConfig(ctx context.Context) (*radiusOAuthConfig, error) {
	var cfg radiusOAuthConfig
	if err := p.getJSON(ctx, p.gateway+"/v1/oauth", "Radius OAuth config", "", &cfg); err != nil {
		return nil, err
	}
	if cfg.TokenEndpoint == "" || cfg.DeviceAuthorizationEndpoint == "" || cfg.ClientID == "" {
		return nil, fmt.Errorf("invalid Radius OAuth config from %s", p.gateway)
	}
	if cfg.DeviceCodeGrantType == "" {
		cfg.DeviceCodeGrantType = radiusDeviceGrant
	}
	return &cfg, nil
}

func (p *RadiusProvider) loadGatewayConfig(ctx context.Context, apiKey string) (*RadiusGatewayConfig, error) {
	var cfg RadiusGatewayConfig
	if err := p.getJSON(ctx, p.gateway+"/v1/config", "Radius config", apiKey, &cfg); err != nil {
		return nil, err
	}
	cfg.Models = sanitizeRadiusGatewayModels(cfg.Models)
	if cfg.BaseURL == "" || cfg.Models == nil {
		return nil, fmt.Errorf("invalid Radius config from %s", p.gateway)
	}
	return &cfg, nil
}

func (p *RadiusProvider) getJSON(ctx context.Context, endpoint, label, apiKey string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("radius OAuth cancelled: %w", ctx.Err())
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("could not load %s from %s: %d: %s", label, endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (p *RadiusProvider) requestToken(ctx context.Context, oauthCfg *radiusOAuthConfig, body url.Values) (*Credentials, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthCfg.TokenEndpoint, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("login cancelled: %w", ctx.Err())
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, readRadiusOAuthResponseError(resp, "Radius OAuth token request failed")
	}
	var data struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	if data.AccessToken == "" {
		return nil, fmt.Errorf("radius OAuth token response is missing access_token")
	}
	expires := int64(0)
	if data.ExpiresIn > 0 {
		expires = time.Now().Add(time.Duration(data.ExpiresIn)*time.Second - radiusExpirySkew).UnixMilli()
	}
	return &Credentials{Access: data.AccessToken, Refresh: data.RefreshToken, Expires: expires, Extra: map[string]interface{}{"scope": data.Scope}}, nil
}

func (p *RadiusProvider) loginWithDeviceCode(ctx context.Context, oauthCfg *radiusOAuthConfig, callbacks LoginCallbacks) (*Credentials, error) {
	device, err := p.requestDeviceAuthorization(ctx, oauthCfg)
	if err != nil {
		return nil, err
	}
	verification := device.VerificationURIComplete
	if verification == "" {
		verification = device.VerificationURI
	}
	if verification == "" {
		verification = oauthCfg.VerificationEndpoint
	}
	if callbacks.OnAuth != nil {
		callbacks.OnAuth(AuthInfo{URL: verification, Instructions: fmt.Sprintf("Enter code: %s", device.UserCode)})
	}
	return p.pollDeviceToken(ctx, oauthCfg, device)
}

func (p *RadiusProvider) requestDeviceAuthorization(ctx context.Context, oauthCfg *radiusOAuthConfig) (*radiusDeviceAuthorization, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthCfg.DeviceAuthorizationEndpoint, strings.NewReader(url.Values{"client_id": {oauthCfg.ClientID}, "scope": {oauthCfg.Scope}}.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("login cancelled: %w", ctx.Err())
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, readRadiusOAuthResponseError(resp, "Radius OAuth device authorization failed")
	}
	var device radiusDeviceAuthorization
	if err := json.NewDecoder(resp.Body).Decode(&device); err != nil {
		return nil, err
	}
	if device.DeviceCode == "" || device.UserCode == "" || device.ExpiresIn == 0 {
		return nil, fmt.Errorf("radius OAuth device authorization response is missing required fields")
	}
	return &device, nil
}

func (p *RadiusProvider) pollDeviceToken(ctx context.Context, oauthCfg *radiusOAuthConfig, device *radiusDeviceAuthorization) (*Credentials, error) {
	deadline := time.Now().Add(time.Duration(device.ExpiresIn) * time.Second)
	interval := normalizeDeviceCodePollInterval(device.Interval)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("login cancelled: %w", ctx.Err())
		case <-time.After(interval):
		}
		creds, err := p.requestToken(ctx, oauthCfg, url.Values{"grant_type": {oauthCfg.DeviceCodeGrantType}, "client_id": {oauthCfg.ClientID}, "device_code": {device.DeviceCode}})
		if err == nil {
			return creds, nil
		}
		var oauthErr *OAuthResponseError
		if !errors.As(err, &oauthErr) {
			return nil, err
		}
		switch oauthErr.OAuthError {
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
		case "expired_token":
			return nil, fmt.Errorf("device authorization expired")
		case "access_denied":
			return nil, fmt.Errorf("device authorization was denied")
		default:
			return nil, err
		}
	}
	return nil, fmt.Errorf("device flow timed out")
}

func (p *RadiusProvider) attachGatewayConfig(ctx context.Context, creds *Credentials, previous *Credentials) (*Credentials, error) {
	cfg, err := p.loadGatewayConfig(ctx, creds.Access)
	if err != nil {
		if previousCfg, ok := radiusCredentialConfig(previous); ok {
			creds.Extra = cloneExtra(creds.Extra)
			creds.Extra["gatewayConfig"] = previousCfg
			return creds, nil
		}
		return nil, err
	}
	creds.Extra = cloneExtra(creds.Extra)
	creds.Extra["gatewayConfig"] = cfg
	return creds, nil
}

func normalizeRadiusGatewayURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		value = "https://" + value
	}
	return strings.TrimRight(value, "/")
}

func sanitizeRadiusGatewayModels(models []RadiusGatewayModel) []RadiusGatewayModel {
	out := make([]RadiusGatewayModel, 0, len(models))
	for _, model := range models {
		if model.ID == "" || model.Name == "" || len(model.Input) == 0 || model.ContextWindow == 0 || model.MaxTokens == 0 {
			continue
		}
		out = append(out, model)
	}
	return out
}

func radiusCredentialConfig(creds *Credentials) (*RadiusGatewayConfig, bool) {
	if creds == nil || creds.Extra == nil {
		return nil, false
	}
	switch cfg := creds.Extra["gatewayConfig"].(type) {
	case *RadiusGatewayConfig:
		return cfg, true
	case RadiusGatewayConfig:
		return &cfg, true
	case map[string]interface{}:
		b, _ := json.Marshal(cfg)
		var decoded RadiusGatewayConfig
		if json.Unmarshal(b, &decoded) == nil {
			decoded.Models = sanitizeRadiusGatewayModels(decoded.Models)
			if decoded.BaseURL != "" && decoded.Models != nil {
				return &decoded, true
			}
		}
	}
	return nil, false
}

func cloneExtra(extra map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func readRadiusOAuthResponseError(resp *http.Response, message string) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	text := strings.TrimSpace(string(body))
	oauthErr := ""
	detail := text
	if text != "" {
		var data struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		if json.Unmarshal(body, &data) == nil {
			oauthErr = data.Error
			detail = data.ErrorDescription
		}
	}
	return &OAuthResponseError{Status: resp.StatusCode, OAuthError: oauthErr, Detail: detail, Message: message}
}
