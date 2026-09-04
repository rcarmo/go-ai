package goai_test

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0850UUIDv7AcceptsOptionalTimestamp(t *testing.T) {
	uuid := goai.UUIDv7(0x0199aabbccdd)
	if !strings.HasPrefix(uuid, "0199aabb-ccdd-7") {
		t.Fatalf("uuid %q does not encode requested timestamp", uuid)
	}
	later := goai.UUIDv7(0x0199aabbccde)
	if later <= uuid {
		t.Fatalf("expected later timestamp UUID to sort after: %q <= %q", later, uuid)
	}
}

func TestV0850HTTPProxyNoProxyRootSubdomainAndIPv6(t *testing.T) {
	env := map[string]string{
		"HTTP_PROXY":  "http://proxy.example:8080",
		"HTTPS_PROXY": "http://proxy.example:8080",
		"NO_PROXY":    "example.com,.internal,[2001:db8::1],2001:db8::2",
	}
	for _, target := range []string{"http://example.com", "http://api.example.com", "http://service.internal", "http://[2001:db8::1]", "http://[2001:db8::2]"} {
		if out := runProxyHelper(t, env, target); out != "<nil>" {
			t.Fatalf("%s should bypass proxy, got %s", target, out)
		}
	}
	if out := runProxyHelper(t, env, "http://notexample.com"); out == "<nil>" {
		t.Fatal("unmatched host should use proxy")
	}
}
