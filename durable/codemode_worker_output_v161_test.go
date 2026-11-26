package durable_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/rcarmo/go-ai/durable"
	"github.com/rcarmo/go-ai/durable/tools"
)

// Source: pi v1.6.1 packages/durable/test/codemode.test.ts:970–975.
func TestSourceV161CodemodePublishedWorkerOutputLayout(t *testing.T) {
	const code = `text("one"); console.log("log", 1); text({ two: 2 }); return "three";`
	text, _ := runV161PublishedOutput(t, code)
	header, body, ok := strings.Cut(text, "\nOutput:\n")
	if !ok || !regexp.MustCompile(`^Script completed\nWall time [\d.]+ seconds$`).MatchString(header) {
		t.Fatalf("unexpected script header: %q", text)
	}
	const items = "==> text 1/3 <==\none\n==> text 2/3 <==\n{\"two\":2}\n==> text 3/3 <==\nthree"
	const console = "<console_output>\nlog 1\n</console_output>"
	if body != items+"\n"+console {
		t.Fatalf("script body = %q; want ordered items %q followed by %q", body, items, console)
	}
}

// Source: pi v1.6.1 packages/durable/test/codemode.test.ts:990–1006.
func TestSourceV161CodemodePublishedWorkerAbsoluteOutputLimits(t *testing.T) {
	t.Run("bytes", func(t *testing.T) {
		const code = `// @options: {"max_output_tokens": 2000000}
return "x".repeat(6 * 1024 * 1024);`
		text, details := runV161PublishedOutput(t, code)
		if !strings.Contains(text, "Warning: truncated output") {
			t.Error("large output lacks the truncation warning")
		}
		if len(text) > 1024*1024+1000 {
			t.Errorf("visible output = %d bytes; want at most 1 MiB plus 1000 bytes", len(text))
		}
		_, saved := readV160PublishedSpill(t, details)
		if len(saved) != 6*1024*1024 || strings.Trim(saved, "x") != "" {
			t.Errorf("saved output = %d bytes; want all 6 MiB of x", len(saved))
		}
	})
	t.Run("lines", func(t *testing.T) {
		const code = `// @options: {"max_output_tokens": 2000000}
return Array.from({ length: 200000 }, () => "a").join("\n");`
		text, details := runV161PublishedOutput(t, code)
		if !strings.Contains(text, "Warning: truncated output") {
			t.Error("long output lacks the truncation warning")
		}
		if n := strings.Count(text, "\n") + 1; n >= 100000 {
			t.Errorf("visible output = %d lines; want fewer than 100,000", n)
		}
		_, saved := readV160PublishedSpill(t, details)
		if saved != strings.Repeat("a\n", 199999)+"a" {
			t.Errorf("saved output = %d bytes; want all 200,000 original lines", len(saved))
		}
	})
}

func runV161PublishedOutput(t *testing.T, code string) (string, durable.JSON) {
	t.Helper()
	cwd := t.TempDir()
	spillRoot := t.TempDir()
	t.Setenv("TMPDIR", spillRoot)
	t.Setenv("TMP", spillRoot)
	text, result := durable.CheckV161PublishedWorkerOutput(t, code, tools.LocalEnv(cwd))
	return text, result.Details
}
