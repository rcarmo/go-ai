package oauth

import (
	"context"
	"fmt"

	goai "github.com/rcarmo/go-ai"
)

// RuntimeCredentials is the provider-specific credential material needed to
// call go-ai after an OAuth login or refresh.
type RuntimeCredentials struct {
	// Credentials are the original or refreshed credentials. Persist this value
	// if it differs from the caller's stored credentials.
	Credentials *Credentials
	// APIKey is the bearer/API token to pass in goai.StreamOptions.APIKey.
	APIKey string
	// Models is the built-in model registry filtered/modified by the OAuth
	// provider. For GitHub Copilot this applies account availability and token
	// derived base URLs.
	Models []*goai.Model
}

// StreamOptions returns request options pre-populated with the runtime API key.
func (r *RuntimeCredentials) StreamOptions() *goai.StreamOptions {
	if r == nil {
		return &goai.StreamOptions{}
	}
	return &goai.StreamOptions{APIKey: r.APIKey}
}

// ModelPickerItems returns stable provider/id labels for a UI model picker.
func (r *RuntimeCredentials) ModelPickerItems(provider goai.Provider) []string {
	if r == nil {
		return nil
	}
	return goai.ModelPickerItems(r.Models, provider)
}

// FindModel resolves a UI/storage model reference against this runtime's
// filtered model list.
func (r *RuntimeCredentials) FindModel(ref goai.ModelRef) *goai.Model {
	if r == nil {
		return nil
	}
	return goai.FindModelByRef(r.Models, ref)
}

// SelectModel resolves a provider/id string from a UI picker or persisted
// setting against this runtime's filtered model list.
func (r *RuntimeCredentials) SelectModel(selection string) (*goai.Model, error) {
	ref, err := goai.ParseModelRef(selection)
	if err != nil {
		return nil, err
	}
	model := r.FindModel(ref)
	if model == nil {
		return nil, fmt.Errorf("model %q is not available", selection)
	}
	return model, nil
}

// SwitchContextForModel transforms a conversation for a selected runtime model.
func (r *RuntimeCredentials) SwitchContextForModel(ctx *goai.Context, model *goai.Model) *goai.Context {
	return goai.SwitchModel(ctx, model)
}

// RuntimeForProvider refreshes OAuth credentials if needed, extracts the API
// key, registers built-in models, and returns provider-adjusted models. This is
// the end-to-end bridge from oauth.Login/RefreshToken to goai.Stream/Complete.
func RuntimeForProvider(id string, creds *Credentials) (*RuntimeCredentials, error) {
	provider := GetProvider(id)
	if provider == nil {
		return nil, fmt.Errorf("OAuth provider %q not registered", id)
	}
	updated, apiKey, err := GetAPIKey(id, creds)
	if err != nil {
		return nil, err
	}
	goai.RegisterBuiltinModels()
	if dynamic, ok := provider.(interface {
		goai.DynamicModelProvider
		SetRuntimeCredentials(*Credentials)
	}); ok {
		dynamic.SetRuntimeCredentials(updated)
		if len(dynamic.StaticModels()) == 0 && updated != nil && updated.Refresh != "" {
			refreshed, err := provider.RefreshToken(updated)
			if err != nil {
				return nil, err
			}
			if refreshed != nil {
				updated = refreshed
				apiKey = provider.GetAPIKey(updated)
				dynamic.SetRuntimeCredentials(updated)
			}
		}
		goai.RegisterDynamicModelProvider(dynamic)
		goai.RefreshModels(context.Background(), true)
	}
	models := provider.ModifyModels(goai.ListModels(""), updated)
	return &RuntimeCredentials{Credentials: updated, APIKey: apiKey, Models: models}, nil
}

// RuntimeForGitHubCopilot is a convenience wrapper around RuntimeForProvider
// for the GitHub Copilot OAuth provider.
func RuntimeForGitHubCopilot(creds *Credentials) (*RuntimeCredentials, error) {
	return RuntimeForProvider("github-copilot", creds)
}
