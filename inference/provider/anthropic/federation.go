package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	goai "github.com/rcarmo/go-ai"
)

const (
	anthropicFederationRuleIDEnv       = "ANTHROPIC_FEDERATION_RULE_ID"
	anthropicOrganizationIDEnv         = "ANTHROPIC_ORGANIZATION_ID"
	anthropicServiceAccountIDEnv       = "ANTHROPIC_SERVICE_ACCOUNT_ID"
	anthropicIdentityTokenFileEnv      = "ANTHROPIC_IDENTITY_TOKEN_FILE"
	anthropicWorkspaceIDEnv            = "ANTHROPIC_WORKSPACE_ID"
	anthropicFederationGrantType       = "urn:ietf:params:oauth:grant-type:jwt-bearer"
	anthropicFederationOAuthBeta       = "oauth-2025-04-20"
	anthropicFederationOIDCBeta        = "oidc-federation-2026-04-01"
	anthropicFederationRefreshSkew     = 30 * time.Second
	anthropicFederationMaxResponseSize = 1 << 20
)

type anthropicFederationConfig struct {
	baseURL           string
	federationRuleID  string
	organizationID    string
	serviceAccountID  string
	identityTokenFile string
	workspaceID       string
}

func getAnthropicFederationConfig(model *goai.Model, env goai.ProviderEnv, baseURL string) (anthropicFederationConfig, bool) {
	if model == nil || model.Provider != goai.ProviderAnthropic {
		return anthropicFederationConfig{}, false
	}
	cfg := anthropicFederationConfig{
		baseURL:           strings.TrimRight(baseURL, "/"),
		federationRuleID:  goai.GetProviderEnvValue(anthropicFederationRuleIDEnv, env),
		organizationID:    goai.GetProviderEnvValue(anthropicOrganizationIDEnv, env),
		identityTokenFile: goai.GetProviderEnvValue(anthropicIdentityTokenFileEnv, env),
		serviceAccountID:  goai.GetProviderEnvValue(anthropicServiceAccountIDEnv, env),
		workspaceID:       goai.GetProviderEnvValue(anthropicWorkspaceIDEnv, env),
	}
	if cfg.federationRuleID == "" || cfg.organizationID == "" || cfg.identityTokenFile == "" {
		return anthropicFederationConfig{}, false
	}
	return cfg, true
}

func (c anthropicFederationConfig) cacheKey() string {
	parts := []string{c.baseURL, c.federationRuleID, c.organizationID, c.serviceAccountID, c.identityTokenFile, c.workspaceID}
	data, _ := json.Marshal(parts)
	return string(data)
}

type anthropicFederationToken struct {
	token     string
	expiresAt time.Time
}

type anthropicFederationCall struct {
	done  chan struct{}
	token anthropicFederationToken
	err   error
}

type anthropicFederationEntry struct {
	token    anthropicFederationToken
	inflight *anthropicFederationCall
}

var anthropicFederationCache = struct {
	sync.Mutex
	entries map[string]*anthropicFederationEntry
}{entries: map[string]*anthropicFederationEntry{}}

func resetAnthropicFederationCacheForTest() {
	anthropicFederationCache.Lock()
	defer anthropicFederationCache.Unlock()
	anthropicFederationCache.entries = map[string]*anthropicFederationEntry{}
}

func getAnthropicFederationBearer(ctx context.Context, client *http.Client, cfg anthropicFederationConfig) (string, error) {
	key := cfg.cacheKey()
	anthropicFederationCache.Lock()
	entry := anthropicFederationCache.entries[key]
	if entry == nil {
		entry = &anthropicFederationEntry{}
		anthropicFederationCache.entries[key] = entry
	}
	now := time.Now()
	if entry.token.token != "" && (entry.token.expiresAt.IsZero() || now.Before(entry.token.expiresAt.Add(-anthropicFederationRefreshSkew))) {
		token := entry.token.token
		anthropicFederationCache.Unlock()
		return token, nil
	}
	if call := entry.inflight; call != nil {
		anthropicFederationCache.Unlock()
		select {
		case <-call.done:
			if call.err != nil {
				return "", call.err
			}
			return call.token.token, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	call := &anthropicFederationCall{done: make(chan struct{})}
	entry.inflight = call
	anthropicFederationCache.Unlock()

	call.token, call.err = exchangeAnthropicFederationToken(ctx, client, cfg)

	anthropicFederationCache.Lock()
	if call.err == nil {
		entry.token = call.token
	}
	if entry.inflight == call {
		entry.inflight = nil
	}
	anthropicFederationCache.Unlock()
	close(call.done)

	if call.err != nil {
		return "", call.err
	}
	return call.token.token, nil
}

func exchangeAnthropicFederationToken(ctx context.Context, client *http.Client, cfg anthropicFederationConfig) (anthropicFederationToken, error) {
	if client == nil {
		client = http.DefaultClient
	}
	identityToken, err := readAnthropicIdentityTokenFile(cfg.identityTokenFile)
	if err != nil {
		return anthropicFederationToken{}, err
	}
	if len(identityToken) > 16*1024 {
		return anthropicFederationToken{}, fmt.Errorf("identity token is %d KiB, exceeds the 16 KiB assertion limit", (len(identityToken)+1023)/1024)
	}
	body := map[string]string{
		"grant_type":         anthropicFederationGrantType,
		"assertion":          identityToken,
		"federation_rule_id": cfg.federationRuleID,
		"organization_id":    cfg.organizationID,
	}
	if cfg.serviceAccountID != "" {
		body["service_account_id"] = cfg.serviceAccountID
	}
	if cfg.workspaceID != "" {
		body["workspace_id"] = cfg.workspaceID
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return anthropicFederationToken{}, err
	}
	url := strings.TrimRight(cfg.baseURL, "/") + "/oauth/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return anthropicFederationToken{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-beta", anthropicFederationOAuthBeta+","+anthropicFederationOIDCBeta)
	req.Header.Set("User-Agent", goai.PiUserAgent()+" oidcFederationProvider")
	resp, err := client.Do(req)
	if err != nil {
		return anthropicFederationToken{}, fmt.Errorf("failed to reach token endpoint %s: %w", url, err)
	}
	defer resp.Body.Close()
	requestID := resp.Header.Get("Request-Id")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		text := readLimitedAnthropicTokenBody(resp.Body)
		msg := fmt.Sprintf("token exchange failed with status %d", resp.StatusCode)
		if requestID != "" {
			msg += fmt.Sprintf(" (request-id %s)", requestID)
		}
		if redacted := redactAnthropicTokenBody(text, identityToken); redacted != "" {
			msg += ": " + redacted
		}
		return anthropicFederationToken{}, fmt.Errorf("%s", msg)
	}
	var parsed struct {
		AccessToken string  `json:"access_token"`
		ExpiresIn   float64 `json:"expires_in"`
		TokenType   string  `json:"token_type"`
	}
	text := readLimitedAnthropicTokenBody(resp.Body)
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return anthropicFederationToken{}, fmt.Errorf("token endpoint returned non-JSON response (status %d)", resp.StatusCode)
	}
	if parsed.AccessToken == "" {
		return anthropicFederationToken{}, fmt.Errorf("token endpoint response missing access_token: %s", redactAnthropicTokenBody(text, identityToken))
	}
	if parsed.TokenType != "" && !strings.EqualFold(parsed.TokenType, "bearer") {
		return anthropicFederationToken{}, fmt.Errorf("token endpoint response: unsupported token_type %q (want Bearer)", parsed.TokenType)
	}
	if parsed.ExpiresIn <= 0 || parsed.ExpiresIn != parsed.ExpiresIn {
		return anthropicFederationToken{}, fmt.Errorf("token endpoint response missing required fields: %s", redactAnthropicTokenBody(text, identityToken))
	}
	return anthropicFederationToken{token: parsed.AccessToken, expiresAt: time.Now().Add(time.Duration(parsed.ExpiresIn * float64(time.Second)))}, nil
}

func readAnthropicIdentityTokenFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read identity token file at %s: %w", path, err)
	}
	token := strings.TrimSpace(string(content))
	if token == "" {
		return "", fmt.Errorf("identity token file at %s is empty", path)
	}
	return token, nil
}

func readLimitedAnthropicTokenBody(body io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(body, anthropicFederationMaxResponseSize))
	return string(data)
}

func redactAnthropicTokenBody(text string, assertion string) string {
	if text == "" {
		return ""
	}
	var value any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		text = redactAnthropicAssertion(text, assertion)
		if len(text) > 2000 {
			return text[:2000] + fmt.Sprintf("... <%d more chars>", len(text)-2000)
		}
		return text
	}
	redacted := redactAnthropicTokenValue(value, assertion)
	data, err := json.Marshal(redacted)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func redactAnthropicTokenValue(value any, assertion string) any {
	obj, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]any{}
	for _, key := range []string{"error", "error_description", "error_uri"} {
		if v, ok := obj[key]; ok {
			out[key] = redactAnthropicTokenField(v, assertion)
		}
	}
	return out
}

func redactAnthropicTokenField(value any, assertion string) any {
	s, ok := value.(string)
	if !ok {
		return nil
	}
	return redactAnthropicAssertion(s, assertion)
}

func redactAnthropicAssertion(text string, assertion string) string {
	if assertion == "" {
		return text
	}
	return strings.ReplaceAll(text, assertion, "[REDACTED]")
}
