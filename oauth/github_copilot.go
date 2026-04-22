// GitHub Copilot OAuth provider — device code flow.
package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	goai "github.com/rcarmo/go-ai"
)

var clientID = func() string {
	b, _ := base64.StdEncoding.DecodeString("SXYxLmI1MDdhMDhjODdlY2ZlOTg=")
	return string(b)
}()

var copilotHeaders = map[string]string{
	"User-Agent":              "GitHubCopilotChat/0.35.0",
	"Editor-Version":          "vscode/1.107.0",
	"Editor-Plugin-Version":   "copilot-chat/0.35.0",
	"Copilot-Integration-Id":  "vscode-chat",
}

// GitHubCopilotProvider implements the OAuth flow for GitHub Copilot.
type GitHubCopilotProvider struct{}

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

	// Exchange for Copilot token
	creds, err := RefreshGitHubCopilotToken(githubToken, enterpriseDomain)
	if err != nil {
		return nil, fmt.Errorf("copilot token: %w", err)
	}

	return creds, nil
}

func (p *GitHubCopilotProvider) RefreshToken(creds *Credentials) (*Credentials, error) {
	domain := ""
	if creds.Extra != nil {
		if d, ok := creds.Extra["enterpriseUrl"].(string); ok {
			domain = d
		}
	}
	return RefreshGitHubCopilotToken(creds.Refresh, domain)
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
	for _, m := range models {
		if m.Provider == goai.ProviderGitHubCopilot {
			m.BaseURL = baseURL
		}
	}
	return models
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
	interval := time.Duration(intervalSecs) * time.Second
	if interval < time.Second {
		interval = time.Second
	}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval):
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
			switch errStr {
			case "authorization_pending":
				continue
			case "slow_down":
				interval = interval * 14 / 10
				continue
			default:
				desc := ""
				if d, ok := raw["error_description"].(string); ok {
					desc = ": " + d
				}
				return "", fmt.Errorf("device flow failed: %s%s", errStr, desc)
			}
		}
	}

	return "", fmt.Errorf("device flow timed out")
}

// RefreshGitHubCopilotToken exchanges a GitHub access token for a Copilot API token.
func RefreshGitHubCopilotToken(refreshToken, enterpriseDomain string) (*Credentials, error) {
	domain := "github.com"
	if enterpriseDomain != "" {
		domain = enterpriseDomain
	}

	u := fmt.Sprintf("https://api.%s/copilot_internal/v2/token", domain)
	req, _ := http.NewRequest("GET", u, nil)
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
