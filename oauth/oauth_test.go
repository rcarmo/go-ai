package oauth

import (
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestPKCE(t *testing.T) {
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		t.Fatal(err)
	}
	if len(verifier) == 0 {
		t.Fatal("empty verifier")
	}
	if len(challenge) == 0 {
		t.Fatal("empty challenge")
	}
	if verifier == challenge {
		t.Fatal("verifier and challenge should differ")
	}

	// Second call should produce different values (random)
	v2, c2, err := GeneratePKCE()
	if err != nil {
		t.Fatal(err)
	}
	if v2 == verifier {
		t.Fatal("PKCE should be random")
	}
	if c2 == challenge {
		t.Fatal("PKCE challenges should differ")
	}
}

func TestNormalizeDomain(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"github.com", "github.com"},
		{"https://github.com", "github.com"},
		{"https://company.ghe.com/", "company.ghe.com"},
		{"company.ghe.com", "company.ghe.com"},
		{"  github.com  ", "github.com"},
		{"", ""},
	}
	for _, tt := range tests {
		got := NormalizeDomain(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeDomain(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestGetGitHubCopilotBaseURL(t *testing.T) {
	// Default
	url := GetGitHubCopilotBaseURL("", "")
	if url != "https://api.individual.githubcopilot.com" {
		t.Fatalf("expected default URL, got %q", url)
	}

	// Enterprise
	url = GetGitHubCopilotBaseURL("", "company.ghe.com")
	if url != "https://copilot-api.company.ghe.com" {
		t.Fatalf("expected enterprise URL, got %q", url)
	}

	// Token with proxy-ep
	token := "tid=abc;exp=123;proxy-ep=proxy.individual.githubcopilot.com;sku=abc"
	url = GetGitHubCopilotBaseURL(token, "")
	if url != "https://api.individual.githubcopilot.com" {
		t.Fatalf("expected URL from token, got %q", url)
	}
}

func TestGitHubCopilotModelFiltering(t *testing.T) {
	provider := &GitHubCopilotProvider{}
	models := []*goai.Model{
		{ID: "keep", Provider: goai.ProviderGitHubCopilot},
		{ID: "drop", Provider: goai.ProviderGitHubCopilot},
		{ID: "other", Provider: goai.ProviderOpenAI},
	}
	filtered := provider.ModifyModels(models, &Credentials{Access: "tok", Extra: map[string]interface{}{"availableModelIds": []interface{}{"keep"}}})
	if len(filtered) != 2 || filtered[0].ID != "keep" || filtered[0].BaseURL == "" || filtered[1].ID != "other" {
		t.Fatalf("unexpected filtered models: %#v", filtered)
	}
	unfiltered := provider.ModifyModels(models, &Credentials{Access: "tok"})
	if len(unfiltered) != 3 {
		t.Fatalf("expected old credentials to preserve generated catalog, got %d", len(unfiltered))
	}
}

func TestIsSelectableCopilotModel(t *testing.T) {
	if !isSelectableCopilotModel(map[string]interface{}{"model_picker_enabled": true}) {
		t.Fatal("expected enabled model to be selectable")
	}
	if isSelectableCopilotModel(map[string]interface{}{"model_picker_enabled": false}) {
		t.Fatal("expected hidden model to be filtered")
	}
	if isSelectableCopilotModel(map[string]interface{}{"model_picker_enabled": true, "policy": map[string]interface{}{"state": "disabled"}}) {
		t.Fatal("expected disabled policy model to be filtered")
	}
	if isSelectableCopilotModel(map[string]interface{}{"model_picker_enabled": true, "capabilities": map[string]interface{}{"supports": map[string]interface{}{"tool_calls": false}}}) {
		t.Fatal("expected non-tool-call model to be filtered")
	}
}

type testOAuthProvider struct {
	refreshes int
}

func (p *testOAuthProvider) ID() string                                 { return "test-refresh" }
func (p *testOAuthProvider) Name() string                               { return "Test Refresh" }
func (p *testOAuthProvider) Login(LoginCallbacks) (*Credentials, error) { return nil, nil }
func (p *testOAuthProvider) RefreshToken(creds *Credentials) (*Credentials, error) {
	p.refreshes++
	return &Credentials{Refresh: creds.Refresh, Access: "new-token", Expires: time.Now().Add(time.Hour).UnixMilli()}, nil
}
func (p *testOAuthProvider) GetAPIKey(creds *Credentials) string { return creds.Access }
func (p *testOAuthProvider) ModifyModels(models []*goai.Model, creds *Credentials) []*goai.Model {
	return models
}

func TestGetAPIKeyRefreshesExpiredCredential(t *testing.T) {
	provider := &testOAuthProvider{}
	RegisterProvider(provider)
	creds, key, err := GetAPIKey(provider.ID(), &Credentials{Refresh: "refresh", Access: "old-token", Expires: time.Now().Add(-time.Minute).UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	if key != "new-token" || creds.Access != "new-token" || provider.refreshes != 1 {
		t.Fatalf("expected refreshed key, got key=%q creds=%#v refreshes=%d", key, creds, provider.refreshes)
	}
}

func TestGetAPIKeyKeepsValidCredential(t *testing.T) {
	provider := &testOAuthProvider{}
	RegisterProvider(provider)
	_, key, err := GetAPIKey(provider.ID(), &Credentials{Refresh: "refresh", Access: "valid-token", Expires: time.Now().Add(time.Hour).UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	if key != "valid-token" || provider.refreshes != 0 {
		t.Fatalf("expected existing key without refresh, got key=%q refreshes=%d", key, provider.refreshes)
	}
}

func TestRuntimeForProviderReturnsAPIKeyAndModels(t *testing.T) {
	goai.ClearModels()
	defer goai.RegisterBuiltinModels()

	provider := &testOAuthProvider{}
	RegisterProvider(provider)
	runtime, err := RuntimeForProvider(provider.ID(), &Credentials{Access: "valid-token", Expires: time.Now().Add(time.Hour).UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.APIKey != "valid-token" {
		t.Fatalf("APIKey = %q, want valid-token", runtime.APIKey)
	}
	if runtime.Credentials == nil || runtime.Credentials.Access != "valid-token" {
		t.Fatalf("unexpected runtime credentials: %#v", runtime.Credentials)
	}
	if len(runtime.Models) == 0 {
		t.Fatal("expected built-in models")
	}
}

func TestRuntimeForProviderRejectsUnknownProvider(t *testing.T) {
	if _, err := RuntimeForProvider("missing-provider", &Credentials{}); err == nil {
		t.Fatal("expected unknown provider error")
	}
}

func TestRuntimeCredentialsUIHelpers(t *testing.T) {
	runtime := &RuntimeCredentials{
		APIKey: "token",
		Models: []*goai.Model{{ID: "gpt-5.4", Name: "GPT", Provider: goai.ProviderGitHubCopilot}},
	}
	if opts := runtime.StreamOptions(); opts == nil || opts.APIKey != "token" {
		t.Fatalf("unexpected stream options: %#v", opts)
	}
	items := runtime.ModelPickerItems(goai.ProviderGitHubCopilot)
	if len(items) != 1 || items[0] != "github-copilot/gpt-5.4" {
		t.Fatalf("unexpected picker items: %#v", items)
	}
	model, err := runtime.SelectModel("github-copilot/gpt-5.4")
	if err != nil {
		t.Fatal(err)
	}
	if model == nil || model.ID != "gpt-5.4" {
		t.Fatalf("unexpected selected model: %#v", model)
	}
	if _, err := runtime.SelectModel("github-copilot/missing"); err == nil {
		t.Fatal("expected unavailable model error")
	}
	ctx := runtime.SwitchContextForModel(&goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, model)
	if ctx == nil || len(ctx.Messages) != 1 {
		t.Fatalf("unexpected switched context: %#v", ctx)
	}
}

func TestNilRuntimeCredentialsUIHelpersAreSafe(t *testing.T) {
	var runtime *RuntimeCredentials
	if opts := runtime.StreamOptions(); opts == nil || opts.APIKey != "" {
		t.Fatalf("unexpected nil runtime stream options: %#v", opts)
	}
	if items := runtime.ModelPickerItems(goai.ProviderGitHubCopilot); items != nil {
		t.Fatalf("unexpected nil runtime picker items: %#v", items)
	}
	if model := runtime.FindModel(goai.ModelRef{Provider: goai.ProviderGitHubCopilot, ID: "gpt-5.4"}); model != nil {
		t.Fatalf("unexpected nil runtime model: %#v", model)
	}
}

func TestOAuthRegistryRoundTrip(t *testing.T) {
	// GitHub Copilot should be auto-registered via init()
	p := GetProvider("github-copilot")
	if p == nil {
		t.Fatal("github-copilot provider not registered")
	}
	if p.ID() != "github-copilot" {
		t.Fatalf("expected ID 'github-copilot', got %q", p.ID())
	}
	if p.Name() != "GitHub Copilot" {
		t.Fatalf("expected name 'GitHub Copilot', got %q", p.Name())
	}

	// List should include it
	all := ListProviders()
	found := false
	for _, pp := range all {
		if pp.ID() == "github-copilot" {
			found = true
		}
	}
	if !found {
		t.Fatal("github-copilot not in ListProviders()")
	}
}
