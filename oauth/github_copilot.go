// GitHub Copilot OAuth provider — device code flow.
package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	goai "github.com/rcarmo/go-ai"
)

var clientID = func() string {
	b, _ := base64.StdEncoding.DecodeString("SXYxLmI1MDdhMDhjODdlY2ZlOTg=")
	return string(b)
}()

var copilotHeaders = map[string]string{
	"User-Agent":             "GitHubCopilotChat/0.35.0",
	"Editor-Version":         "vscode/1.107.0",
	"Editor-Plugin-Version":  "copilot-chat/0.35.0",
	"Copilot-Integration-Id": "vscode-chat",
}

const copilotAPIVersion = "2026-06-01"

var copilotPolicyHTTPClient = &http.Client{Timeout: 5 * time.Second}
var githubCopilotDevicePollWait = func(ctx context.Context, delay time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}
var copilotPolicyListModels = func() []*goai.Model {
	goai.RegisterBuiltinModels()
	return goai.ListModels(goai.ProviderGitHubCopilot)
}
var copilotPolicyBaseURL = GetGitHubCopilotBaseURL

// GitHubCopilotProvider implements the OAuth flow for GitHub Copilot.
type GitHubCopilotProvider struct{}

type copilotModelCatalog struct {
	AvailableModelIDs []string
	PolicyModelIDs    []string
}

func init() {
	RegisterProvider(&GitHubCopilotProvider{})
}

func (p *GitHubCopilotProvider) ID() string   { return "github-copilot" }
func (p *GitHubCopilotProvider) Name() string { return "GitHub Copilot" }

func (p *GitHubCopilotProvider) Login(callbacks LoginCallbacks) (*Credentials, error) {
	// Prompt for enterprise domain
	domainInput := ""
	if callbacks.OnPrompt != nil {
		var err error
		domainInput, err = callbacks.OnPrompt(Prompt{
			Message:     "GitHub Enterprise URL/domain (blank for github.com)",
			Placeholder: "company.ghe.com",
			AllowEmpty:  true,
		})
		if err != nil {
			return nil, err
		}
	}

	domain := "github.com"
	enterpriseDomain := ""
	if trimmed := strings.TrimSpace(domainInput); trimmed != "" {
		normalized := NormalizeDomain(trimmed)
		if normalized == "" {
			return nil, fmt.Errorf("invalid GitHub Enterprise URL/domain")
		}
		domain = normalized
		enterpriseDomain = normalized
	}

	// Start device flow
	device, err := startDeviceFlow(domain)
	if err != nil {
		return nil, fmt.Errorf("device flow: %w", err)
	}

	// Notify user
	if callbacks.OnAuth != nil {
		callbacks.OnAuth(AuthInfo{
			URL:          device.VerificationURI,
			Instructions: fmt.Sprintf("Enter code: %s", device.UserCode),
		})
	}

	// Poll for access token
	ctx := context.Background()
	githubToken, err := pollForAccessToken(ctx, domain, device.DeviceCode, device.Interval, device.ExpiresIn)
	if err != nil {
		return nil, fmt.Errorf("access token: %w", err)
	}

	// Exchange for Copilot token, fetch account availability, then enable requested policies.
	creds, err := refreshGitHubCopilotAccessToken(ctx, githubToken, enterpriseDomain)
	if err != nil {
		return nil, fmt.Errorf("copilot token: %w", err)
	}
	catalog, err := FetchGitHubCopilotModelCatalogContext(ctx, creds.Access, enterpriseDomain)
	if err != nil {
		return nil, fmt.Errorf("copilot models: %w", err)
	}
	ids := append([]string(nil), catalog.AvailableModelIDs...)
	if callbacks.OnProgress != nil && len(catalog.PolicyModelIDs) > 0 {
		callbacks.OnProgress("Enabling models...")
	}
	ids = uniqueCopilotModelIDs(append(ids, EnableGitHubCopilotModels(ctx, creds.Access, enterpriseDomain, catalog.PolicyModelIDs)...))
	if creds.Extra == nil {
		creds.Extra = map[string]interface{}{}
	}
	creds.Extra["availableModelIds"] = ids
	return creds, nil
}

func (p *GitHubCopilotProvider) RefreshToken(creds *Credentials) (*Credentials, error) {
	return p.RefreshTokenContext(context.Background(), creds)
}

func (p *GitHubCopilotProvider) RefreshTokenContext(ctx context.Context, creds *Credentials) (*Credentials, error) {
	if creds == nil || creds.Refresh == "" {
		return nil, fmt.Errorf("GitHub Copilot OAuth refresh token is missing")
	}
	domain := ""
	if creds.Extra != nil {
		if d, ok := creds.Extra["enterpriseUrl"].(string); ok {
			domain = d
		}
	}
	return RefreshGitHubCopilotTokenContext(ctx, creds.Refresh, domain)
}

func (p *GitHubCopilotProvider) GetAPIKey(creds *Credentials) string {
	return creds.Access
}

func (p *GitHubCopilotProvider) ModifyModels(models []*goai.Model, creds *Credentials) []*goai.Model {
	domain := ""
	if creds.Extra != nil {
		if d, ok := creds.Extra["enterpriseUrl"].(string); ok {
			domain = d
		}
	}
	baseURL := GetGitHubCopilotBaseURL(creds.Access, domain)
	available := availableCopilotModelSet(creds)
	out := make([]*goai.Model, 0, len(models))
	for _, m := range models {
		if m.Provider != goai.ProviderGitHubCopilot {
			out = append(out, m)
			continue
		}
		if available != nil && !available[m.ID] {
			continue
		}
		m.BaseURL = baseURL
		out = append(out, m)
	}
	return out
}

// --- Device flow ---

type deviceFlowResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	Interval        int    `json:"interval"`
	ExpiresIn       int    `json:"expires_in"`
}

func startDeviceFlow(domain string) (*deviceFlowResponse, error) {
	u := fmt.Sprintf("https://%s/login/device/code", domain)
	body := url.Values{
		"client_id": {clientID},
		"scope":     {"read:user"},
	}

	req, _ := http.NewRequest("POST", u, strings.NewReader(body.Encode()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "GitHubCopilotChat/0.35.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}

	var result deviceFlowResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func pollForAccessToken(ctx context.Context, domain, deviceCode string, intervalSecs, expiresIn int) (string, error) {
	u := fmt.Sprintf("https://%s/login/oauth/access_token", domain)
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	interval := normalizeDeviceCodePollInterval(intervalSecs)

	for time.Now().Before(deadline) {
		if err := githubCopilotDevicePollWait(ctx, interval); err != nil {
			return "", err
		}

		body := url.Values{
			"client_id":   {clientID},
			"device_code": {deviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}

		req, _ := http.NewRequest("POST", u, strings.NewReader(body.Encode()))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", "GitHubCopilotChat/0.35.0")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}

		var raw map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&raw)
		resp.Body.Close()

		if token, ok := raw["access_token"].(string); ok {
			return token, nil
		}

		if errStr, ok := raw["error"].(string); ok {
			if nextInterval, ok := nextDeviceCodePollIntervalWithServerInterval(interval, errStr, deviceCodeServerInterval(raw)); ok {
				interval = nextInterval
				continue
			}
			desc := ""
			if d, ok := raw["error_description"].(string); ok {
				desc = ": " + d
			}
			return "", fmt.Errorf("device flow failed: %s%s", errStr, desc)
		}
	}

	return "", fmt.Errorf("device flow timed out")
}

// RefreshGitHubCopilotToken exchanges a GitHub access token for a Copilot API token.
func RefreshGitHubCopilotToken(refreshToken, enterpriseDomain string) (*Credentials, error) {
	return RefreshGitHubCopilotTokenContext(context.Background(), refreshToken, enterpriseDomain)
}

func RefreshGitHubCopilotTokenContext(ctx context.Context, refreshToken, enterpriseDomain string) (*Credentials, error) {
	creds, err := refreshGitHubCopilotAccessToken(ctx, refreshToken, enterpriseDomain)
	if err != nil {
		return nil, err
	}
	catalog, err := FetchGitHubCopilotModelCatalogContext(ctx, creds.Access, enterpriseDomain)
	if err != nil {
		return nil, err
	}
	ids := catalog.AvailableModelIDs
	if creds.Extra == nil {
		creds.Extra = map[string]interface{}{}
	}
	creds.Extra["availableModelIds"] = ids
	return creds, nil
}

func refreshGitHubCopilotAccessToken(ctx context.Context, refreshToken, enterpriseDomain string) (*Credentials, error) {
	domain := "github.com"
	if enterpriseDomain != "" {
		domain = enterpriseDomain
	}

	u := fmt.Sprintf("https://api.%s/copilot_internal/v2/token", domain)
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+refreshToken)
	for k, v := range copilotHeaders {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}

	var raw struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	return &Credentials{
		Refresh: refreshToken,
		Access:  raw.Token,
		Expires: raw.ExpiresAt*1000 - 5*60*1000, // 5 min buffer
		Extra:   map[string]interface{}{"enterpriseUrl": enterpriseDomain},
	}, nil
}

const copilotPolicyConcurrency = 4
const copilotPolicyRetryBudget = 5 * time.Second

var errCopilotPolicyRateLimited = fmt.Errorf("copilot model policy rate limited")

// EnableAllGitHubCopilotModels enables all known Copilot model policies where required.
func EnableAllGitHubCopilotModels(copilotToken, enterpriseDomain string) {
	models := copilotPolicyListModels()
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	_ = EnableGitHubCopilotModels(context.Background(), copilotToken, enterpriseDomain, ids)
}

// EnableGitHubCopilotModels enables selected model policies in catalog order.
func EnableGitHubCopilotModels(ctx context.Context, copilotToken, enterpriseDomain string, modelIDs []string) []string {
	enabled := make([]string, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		if ctx.Err() != nil {
			return enabled
		}
		ok, err := EnableGitHubCopilotModelContext(ctx, copilotToken, modelID, enterpriseDomain)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, errCopilotPolicyRateLimited) {
				return enabled
			}
			continue
		}
		if ok {
			enabled = append(enabled, modelID)
		}
	}
	return enabled
}

// EnableGitHubCopilotModel enables one Copilot model policy where required.
func EnableGitHubCopilotModel(copilotToken, modelID, enterpriseDomain string) bool {
	ok, _ := EnableGitHubCopilotModelContext(context.Background(), copilotToken, modelID, enterpriseDomain)
	return ok
}

func EnableGitHubCopilotModelContext(ctx context.Context, copilotToken, modelID, enterpriseDomain string) (bool, error) {
	baseURL := copilotPolicyBaseURL(copilotToken, enterpriseDomain)
	endpoint := strings.TrimRight(baseURL, "/") + "/models/" + url.PathEscape(modelID) + "/policy"
	deadline := time.Now().Add(copilotPolicyRetryBudget)
	for attempt := 0; attempt <= 2; attempt++ {
		body := strings.NewReader(`{"state":"enabled"}`)
		req, _ := http.NewRequestWithContext(ctx, "POST", endpoint, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+copilotToken)
		req.Header.Set("openai-intent", "chat-policy")
		req.Header.Set("x-interaction-type", "chat-policy")
		for k, v := range copilotHeaders {
			req.Header.Set(k, v)
		}
		resp, err := copilotPolicyHTTPClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return false, nil
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt < 2 {
			delay := copilotRetryAfter(resp.Header, attempt)
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			if delay >= time.Until(deadline) {
				return false, errCopilotPolicyRateLimited
			}
			if err := sleepContext(ctx, delay); err != nil {
				return false, err
			}
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			return false, fmt.Errorf("%w: HTTP %d: %s", errCopilotPolicyRateLimited, resp.StatusCode, string(b))
		}
		return resp.StatusCode >= 200 && resp.StatusCode < 300, nil
	}
	return false, nil
}

func copilotRetryAfter(header http.Header, attempt int) time.Duration {
	if raw := header.Get("Retry-After"); raw != "" {
		if seconds, err := strconv.ParseFloat(raw, 64); err == nil && seconds >= 0 {
			return time.Duration(seconds * float64(time.Second))
		}
		if when, err := http.ParseTime(raw); err == nil {
			if d := time.Until(when); d > 0 {
				return d
			}
			return 0
		}
	}
	return time.Duration(500*(1<<attempt)) * time.Millisecond
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// FetchAvailableGitHubCopilotModelIDs returns account-selectable Copilot model IDs.
func FetchAvailableGitHubCopilotModelIDs(copilotToken, enterpriseDomain string) ([]string, error) {
	return FetchAvailableGitHubCopilotModelIDsContext(context.Background(), copilotToken, enterpriseDomain)
}

func FetchAvailableGitHubCopilotModelIDsContext(ctx context.Context, copilotToken, enterpriseDomain string) ([]string, error) {
	catalog, err := FetchGitHubCopilotModelCatalogContext(ctx, copilotToken, enterpriseDomain)
	if err != nil {
		return nil, err
	}
	return catalog.AvailableModelIDs, nil
}

func FetchGitHubCopilotModelCatalogContext(ctx context.Context, copilotToken, enterpriseDomain string) (copilotModelCatalog, error) {
	baseURL := copilotPolicyBaseURL(copilotToken, enterpriseDomain)
	req, _ := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(baseURL, "/")+"/models", nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+copilotToken)
	req.Header.Set("X-GitHub-Api-Version", copilotAPIVersion)
	for k, v := range copilotHeaders {
		req.Header.Set(k, v)
	}
	resp, err := copilotPolicyHTTPClient.Do(req)
	if err != nil {
		return copilotModelCatalog{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return copilotModelCatalog{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	var raw struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return copilotModelCatalog{}, err
	}
	return parseGitHubCopilotModelCatalog(raw.Data, strings.TrimRight(baseURL, "/") == "https://api.individual.githubcopilot.com"), nil
}

func parseGitHubCopilotModelCatalog(items []map[string]interface{}, allowPolicyFallback bool) copilotModelCatalog {
	known := map[string]bool{}
	for _, model := range copilotPolicyListModels() {
		known[model.ID] = true
	}
	var available []string
	var policy []string
	type accountModel struct {
		id          string
		picker      bool
		policyState string
		toolCapable bool
	}
	account := make([]accountModel, 0, len(items))
	for _, item := range items {
		id, _ := item["id"].(string)
		if id == "" {
			continue
		}
		toolCapable := true
		if capabilities, ok := item["capabilities"].(map[string]interface{}); ok {
			if supports, ok := capabilities["supports"].(map[string]interface{}); ok {
				if toolCalls, ok := supports["tool_calls"].(bool); ok {
					toolCapable = toolCalls
				}
			}
		}
		if !toolCapable {
			continue
		}
		policyState := ""
		if p, ok := item["policy"].(map[string]interface{}); ok {
			policyState, _ = p["state"].(string)
		}
		picker, _ := item["model_picker_enabled"].(bool)
		account = append(account, accountModel{id: id, picker: picker, policyState: policyState, toolCapable: toolCapable})
	}
	for _, model := range account {
		if model.picker && model.policyState != "disabled" {
			available = append(available, model.id)
		}
	}
	usePolicyFallback := allowPolicyFallback && len(available) == 0
	if usePolicyFallback {
		for _, model := range account {
			if model.policyState == "enabled" {
				available = append(available, model.id)
			}
		}
	}
	for _, model := range account {
		if model.policyState == "unconfigured" && known[model.id] && (model.picker || usePolicyFallback) {
			policy = append(policy, model.id)
		}
	}
	return copilotModelCatalog{AvailableModelIDs: uniqueCopilotModelIDs(available), PolicyModelIDs: uniqueCopilotModelIDs(policy)}
}

func uniqueCopilotModelIDs(ids []string) []string {
	if len(ids) == 0 {
		return ids
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func isSelectableCopilotModel(item map[string]interface{}) bool {
	if enabled, _ := item["model_picker_enabled"].(bool); !enabled {
		return false
	}
	if policy, ok := item["policy"].(map[string]interface{}); ok {
		if state, _ := policy["state"].(string); state == "disabled" {
			return false
		}
	}
	if capabilities, ok := item["capabilities"].(map[string]interface{}); ok {
		if supports, ok := capabilities["supports"].(map[string]interface{}); ok {
			if toolCalls, ok := supports["tool_calls"].(bool); ok && !toolCalls {
				return false
			}
		}
	}
	return true
}

func availableCopilotModelSet(creds *Credentials) map[string]bool {
	if creds == nil || creds.Extra == nil {
		return nil
	}
	raw, ok := creds.Extra["availableModelIds"]
	if !ok {
		return nil
	}
	out := map[string]bool{}
	switch v := raw.(type) {
	case []string:
		for _, id := range v {
			out[id] = true
		}
	case []interface{}:
		for _, item := range v {
			if id, ok := item.(string); ok {
				out[id] = true
			}
		}
	}
	return out
}

// GetGitHubCopilotBaseURL extracts the API base URL from a Copilot token.
func GetGitHubCopilotBaseURL(token, enterpriseDomain string) string {
	if token != "" {
		re := regexp.MustCompile(`proxy-ep=([^;]+)`)
		if m := re.FindStringSubmatch(token); len(m) > 1 {
			apiHost := strings.Replace(m[1], "proxy.", "api.", 1)
			return "https://" + apiHost
		}
	}
	if enterpriseDomain != "" {
		return "https://copilot-api." + enterpriseDomain
	}
	return "https://api.individual.githubcopilot.com"
}

// NormalizeDomain extracts a clean hostname from user input.
func NormalizeDomain(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
