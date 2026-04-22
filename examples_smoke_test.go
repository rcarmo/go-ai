package goai_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestExamplesBuild(t *testing.T) {
	cmd := exec.Command("go", "build", "./examples/...")
	cmd.Dir = "/workspace/projects/go-ai"
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build ./examples/... failed: %v\n%s", err, string(out))
	}
}

func TestExamplesMissingCredentialMessages(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"./examples/basic", "set OPENAI_API_KEY to run this example"},
		{"./examples/streaming", "set ANTHROPIC_API_KEY or ANTHROPIC_OAUTH_TOKEN to run this example"},
		{"./examples/tools", "set OPENAI_API_KEY to run this example"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			cmd := exec.Command("go", "run", tt.path)
			cmd.Dir = "/workspace/projects/go-ai"
			cmd.Env = minimalEnvWithoutAIKeys()
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("expected failure without credentials, got success: %s", string(out))
			}
			if !strings.Contains(string(out), tt.want) {
				t.Fatalf("expected %q in output, got:\n%s", tt.want, string(out))
			}
		})
	}
}

func minimalEnvWithoutAIKeys() []string {
	var out []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "OPENAI_API_KEY=") ||
			strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") ||
			strings.HasPrefix(kv, "ANTHROPIC_OAUTH_TOKEN=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
