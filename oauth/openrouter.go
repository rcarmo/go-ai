package oauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	goai "github.com/rcarmo/go-ai"
)

const (
	openRouterAuthorizeURL = "https://openrouter.ai/auth"
	openRouterTokenURL     = "https://openrouter.ai/api/v1/auth/keys"
)

type OpenRouterProvider struct {
	client       *http.Client
	tokenURL     string
	authorizeURL string
}

func init() { RegisterProvider(NewOpenRouterProvider(nil)) }
func NewOpenRouterProvider(client *http.Client) *OpenRouterProvider {
	if client == nil {
		client = http.DefaultClient
	}
	return &OpenRouterProvider{client: client, tokenURL: openRouterTokenURL, authorizeURL: openRouterAuthorizeURL}
}
func (p *OpenRouterProvider) ID() string   { return string(goai.ProviderOpenRouter) }
func (p *OpenRouterProvider) Name() string { return "OpenRouter" }
func (p *OpenRouterProvider) GetAPIKey(creds *Credentials) string {
	if creds == nil {
		return ""
	}
	return creds.Access
}
func (p *OpenRouterProvider) ModifyModels(models []*goai.Model, creds *Credentials) []*goai.Model {
	return models
}
func (p *OpenRouterProvider) RefreshToken(creds *Credentials) (*Credentials, error) {
	return p.RefreshTokenContext(context.Background(), creds)
}

func (p *OpenRouterProvider) RefreshTokenContext(ctx context.Context, creds *Credentials) (*Credentials, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if creds == nil || creds.Access == "" {
		return nil, fmt.Errorf("OpenRouter OAuth key is missing")
	}
	return creds, nil
}

func (p *OpenRouterProvider) Login(callbacks LoginCallbacks) (*Credentials, error) {
	verifier, challenge := openRouterPKCE("go-ai-openrouter")
	if callbacks.OnAuth != nil {
		callbacks.OnAuth(AuthInfo{URL: p.authorizeURL + "?code_challenge=" + challenge + "&code_challenge_method=S256", Instructions: "Authorize OpenRouter and paste the returned code."})
	}
	if callbacks.OnPrompt == nil {
		return nil, fmt.Errorf("OpenRouter OAuth requires an authorization code prompt")
	}
	code, err := callbacks.OnPrompt(Prompt{Message: "OpenRouter authorization code", Placeholder: "code"})
	if err != nil {
		return nil, err
	}
	return p.exchangeAuthorizationCode(context.Background(), strings.TrimSpace(code), verifier)
}

func (p *OpenRouterProvider) exchangeAuthorizationCode(ctx context.Context, code, verifier string) (*Credentials, error) {
	if code == "" {
		return nil, fmt.Errorf("OpenRouter authorization code is missing")
	}
	body, _ := json.Marshal(map[string]string{"code": code, "code_verifier": verifier, "code_challenge_method": "S256"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	client := p.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("Login cancelled")
		}
		return nil, err
	}
	defer resp.Body.Close()
	var decoded map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil && resp.StatusCode < 300 {
		return nil, fmt.Errorf("OpenRouter OAuth returned invalid JSON")
	}
	if decoded == nil {
		decoded = map[string]interface{}{}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("OpenRouter OAuth key exchange failed (HTTP %d): %s", resp.StatusCode, oauthErrorDetail(decoded))
	}
	key, ok := decoded["key"].(string)
	if !ok || key == "" {
		return nil, fmt.Errorf("OpenRouter OAuth response carries no key")
	}
	return &Credentials{Access: key, Refresh: "", Expires: time.Now().Add(100 * 365 * 24 * time.Hour).UnixMilli()}, nil
}

func openRouterPKCE(seed string) (string, string) {
	sum := sha256.Sum256([]byte(seed))
	verifier := base64.RawURLEncoding.EncodeToString(sum[:])
	challengeBytes := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(challengeBytes[:])
}
