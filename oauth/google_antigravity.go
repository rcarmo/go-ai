// Google Antigravity OAuth — same as Gemini CLI but with different endpoints.
// Antigravity is Google's consumer-facing Vertex AI gateway.
package oauth

import (
	goai "github.com/rcarmo/go-ai"
)

// AntigravityProvider implements Google OAuth for the Antigravity (consumer Vertex) gateway.
// Uses the same OAuth flow as Gemini CLI with different endpoint defaults.
type AntigravityProvider struct {
	GeminiCLIProvider // embed — same flow, different ID/name
}

func init() {
	RegisterProvider(&AntigravityProvider{})
}

func (p *AntigravityProvider) ID() string   { return "google-antigravity" }
func (p *AntigravityProvider) Name() string { return "Antigravity (Google Cloud)" }

func (p *AntigravityProvider) Login(callbacks LoginCallbacks) (*Credentials, error) {
	// Same flow as Gemini CLI
	return p.GeminiCLIProvider.Login(callbacks)
}

func (p *AntigravityProvider) RefreshToken(creds *Credentials) (*Credentials, error) {
	return p.GeminiCLIProvider.RefreshToken(creds)
}

func (p *AntigravityProvider) GetAPIKey(creds *Credentials) string {
	return p.GeminiCLIProvider.GetAPIKey(creds)
}

func (p *AntigravityProvider) ModifyModels(models []*goai.Model, creds *Credentials) []*goai.Model {
	return models
}
