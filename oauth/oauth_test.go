package oauth

import (
	"testing"
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
