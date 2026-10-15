package quickjs

import (
	"strings"
	"testing"
)

func TestQuickJS120MalformedScriptErrorBridgeCases(t *testing.T) {
	for _, payload := range []string{`5`, `null`, `[]`, `{}`, `{"name":"Error"}`, `{"name":null,"message":"x"}`, `{"name":"Error","message":42}`, `{"name":"Error","message":null}`, `{"name":"Error","message":"x","stack":[]}`, `{"name":"Error","message":"x","stack":null}`, `{`} {
		error, err := parseScriptError(payload)
		if err == nil || error != nil {
			t.Fatal("malformed bridge accepted", payload, error)
		}
	}
	error, err := parseScriptError(`{"name":"TypeError","message":"","stack":"TypeError\n at private (codemode-prelude.js:1)\n at script (codemode.js:3)"}`)
	if err != nil || error.Kind != ErrorScript || error.Name != "TypeError" || error.Message != "" || strings.Contains(error.Stack, "codemode-prelude") || !strings.Contains(error.Stack, "codemode.js:3") {
		t.Fatal(error, err)
	}
}
