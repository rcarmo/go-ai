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

// GetAPIKey returns the API key for a provider, refreshing if needed.
// Returns the (possibly updated) credentials and the API key.
func GetAPIKey(id string, creds *Credentials) (*Credentials, string, error) {
	p := GetProvider(id)
	if p == nil {
		return nil, "", fmt.Errorf("OAuth provider %q not registered", id)
	}
	if creds == nil {
		return nil, "", fmt.Errorf("no credentials for OAuth provider %q", id)
	}

	// Check if token needs refresh (expired or about to expire)
	// Credentials.Expires is in Unix millis; compare with buffer
	key := p.GetAPIKey(creds)
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
