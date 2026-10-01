// Anthropic OAuth provider — authorization code flow with local callback server.
package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	goai "github.com/rcarmo/go-ai"
)

var anthropicOAuthSensitivePattern = regexp.MustCompile(`(?i)(access_token|refresh_token|code_verifier|code)([=: ]+)([^\s,;\"'}]+)`)

const (
	anthropicAuthURL             = "https://claude.ai/oauth/authorize"
	anthropicTokenURL            = "https://platform.claude.com/v1/oauth/token"
	anthropicClientID            = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	anthropicBrowserRedirect     = "http://localhost:53692/callback"
	anthropicCopyCodeRedirect    = "https://platform.claude.com/oauth/code/callback"
	anthropicBrowserLoginMethod  = "browser"
	anthropicCopyCodeLoginMethod = "copy_code"
	anthropicOAuthScope          = "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"
	anthropicCallbackHost        = "127.0.0.1"
	anthropicCallbackPort        = 53692
	anthropicCallbackPath        = "/callback"
	anthropicDefaultLoginTimeout = 5 * time.Minute
	anthropicTokenExpirySkew     = 5 * time.Minute
)

type AnthropicProvider struct {
	client       *http.Client
	authorizeURL string
	tokenURL     string
	callbackHost string
	callbackPort int
	callbackPath string
	timeout      time.Duration
	listen       func(network, address string) (net.Listener, error)
}

// configuration returns a defaulted copy, so legacy zero-value construction
// remains usable without mutating a shared provider during concurrent calls.
func (p *AnthropicProvider) configuration() AnthropicProvider {
	cfg := *NewAnthropicProvider(nil)
	if p == nil {
		return cfg
	}
	if p.client != nil {
		cfg.client = p.client
	}
	if p.authorizeURL != "" {
		cfg.authorizeURL = p.authorizeURL
	}
	if p.tokenURL != "" {
		cfg.tokenURL = p.tokenURL
	}
	if p.callbackHost != "" {
		cfg.callbackHost = p.callbackHost
	}
	if p.callbackPath != "" {
		cfg.callbackPath = p.callbackPath
		cfg.callbackPort = p.callbackPort
	} else if p.callbackPort != 0 {
		cfg.callbackPort = p.callbackPort
	}
	if p.timeout > 0 {
		cfg.timeout = p.timeout
	}
	cfg.listen = p.listen
	return cfg
}

func init() {
	RegisterProvider(NewAnthropicProvider(nil))
}

func NewAnthropicProvider(client *http.Client) *AnthropicProvider {
	if client == nil {
		client = http.DefaultClient
	}
	return &AnthropicProvider{client: client, authorizeURL: anthropicAuthURL, tokenURL: anthropicTokenURL, callbackHost: anthropicCallbackHost, callbackPort: anthropicCallbackPort, callbackPath: anthropicCallbackPath, timeout: anthropicDefaultLoginTimeout}
}

func (p *AnthropicProvider) ID() string   { return "anthropic" }
func (p *AnthropicProvider) Name() string { return "Anthropic" }

func (p *AnthropicProvider) Login(callbacks LoginCallbacks) (*Credentials, error) {
	cfg := p.configuration()
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()
	return cfg.loginContext(ctx, callbacks)
}

func (p *AnthropicProvider) loginContext(ctx context.Context, callbacks LoginCallbacks) (*Credentials, error) {
	method := anthropicBrowserLoginMethod
	if callbacks.OnSelect != nil {
		selected, err := callbacks.OnSelect(SelectPrompt{Message: "Choose Anthropic sign-in method", Options: []SelectOption{{Value: anthropicBrowserLoginMethod, Label: "Browser login (default)"}, {Value: anthropicCopyCodeLoginMethod, Label: "Copy code login (headless)"}}, Default: anthropicBrowserLoginMethod})
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(selected) != "" {
			method = strings.TrimSpace(selected)
		}
	}
	switch method {
	case anthropicBrowserLoginMethod:
		return p.loginBrowser(ctx, callbacks)
	case anthropicCopyCodeLoginMethod:
		return p.loginCopyCode(ctx, callbacks)
	default:
		return nil, fmt.Errorf("unsupported Anthropic OAuth login method %q", method)
	}
}

func (p *AnthropicProvider) loginBrowser(ctx context.Context, callbacks LoginCallbacks) (*Credentials, error) {
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}
	state := verifier
	var credentials *Credentials
	callback, err := startOAuthCallbackServer(ctx, oauthCallbackOptions{
		ProviderName: "Anthropic", Host: p.callbackHost, Port: p.callbackPort, Path: p.callbackPath, State: state, Listen: p.listen,
		Complete: func(_ context.Context, callbackURL *url.URL) error {
			code, err := anthropicCodeFromCallback(callbackURL, state)
			if err != nil {
				return err
			}
			redirectURI := anthropicBrowserRedirect
			if p.callbackPort != anthropicCallbackPort || p.callbackPath != anthropicCallbackPath {
				redirectURI = callbackURL.Scheme + "://" + callbackURL.Host + callbackURL.Path
			}
			credentials, err = p.exchangeCode(ctx, code, state, verifier, redirectURI)
			return err
		},
	})
	if err != nil {
		return nil, fmt.Errorf("callback server: %w", err)
	}
	defer callback.Close()
	redirectURI := p.browserRedirectURI(callback.RedirectURI)
	authURL, err := p.authorizationURL(redirectURI, state, challenge)
	if err != nil {
		return nil, err
	}
	if callbacks.OnAuth != nil {
		callbacks.OnAuth(AuthInfo{URL: authURL, Instructions: "Complete Anthropic login in your browser."})
	}
	if _, err := callback.Wait(); err != nil {
		return nil, err
	}
	return credentials, nil
}

func (p *AnthropicProvider) browserRedirectURI(callbackRedirectURI string) string {
	// The official browser redirect is localhost even when the listener binds
	// to another interface. Tests may change the port/path without live calls.
	if p.callbackPort == anthropicCallbackPort && p.callbackPath == anthropicCallbackPath {
		return anthropicBrowserRedirect
	}
	return callbackRedirectURI
}

func (p *AnthropicProvider) loginCopyCode(ctx context.Context, callbacks LoginCallbacks) (*Credentials, error) {
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}
	state := verifier
	authURL, err := p.authorizationURL(anthropicCopyCodeRedirect, state, challenge)
	if err != nil {
		return nil, err
	}
	if callbacks.OnAuth != nil {
		callbacks.OnAuth(AuthInfo{URL: authURL, Instructions: "Open the Anthropic login URL, then paste the returned authorization code in code#state format."})
	}
	if callbacks.OnPrompt == nil {
		return nil, fmt.Errorf("anthropic copy-code OAuth requires a prompt callback")
	}
	input, err := callbacks.OnPrompt(Prompt{Message: "Paste Anthropic authorization code:", Placeholder: "code#state", AllowEmpty: false})
	if err != nil {
		return nil, err
	}
	code, pastedState, err := parseAnthropicCopyCodeInput(input)
	if err != nil {
		return nil, err
	}
	if pastedState != "" && pastedState != state {
		return nil, fmt.Errorf("oauth state mismatch")
	}
	return p.exchangeCode(ctx, code, state, verifier, anthropicCopyCodeRedirect)
}

func (p *AnthropicProvider) authorizationURL(redirectURI, state, challenge string) (string, error) {
	base, err := url.Parse(p.authorizeURL)
	if err != nil {
		return "", err
	}
	base.RawQuery = url.Values{
		"code":                  {"true"},
		"response_type":         {"code"},
		"client_id":             {anthropicClientID},
		"redirect_uri":          {redirectURI},
		"scope":                 {anthropicOAuthScope},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode()
	return base.String(), nil
}

func anthropicCodeFromCallback(callbackURL *url.URL, expectedState string) (string, error) {
	if callbackURL == nil {
		return "", fmt.Errorf("missing callback URL")
	}
	if oauthErr := callbackURL.Query().Get("error"); oauthErr != "" {
		desc := callbackURL.Query().Get("error_description")
		if desc == "" {
			desc = oauthErr
		}
		return "", fmt.Errorf("anthropic authorization failed: %s", desc)
	}
	state := callbackURL.Query().Get("state")
	if state == "" {
		return "", fmt.Errorf("missing OAuth state")
	}
	if state != expectedState {
		return "", fmt.Errorf("oauth state mismatch")
	}
	code := strings.TrimSpace(callbackURL.Query().Get("code"))
	if code == "" {
		return "", fmt.Errorf("missing authorization code")
	}
	return code, nil
}

func (p *AnthropicProvider) RefreshToken(creds *Credentials) (*Credentials, error) {
	return p.RefreshTokenContext(context.Background(), creds)
}

func (p *AnthropicProvider) RefreshTokenContext(ctx context.Context, creds *Credentials) (*Credentials, error) {
	if creds == nil || creds.Refresh == "" {
		return nil, fmt.Errorf("anthropic OAuth refresh token is missing")
	}
	cfg := p.configuration()
	return cfg.refreshToken(ctx, creds.Refresh)
}

func (p *AnthropicProvider) GetAPIKey(creds *Credentials) string {
	if creds == nil {
		return ""
	}
	return creds.Access
}

func (p *AnthropicProvider) ModifyModels(models []*goai.Model, creds *Credentials) []*goai.Model {
	return models
}

func parseAnthropicCopyCodeInput(input string) (code string, state string, err error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", "", fmt.Errorf("missing authorization code")
	}
	if callbackURL, parseErr := url.Parse(value); parseErr == nil && callbackURL.Scheme != "" && callbackURL.Host != "" {
		code = strings.TrimSpace(callbackURL.Query().Get("code"))
		state = strings.TrimSpace(callbackURL.Query().Get("state"))
	} else if strings.Contains(value, "#") {
		parts := strings.SplitN(value, "#", 2)
		code = strings.TrimSpace(parts[0])
		state = strings.TrimSpace(parts[1])
	} else if strings.Contains(value, "code=") {
		params, parseErr := url.ParseQuery(value)
		if parseErr != nil {
			return "", "", fmt.Errorf("invalid authorization code input")
		}
		code = strings.TrimSpace(params.Get("code"))
		state = strings.TrimSpace(params.Get("state"))
	} else {
		code = value
	}
	if code == "" {
		return "", "", fmt.Errorf("missing authorization code")
	}
	return code, state, nil
}

func (p *AnthropicProvider) exchangeCode(ctx context.Context, code, state, verifier, redirectURI string) (*Credentials, error) {
	return p.requestToken(ctx, map[string]string{"grant_type": "authorization_code", "client_id": anthropicClientID, "code": code, "state": state, "redirect_uri": redirectURI, "code_verifier": verifier})
}

func (p *AnthropicProvider) refreshToken(ctx context.Context, refreshToken string) (*Credentials, error) {
	return p.requestToken(ctx, map[string]string{"grant_type": "refresh_token", "client_id": anthropicClientID, "refresh_token": refreshToken})
}

func (p *AnthropicProvider) requestToken(ctx context.Context, form map[string]string) (*Credentials, error) {
	payload, err := json.Marshal(form)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, anthropicTokenErrorSummary(body, form))
	}
	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.AccessToken == "" {
		return nil, fmt.Errorf("anthropic OAuth token response missing access_token")
	}
	refresh := result.RefreshToken
	if refresh == "" {
		refresh = form["refresh_token"]
	}
	return &Credentials{Refresh: refresh, Access: result.AccessToken, Expires: time.Now().Add(time.Duration(result.ExpiresIn)*time.Second - anthropicTokenExpirySkew).UnixMilli()}, nil
}

func anthropicTokenErrorSummary(body []byte, form map[string]string) string {
	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "token endpoint returned non-JSON error"
	}
	out := map[string]string{}
	for _, key := range []string{"error", "error_description", "error_uri"} {
		if value, ok := parsed[key].(string); ok && strings.TrimSpace(value) != "" {
			out[key] = redactAnthropicOAuthErrorText(value, form)
		}
	}
	if len(out) == 0 {
		return "token endpoint returned error"
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "token endpoint returned error"
	}
	return string(data)
}

func redactAnthropicOAuthErrorText(text string, form map[string]string) string {
	sensitive := anthropicOAuthSensitivePattern.MatchString(text)
	for _, key := range []string{"code", "state", "code_verifier", "refresh_token", "access_token"} {
		if value := form[key]; strings.TrimSpace(value) != "" && strings.Contains(text, value) {
			sensitive = true
		}
	}
	if sensitive {
		return "[REDACTED]"
	}
	if len(text) > 500 {
		return text[:500] + "..."
	}
	return text
}
