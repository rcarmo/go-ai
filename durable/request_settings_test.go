package durable

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestCuratedRequestSettingsDetachAndApplyTransportRetryMetadataHeaders(t *testing.T) {
	timeout, retries, delay := 1234, 0, 900
	settings := RequestSettings{Transport: goai.TransportSSE, TimeoutMs: &timeout, MaxRetries: &retries, MaxRetryDelayMs: &delay, CacheRetention: goai.CacheRetentionLong, Headers: map[string]string{"x-public": "pinned"}, Metadata: map[string]any{"nested": map[string]any{"value": "pinned"}}}
	h, err := Open(bg, mustMemory(t), Options{Settings: &HarnessSettings{Stream: settings}})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close(bg)
	timeout = 999
	settings.Headers["x-public"] = "mutated"
	settings.Metadata["nested"].(map[string]any)["value"] = "mutated"
	resolved := h.resolvedSettings(RequestSettings{})
	for i := 0; i < 2; i++ {
		options := &goai.StreamOptions{Headers: map[string]string{"authorization": "local-secret"}}
		if err := applyRequestSettings(options, resolved, DefaultLimits()); err != nil {
			t.Fatal(err)
		}
		if options.Transport != goai.TransportSSE || options.TimeoutMs == nil || *options.TimeoutMs != 1234 || *options.MaxRetries != 0 || *options.MaxRetryDelayMs != 900 || options.CacheRetention != goai.CacheRetentionLong || options.Headers["x-public"] != "pinned" || options.Headers["authorization"] != "local-secret" || options.Metadata["nested"].(map[string]any)["value"] != "pinned" {
			t.Fatal(options)
		}
		options.Headers["x-public"] = "changed"
		options.Metadata["nested"].(map[string]any)["value"] = "changed"
	}
	bad := -1
	if _, err := cloneSettings(RequestSettings{TimeoutMs: &bad}, DefaultLimits()); err == nil {
		t.Fatal("negative timeout accepted")
	}
	if _, err := cloneSettings(RequestSettings{Transport: "invalid"}, DefaultLimits()); err == nil {
		t.Fatal("invalid transport accepted")
	}
}
