package openaicodex

import "testing"

func TestCodex110CallerOverridesOriginatorAndUserAgent(t *testing.T) {
	for _, transport := range []string{"sse", "websocket"} {
		model := map[string]string{"originator": "host-model", "User-Agent": "model-agent"}
		caller := map[string]string{"originator": "host-caller", "user-agent": "caller-agent"}
		h := buildCodexSSEHeaders(model, caller, nil, "account", "token", "session")
		if transport == "websocket" {
			h = buildCodexWebSocketHeaders(model, caller, nil, "account", "token", "session")
		}
		if h.Get("originator") != "host-caller" || h.Get("User-Agent") != "caller-agent" || h.Get("Authorization") != "Bearer token" {
			t.Fatal(transport, h)
		}
		h = buildCodexSSEHeaders(model, nil, []string{"originator", "User-Agent"}, "account", "token", "")
		if h.Get("originator") != "" || h.Get("User-Agent") != "" {
			t.Fatal("explicit suppression lost", h)
		}
	}
}
