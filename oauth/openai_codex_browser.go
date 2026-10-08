package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const codexBrowserClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
const codexBrowserRedirect = "http://localhost:1455/auth/callback"

func (p *OpenAICodexProvider) browserAuthorizationURL(state, challenge string, agentName *string) (string, error) {
	endpoint := p.authorizeURL
	if endpoint == "" {
		endpoint = "https://auth.openai.com/oauth/authorize"
	}
	u, e := url.Parse(endpoint)
	if e != nil {
		return "", e
	}
	originator := "pi"
	if agentName != nil {
		originator = *agentName
	}
	u.RawQuery = url.Values{"response_type": {"code"}, "client_id": {codexBrowserClientID}, "redirect_uri": {codexBrowserRedirect}, "scope": {codexScope}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}, "state": {state}, "id_token_add_organizations": {"true"}, "codex_cli_simplified_flow": {"true"}, "originator": {originator}}.Encode()
	return u.String(), nil
}
func (p *OpenAICodexProvider) loginBrowser(ctx context.Context, callbacks LoginCallbacks) (*Credentials, error) {
	verifier, challenge, e := GeneratePKCE()
	if e != nil {
		return nil, e
	}
	state, e := openAIChatGPTRandomValue()
	if e != nil {
		return nil, e
	}
	port := p.callbackPort
	if port == 0 {
		port = 1455
	}
	var code string
	callback, listenErr := startOAuthCallbackServer(ctx, oauthCallbackOptions{ProviderName: "OpenAI Codex", Host: "127.0.0.1", Port: port, Path: "/auth/callback", State: state, Listen: p.listen, Complete: func(_ context.Context, u *url.URL) error { code = u.Query().Get("code"); return nil }})
	if listenErr != nil {
		callback = nil
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if callbacks.OnPromptContext == nil && callbacks.OnPrompt == nil {
			return nil, fmt.Errorf("callback server: %w", listenErr)
		}
	} else {
		defer callback.Close()
	}
	authURL, e := p.browserAuthorizationURL(state, challenge, callbacks.AgentName)
	if e != nil {
		return nil, e
	}
	if callbacks.OnAuth != nil {
		callbacks.OnAuth(AuthInfo{URL: authURL, Instructions: "Complete login in your browser, or paste the final redirect URL."})
	}
	prompt := Prompt{Message: "Paste the authorization code / redirect URL, or complete browser login:", Placeholder: codexBrowserRedirect, AllowEmpty: false}
	var input string
	if callback == nil {
		if callbacks.OnPromptContext != nil {
			input, e = callbacks.OnPromptContext(ctx, prompt)
		} else {
			input, e = callbacks.OnPrompt(prompt)
		}
	} else if callbacks.OnPromptContext == nil {
		if callbacks.OnPrompt != nil {
			input, e = callbacks.OnPrompt(prompt)
		} else {
			_, e = callback.Wait()
		}
	} else {
		promptCtx, cancel := context.WithCancel(ctx)
		type answer struct {
			input string
			err   error
		}
		answers := make(chan answer, 1)
		promptDone := make(chan struct{})
		go func() {
			defer close(promptDone)
			value, err := callbacks.OnPromptContext(promptCtx, prompt)
			answers <- answer{value, err}
		}()
		completed := make(chan error, 1)
		callbackDone := make(chan struct{})
		go func() { defer close(callbackDone); _, err := callback.Wait(); completed <- err }()
		defer func() { cancel(); <-promptDone; callback.Close(); <-callbackDone }()
		select {
		case e = <-completed:
		case result := <-answers:
			input, e = result.input, result.err
			callback.Close()
			<-callbackDone
		case <-ctx.Done():
			e = ctx.Err()
		}
	}
	if e != nil {
		return nil, e
	}
	if input != "" {
		// Legacy synchronous prompt input also competes with the listener;
		// join callback handlers before assigning the selected authorization code.
		if callback != nil {
			callback.Close()
		}
		var pasted string
		code, pasted, e = parseAnthropicCopyCodeInput(input)
		if e != nil {
			return nil, e
		}
		if pasted != "" && pasted != state {
			return nil, fmt.Errorf("oauth state mismatch")
		}
	}
	if code == "" {
		return nil, fmt.Errorf("missing authorization code")
	}
	return p.browserToken(ctx, url.Values{"grant_type": {"authorization_code"}, "client_id": {codexBrowserClientID}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {codexBrowserRedirect}}, "")
}
func (p *OpenAICodexProvider) browserToken(ctx context.Context, form url.Values, priorRefresh string) (*Credentials, error) {
	endpoint := p.tokenURL
	if endpoint == "" {
		endpoint = "https://auth.openai.com/oauth/token"
	}
	client := p.client
	if client == nil {
		client = http.DefaultClient
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, e := client.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	data, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if e != nil {
		return nil, e
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("OpenAI Codex token exchange failed (%d): %s", resp.StatusCode, data)
	}
	var token struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		Expires int64  `json:"expires_in"`
	}
	if e = json.Unmarshal(data, &token); e != nil {
		return nil, e
	}
	if token.Refresh == "" {
		token.Refresh = priorRefresh
	}
	if token.Access == "" || token.Refresh == "" || token.Expires <= 0 || token.Expires > (math.MaxInt64-time.Now().UnixMilli())/1000 {
		return nil, fmt.Errorf("invalid OpenAI Codex token response")
	}
	parts := strings.Split(token.Access, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid Codex access token")
	}
	payload, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return nil, e
	}
	var claims map[string]any
	if e = json.Unmarshal(payload, &claims); e != nil {
		return nil, e
	}
	auth, _ := claims["https://api.openai.com/auth"].(map[string]any)
	account, _ := auth["chatgpt_account_id"].(string)
	if account == "" {
		return nil, fmt.Errorf("failed to extract Codex accountId")
	}
	return &Credentials{Access: token.Access, Refresh: token.Refresh, Expires: time.Now().UnixMilli() + token.Expires*1000, Extra: map[string]any{"clientId": codexBrowserClientID, "accountId": account}}, nil
}
