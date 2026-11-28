package goai

import (
	"fmt"
	"reflect"
	"runtime"
	"testing"
)

// TestIdentityPublicHelpersUpstream maps the deterministic assertions in pi v1.7.0's
// unchanged identity.test.ts through the public native identity helpers.
func TestIdentityPublicHelpersUpstream(t *testing.T) {
	if PiAIVersion != "1.7.0" {
		t.Fatalf("PiAIVersion = %q, want 1.7.0", PiAIVersion)
	}
	if got, want := FormatUserAgent(nil), fmt.Sprintf("pi/1.7.0 (%s; go/%s; %s)", runtime.GOOS, runtime.Version()[2:], runtime.GOARCH); got != want {
		t.Fatalf("FormatUserAgent(nil) = %q, want %q", got, want)
	}
	short := UserAgentMode{Mode: "short"}
	custom := "acme/1.0"
	version := "9.9.9"
	if got := FormatUserAgent(&ClientIdentity{UserAgent: &short, Version: &version}); got != "pi" {
		t.Fatalf("short agent = %q", got)
	}
	if got := FormatUserAgent(&ClientIdentity{UserAgent: &UserAgentMode{Custom: &custom}}); got != custom {
		t.Fatalf("custom agent = %q", got)
	}

	pi := PiAttribution()
	merged := MergeClientIdentity(&ClientIdentity{Attribution: &pi, UserAgent: &short}, &ClientIdentity{Version: &version})
	if merged == nil || merged.Attribution == nil || merged.Attribution.Name != "pi" || merged.UserAgent.Mode != "short" || *merged.Version != version {
		t.Fatalf("merged identity = %#v", merged)
	}
	clear := &ClientIdentity{AttributionSet: true}
	if got := MergeClientIdentity(&ClientIdentity{Attribution: &pi}, clear); got == nil || got.Attribution != nil || !got.AttributionSet {
		t.Fatalf("explicit attribution clear = %#v", got)
	}

	openRouter := ClientIdentityTarget{Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1"}
	if got := GetAttributionHeaders(openRouter, nil); len(got) != 0 {
		t.Fatalf("nil attribution leaked: %#v", got)
	}
	if got := GetIdentityHeaders(openRouter, nil); len(got) != 1 || got["User-Agent"] == "" {
		t.Fatalf("default headers = %#v", got)
	}
	want := map[string]string{"HTTP-Referer": "https://pi.dev", "X-OpenRouter-Title": "pi", "X-OpenRouter-Categories": "cli-agent"}
	if got := GetAttributionHeaders(openRouter, &ClientIdentity{Attribution: &pi}); !reflect.DeepEqual(got, want) {
		t.Fatalf("OpenRouter attribution = %#v, want %#v", got, want)
	}
	acme := ClientAttribution{Name: "acme", URL: "https://acme.example", Categories: []string{"cli-agent", "cloud-agent"}}
	if got := GetAttributionHeaders(ClientIdentityTarget{Provider: "nvidia"}, &ClientIdentity{Attribution: &acme}); got["X-BILLING-INVOKE-ORIGIN"] != "Acme" {
		t.Fatalf("NVIDIA attribution = %#v", got)
	}
	if got := GetAttributionHeaders(ClientIdentityTarget{Provider: "custom", BaseURL: "https://opencode.ai/zen/v1"}, &ClientIdentity{Attribution: &acme}); got["x-opencode-client"] != "acme" {
		t.Fatalf("OpenCode attribution = %#v", got)
	}
	if got := GetOpenCodeSessionHeaders(ClientIdentityTarget{BaseURL: "https://opencode.ai.evil.invalid"}, "session"); got != nil {
		t.Fatalf("OpenCode lookalike received session: %#v", got)
	}
}
