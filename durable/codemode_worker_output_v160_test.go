package durable_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/rcarmo/go-ai/durable"
	"github.com/rcarmo/go-ai/durable/tools"
)

// Source: pi v1.6.0 codemode.test.ts:970–1006. The model sees the same
// published guest and real file capability that produced the saved output.
func TestSourceV160CodemodePublishedWorkerOutput(t *testing.T) {
	t.Run("text-console-and-return", func(t *testing.T) {
		const code = `text("one"); console.log("log", 1); text({ two: 2 }); return "three";`
		text, _ := runV160PublishedOutput(t, code)
		if !regexp.MustCompile(`^Script completed\nWall time [\d.]+ seconds\nOutput:\n`).MatchString(text) {
			t.Errorf("unexpected script header: %q", text)
		}
		const want = "==> text 1/3 <==\none\n==> text 2/3 <==\n{\"two\":2}\n==> text 3/3 <==\nthree"
		if !strings.Contains(text, want) {
			t.Errorf("script text = %q; want ordered items %q", text, want)
		}
		if !strings.Contains(text, "<console_output>\nlog 1\n</console_output>") {
			t.Errorf("script text = %q; want exact console section", text)
		}
	})
	t.Run("token-budget", func(t *testing.T) {
		const code = `// @options: {"max_output_tokens": 20}
text("BEGIN" + "x".repeat(1000) + "END");`
		text, details := runV160PublishedOutput(t, code)
		for _, part := range []string{"Warning: truncated output", "BEGIN", "END"} {
			if !strings.Contains(text, part) {
				t.Errorf("script text lacks %q: %q", part, text)
			}
		}
		path, saved := readV160PublishedSpill(t, details)
		if strings.Count(text, "[Full output: "+path) != 1 {
			t.Errorf("script text needs one reference to saved file %q: %q", path, text)
		}
		if saved != "BEGIN"+strings.Repeat("x", 1000)+"END" {
			t.Errorf("saved output (%d bytes) differs from the original text", len(saved))
		}
	})
	t.Run("absolute-byte-and-line-limits", func(t *testing.T) {
		const bigCode = `// @options: {"max_output_tokens": 2000000}
return "x".repeat(6 * 1024 * 1024);`
		text, details := runV160PublishedOutput(t, bigCode)
		if !strings.Contains(text, "Warning: truncated output") {
			t.Error("large output lacks the truncation warning")
		}
		if len(text) > 1024*1024+1000 {
			t.Errorf("visible output = %d bytes; want at most 1 MiB plus 1000 bytes", len(text))
		}
		_, saved := readV160PublishedSpill(t, details)
		if len(saved) != 6*1024*1024 || strings.Trim(saved, "x") != "" {
			t.Errorf("saved output = %d bytes; want 6 MiB of x", len(saved))
		}
		const longCode = `// @options: {"max_output_tokens": 2000000}
return Array.from({ length: 200000 }, () => "a").join("\n");`
		text, details = runV160PublishedOutput(t, longCode)
		if !strings.Contains(text, "Warning: truncated output") {
			t.Error("long output lacks the truncation warning")
		}
		if n := strings.Count(text, "\n") + 1; n >= 100000 {
			t.Errorf("visible output = %d lines; want fewer than 100,000", n)
		}
		_, saved = readV160PublishedSpill(t, details)
		if saved != strings.Repeat("a\n", 199999)+"a" {
			t.Errorf("saved output = %d bytes; want all 200,000 original lines", len(saved))
		}
	})
}

func runV160PublishedOutput(t *testing.T, code string) (string, durable.JSON) {
	t.Helper()
	cwd := t.TempDir()
	spillRoot := t.TempDir()
	t.Setenv("TMPDIR", spillRoot)
	t.Setenv("TMP", spillRoot)
	return durable.CheckV160PublishedWorkerOutput(t, code, tools.LocalEnv(cwd))
}

func readV160PublishedSpill(t *testing.T, details durable.JSON) (string, string) {
	t.Helper()
	path, ok := details["fullOutputPath"].(string)
	if !ok || path == "" {
		t.Fatal("result lacks a saved output path")
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved output %q: %v", path, err)
	}
	return path, string(saved)
}
