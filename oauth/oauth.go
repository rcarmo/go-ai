// Package oauth provides OAuth credential management for AI providers.
//
// Handles login flows, token refresh, and credential storage for
// OAuth-based providers: GitHub Copilot, Google Gemini CLI,
// Antigravity, Anthropic, and OpenAI Codex.
package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Credentials holds OAuth token data.
type Credentials struct {
	Refresh string `json:"refresh"`
	Access  string `json:"access"`
	Expires int64  `json:"expires"` // Unix millis

	// Provider-specific extra fields
	Extra map[string]interface{} `json:"extra,omitempty"`
}

// AuthInfo is passed to the user during login (URL to visit).
type AuthInfo struct {
	URL          string
	Instructions string
}

// Prompt asks the user for input during login.
type Prompt struct {
	Message     string
	Placeholder string
	AllowEmpty  bool
}

// LoginCallbacks are implemented by the host to handle OAuth UI.
type LoginCallbacks struct {
	OnAuth     func(info AuthInfo)
	OnPrompt   func(prompt Prompt) (string, error)
	OnProgress func(message string)
}

// ProviderInterface defines an OAuth provider's login/refresh contract.
type ProviderInterface interface {
	ID() string
	Name() string
	Login(callbacks LoginCallbacks) (*Credentials, error)
	RefreshToken(creds *Credentials) (*Credentials, error)
	GetAPIKey(creds *Credentials) string
	ModifyModels(models []*goai.Model, creds *Credentials) []*goai.Model
}

// --- Registry ---

var (
	registryMu sync.RWMutex
	providers  = map[string]ProviderInterface{}
)

// RegisterProvider registers an OAuth provider.
func RegisterProvider(p ProviderInterface) {
	registryMu.Lock()
	defer registryMu.Unlock()
	providers[p.ID()] = p
}

// GetProvider returns a registered OAuth provider by ID.
func GetProvider(id string) ProviderInterface {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return providers[id]
}

// ListProviders returns all registered OAuth providers.
func ListProviders() []ProviderInterface {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]ProviderInterface, 0, len(providers))
	for _, p := range providers {
		out = append(out, p)
	}
	return out
}

const defaultOAuthMinimumValidity = 5 * time.Minute

// GetAPIKey returns the API key for a provider, refreshing if needed.
// Returns the (possibly updated) credentials and the API key.
func GetAPIKey(id string, creds *Credentials) (*Credentials, string, error) {
	return GetAPIKeyWithMinValidity(id, creds, defaultOAuthMinimumValidity)
}

func GetAPIKeyWithMinValidity(id string, creds *Credentials, minValidity time.Duration) (*Credentials, string, error) {
	if minValidity < defaultOAuthMinimumValidity {
		minValidity = defaultOAuthMinimumValidity
	}
	p := GetProvider(id)
	if p == nil {
		return nil, "", goai.NewModelsError(goai.ModelsErrorAuth, fmt.Sprintf("OAuth provider %q not registered", id), nil)
	}
	if creds == nil {
		return nil, "", goai.NewModelsError(goai.ModelsErrorAuth, fmt.Sprintf("no credentials for OAuth provider %q", id), nil)
	}

	// Credentials.Expires already includes each provider's early-refresh buffer.
	expiresSoon := creds.Expires > 0 && time.Now().Add(minValidity).UnixMilli() >= creds.Expires
	if expiresSoon || (creds.Access == "" && creds.Refresh != "") {
		refreshed, err := p.RefreshToken(creds)
		if err != nil {
			return creds, "", goai.NewModelsError(goai.ModelsErrorOAuth, fmt.Sprintf("OAuth refresh failed for %s", id), err)
		}
		if refreshed != nil {
			creds = refreshed
		}
	}
	key := p.GetAPIKey(creds)
	if key == "" {
		return creds, "", goai.NewModelsError(goai.ModelsErrorAuth, fmt.Sprintf("OAuth auth derivation failed for %s", id), fmt.Errorf("provider returned empty API key"))
	}
	if minValidity > defaultOAuthMinimumValidity && creds.Expires > 0 && time.Now().Add(minValidity).UnixMilli() >= creds.Expires {
		return creds, "", goai.NewModelsError(goai.ModelsErrorOAuth, fmt.Sprintf("OAuth refresh returned a token that expires too soon for %s", id), nil)
	}
	return creds, key, nil
}

// --- PKCE ---

// GeneratePKCE creates a PKCE code verifier and challenge pair.
func GeneratePKCE() (verifier, challenge string, err error) {
	// Generate 32 random bytes for verifier
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", fmt.Errorf("PKCE: %w", err)
	}
	verifier = base64URLEncode(b)

	// Compute SHA-256 challenge
	hash := sha256.Sum256([]byte(verifier))
	challenge = base64URLEncode(hash[:])

	return verifier, challenge, nil
}

func base64URLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}
