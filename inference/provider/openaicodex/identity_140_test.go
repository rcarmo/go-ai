package openaicodex

import (
	goai "github.com/rcarmo/go-ai"
	"net/http"
	"testing"
)

func TestCodex140IdentityHeaderPrecedence(t *testing.T) {
	custom := "custom-agent"
	identity := &goai.ClientIdentity{UserAgent: &goai.UserAgentMode{Custom: &custom}}
	for _, build := range []func(map[string]string, map[string]string, []string, string, string, string, ...*goai.ClientIdentity) http.Header{buildCodexSSEHeaders, buildCodexWebSocketHeaders} {
		h := build(nil, nil, nil, "account", "token", "session", identity)
		if h.Get("User-Agent") != custom {
			t.Fatal(h)
		}
		h = build(map[string]string{"User-Agent": "model"}, map[string]string{"user-agent": "request"}, nil, "account", "token", "session", identity)
		if h.Get("User-Agent") != "request" {
			t.Fatal(h)
		}
		h = build(nil, nil, []string{"User-Agent"}, "account", "token", "session", identity)
		if h.Get("User-Agent") != "" {
			t.Fatal(h)
		}
	}
}
