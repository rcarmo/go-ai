// OpenAI ChatGPT OAuth — authorization-code flow for direct OpenAI Responses tokens.
package oauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	goai "github.com/rcarmo/go-ai"
)

const (
	openAIChatGPTProviderID       = "openai-chatgpt"
	openAIChatGPTDynamicClientID  = "dynamic_agent_client"
	openAIChatGPTAgentNameHint    = "Pi"
	openAIChatGPTAuthorizeURL     = "https://auth.openai.com/api/accounts/authorize"
	openAIChatGPTTokenURL         = "https://auth.openai.com/api/accounts/oauth/token"
	openAIChatGPTResource         = "https://api.openai.com/v1"
	openAIChatGPTDirectTokenScope = "chatgpt.tokens.use.direct"
	openAIChatGPTScope            = "openid profile email offline_access resource.invoke " + openAIChatGPTDirectTokenScope
	openAIChatGPTCallbackHost     = "127.0.0.1"
	openAIChatGPTCallbackPort     = 1455
	openAIChatGPTCallbackPath     = "/auth/callback"
	openAIChatGPTExpirySkew       = 3 * time.Minute
)

var openAIChatGPTUUIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type OpenAIChatGPTProvider struct {
	client       *http.Client
	authorizeURL string
	tokenURL     string
	deviceID     string
	callbackHost string
	callbackPort int
	callbackPath string
}

func init() { RegisterProvider(NewOpenAIChatGPTProvider(nil)) }

func NewOpenAIChatGPTProvider(client *http.Client) *OpenAIChatGPTProvider {
	if client == nil {
		client = http.DefaultClient
	}
	return &OpenAIChatGPTProvider{client: client, authorizeURL: openAIChatGPTAuthorizeURL, tokenURL: openAIChatGPTTokenURL, callbackHost: openAIChatGPTCallbackHost, callbackPort: openAIChatGPTCallbackPort, callbackPath: openAIChatGPTCallbackPath}
}

func (p *OpenAIChatGPTProvider) ID() string   { return openAIChatGPTProviderID }
func (p *OpenAIChatGPTProvider) Name() string { return "OpenAI (ChatGPT subscription)" }

func (p *OpenAIChatGPTProvider) Login(callbacks LoginCallbacks) (*Credentials, error) {
	return p.loginContext(context.Background(), callbacks)
}

func (p *OpenAIChatGPTProvider) RefreshToken(creds *Credentials) (*Credentials, error) {
	return p.RefreshTokenContext(context.Background(), creds)
}

func (p *OpenAIChatGPTProvider) RefreshTokenContext(ctx context.Context, creds *Credentials) (*Credentials, error) {
	if creds == nil || creds.Refresh == "" {
		return nil, fmt.Errorf("OpenAI ChatGPT OAuth refresh token is missing")
	}
	clientID := openAIChatGPTCredentialClientID(creds)
	if clientID == "" {
		return nil, fmt.Errorf("stored OpenAI OAuth credential does not contain an issued client ID; reconnect ChatGPT")
	}
	return p.requestToken(ctx, url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {creds.Refresh}, "resource": {openAIChatGPTResource}}, clientID, false)
}

func (p *OpenAIChatGPTProvider) GetAPIKey(creds *Credentials) string {
	if creds == nil {
		return ""
	}
	return creds.Access
}

func (p *OpenAIChatGPTProvider) ModifyModels(models []*goai.Model, creds *Credentials) []*goai.Model {
	return models
}

func (p *OpenAIChatGPTProvider) loginContext(ctx context.Context, callbacks LoginCallbacks) (*Credentials, error) {
	deviceID := p.resolveDeviceID()
	if !openAIChatGPTUUIDPattern.MatchString(deviceID) {
		return nil, fmt.Errorf("sign in with ChatGPT requires a device ID (UUID) for this installation")
	}
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}
	state, err := openAIChatGPTRandomValue()
	if err != nil {
		return nil, err
	}
	nonce, err := openAIChatGPTRandomValue()
	if err != nil {
		return nil, err
	}

	callback, err := startOAuthCallbackServer(ctx, oauthCallbackOptions{ProviderName: "ChatGPT", Host: p.callbackHost, Port: p.callbackPort, Path: p.callbackPath, State: state})
	if err != nil && callbacks.OnProgress != nil {
		callbacks.OnProgress(fmt.Sprintf("Could not listen for the OAuth callback; paste the final redirect URL to continue. %v", err))
	}
	if callback != nil {
		defer callback.Close()
	}
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d%s", openAIChatGPTCallbackPort, openAIChatGPTCallbackPath)
	if callback != nil {
		redirectURI = callback.RedirectURI
	}
	authURL, err := p.authorizationURL(deviceID, redirectURI, state, challenge, nonce)
	if err != nil {
		return nil, err
	}
	if callbacks.OnAuth != nil {
		callbacks.OnAuth(AuthInfo{URL: authURL, Instructions: "Complete sign-in in your browser. If the callback does not complete, paste the final redirect URL here."})
	}

	var result openAIChatGPTAuthorizationResult
	if callbacks.OnPrompt != nil {
		input, err := callbacks.OnPrompt(Prompt{Message: "Complete login in your browser, or paste the final redirect URL here:", Placeholder: redirectURI})
		if err != nil {
			return nil, err
		}
		result, err = openAIChatGPTAuthorizationResultFromManualInput(input, redirectURI, state)
		if err != nil {
			return nil, err
		}
	} else if callback != nil {
		callbackURL, err := callback.Wait()
		if err != nil {
			return nil, err
		}
		result, err = openAIChatGPTAuthorizationResultFromCallback(callbackURL, state)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("OpenAI ChatGPT OAuth requires a callback server or manual callback URL prompt")
	}

	if callbacks.OnProgress != nil {
		callbacks.OnProgress("Exchanging authorization code for tokens...")
	}
	return p.requestToken(ctx, url.Values{"grant_type": {"authorization_code"}, "client_id": {result.ClientID}, "code": {result.Code}, "code_verifier": {verifier}, "redirect_uri": {redirectURI}, "resource": {openAIChatGPTResource}}, result.ClientID, true)
}

func (p *OpenAIChatGPTProvider) authorizationURL(deviceID, redirectURI, state, challenge, nonce string) (string, error) {
	base, err := url.Parse(p.authorizeURL)
	if err != nil {
		return "", err
	}
	base.RawQuery = url.Values{
		"client_id":             {openAIChatGPTDynamicClientID},
		"agent_name_hint":       {openAIChatGPTAgentNameHint},
		"ext_agent_host_id":     {"urn:uuid:" + strings.ToLower(deviceID)},
		"response_type":         {"code"},
		"redirect_uri":          {redirectURI},
		"resource":              {openAIChatGPTResource},
		"scope":                 {openAIChatGPTScope},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"nonce":                 {nonce},
	}.Encode()
	return base.String(), nil
}

func (p *OpenAIChatGPTProvider) resolveDeviceID() string {
	if p.deviceID != "" {
		return p.deviceID
	}
	for _, key := range []string{"OPENAI_CHATGPT_DEVICE_ID", "PI_DEVICE_ID", "GOAI_DEVICE_ID"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return goai.UUIDv7()
}

func (p *OpenAIChatGPTProvider) requestToken(ctx context.Context, form url.Values, clientID string, requireIDToken bool) (*Credentials, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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
		return nil, fmt.Errorf("OpenAI OAuth token request failed (%d): %s", resp.StatusCode, string(body))
	}
	var token map[string]interface{}
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, err
	}
	if requireIDToken {
		if idToken, _ := token["id_token"].(string); strings.TrimSpace(idToken) == "" {
			return nil, fmt.Errorf("OpenAI OAuth token response did not contain an ID token")
		}
	}
	return openAIChatGPTCredentialFromTokenResponse(token, clientID)
}

type openAIChatGPTAuthorizationResult struct {
	Code     string
	ClientID string
}

func openAIChatGPTAuthorizationResultFromCallback(callbackURL *url.URL, expectedState string) (openAIChatGPTAuthorizationResult, error) {
	if callbackURL == nil {
		return openAIChatGPTAuthorizationResult{}, fmt.Errorf("missing callback URL")
	}
	code := callbackURL.Query().Get("code")
	if code == "" {
		return openAIChatGPTAuthorizationResult{}, fmt.Errorf("missing authorization code")
	}
	state := callbackURL.Query().Get("state")
	if state == "" {
		return openAIChatGPTAuthorizationResult{}, fmt.Errorf("missing OAuth state")
	}
	if state != expectedState {
		return openAIChatGPTAuthorizationResult{}, fmt.Errorf("OAuth state mismatch")
	}
	clientID := strings.TrimSpace(callbackURL.Query().Get("client_id"))
	if clientID == "" {
		return openAIChatGPTAuthorizationResult{}, fmt.Errorf("OpenAI OAuth registration callback did not contain an issued client ID")
	}
	return openAIChatGPTAuthorizationResult{Code: code, ClientID: clientID}, nil
}

func openAIChatGPTAuthorizationResultFromManualInput(input, redirectURI, expectedState string) (openAIChatGPTAuthorizationResult, error) {
	callbackURL, err := url.Parse(strings.TrimSpace(input))
	if err != nil || callbackURL.Scheme == "" || callbackURL.Host == "" {
		return openAIChatGPTAuthorizationResult{}, fmt.Errorf("paste the full callback URL from the browser")
	}
	expected, err := url.Parse(redirectURI)
	if err != nil {
		return openAIChatGPTAuthorizationResult{}, err
	}
	if callbackURL.Scheme != expected.Scheme || callbackURL.Host != expected.Host || callbackURL.Path != expected.Path {
		return openAIChatGPTAuthorizationResult{}, fmt.Errorf("the pasted callback URL must start with %s", redirectURI)
	}
	if oauthErr := callbackURL.Query().Get("error"); oauthErr != "" {
		return openAIChatGPTAuthorizationResult{}, fmt.Errorf("ChatGPT authorization failed: %s", oauthErr)
	}
	return openAIChatGPTAuthorizationResultFromCallback(callbackURL, expectedState)
}

func openAIChatGPTRandomValue() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func openAIChatGPTCredentialFromTokenResponse(token map[string]interface{}, clientID string) (*Credentials, error) {
	access, err := requiredOpenAIChatGPTTokenString(token, "access_token")
	if err != nil {
		return nil, err
	}
	refresh, err := requiredOpenAIChatGPTTokenString(token, "refresh_token")
	if err != nil {
		return nil, err
	}
	scope, err := requiredOpenAIChatGPTTokenString(token, "scope")
	if err != nil {
		return nil, err
	}
	expiresIn, ok := numericJSON(token["expires_in"])
	if !ok || expiresIn <= 0 {
		return nil, fmt.Errorf("OpenAI OAuth token response has invalid expires_in")
	}
	scopes := strings.Fields(scope)
	if !openAIChatGPTScopesContain(scopes, openAIChatGPTDirectTokenScope) {
		return nil, fmt.Errorf("OpenAI OAuth grant did not include %s", openAIChatGPTDirectTokenScope)
	}
	return &Credentials{Access: access, Refresh: refresh, Expires: time.Now().Add(time.Duration(expiresIn)*time.Second - openAIChatGPTExpirySkew).UnixMilli(), Extra: map[string]interface{}{"clientId": clientID, "scopes": scopes}}, nil
}

func requiredOpenAIChatGPTTokenString(token map[string]interface{}, field string) (string, error) {
	value, _ := token[field].(string)
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("OpenAI OAuth token response has invalid %s", field)
	}
	return value, nil
}

func openAIChatGPTCredentialClientID(creds *Credentials) string {
	if creds == nil || creds.Extra == nil {
		return ""
	}
	clientID, _ := creds.Extra["clientId"].(string)
	return strings.TrimSpace(clientID)
}

func openAIChatGPTScopesContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
