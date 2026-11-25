package quickjs

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Source: pi v1.6.0 packages/codemode/test/sandbox.test.ts:641–668.
func TestSourceV160QuickJSOutputLimits(t *testing.T) {
	engine, err := NewEngine(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	opts := Options{Timeout: 15 * time.Second, MemoryLimitBytes: 32 << 20}
	const maxChars = 16 << 20
	for _, tc := range []struct{ name, print, typ string }{
		{"text", `text(s)`, "text"},
		{"console", `console.log(s)`, "console"},
		{"image", `image("data:image/png;base64," + p)`, "image"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code := `const s = "x".repeat(1 << 20);
				const p = "iVBORw0KGgoA" + "A".repeat(1 << 20);
				for (;;) { try { ` + tc.print + `; } catch {} }`
			got := engine.Execute(t.Context(), code, opts)
			if got.OK || got.Error == nil || got.Error.Kind != ErrorScript || got.Error.Name != "RangeError" || !strings.Contains(got.Error.Message, "script output exceeded") {
				t.Errorf("ok=%v error=%+v blocks=%d; want a script RangeError about the output limit", got.OK, got.Error, len(got.Output))
			}
			chars := 0
			for i, item := range got.Output {
				if item.Type != tc.typ {
					t.Errorf("block %d type=%q; want %q", i, item.Type, tc.typ)
				}
				chars += len(item.Text) + len(item.Data) // All fixture output is ASCII.
			}
			if chars > maxChars {
				t.Errorf("output=%d chars; exceeds %d", chars, maxChars)
			}
			if chars <= maxChars-(2<<20) {
				t.Errorf("output=%d chars; want more than %d", chars, maxChars-(2<<20))
			}
		})
	}
	t.Run("empty", func(t *testing.T) {
		got := engine.Execute(t.Context(), `for (;;) text("");`, opts)
		if got.OK || got.Error == nil || got.Error.Name != "RangeError" {
			t.Errorf("ok=%v error=%+v; want RangeError", got.OK, got.Error)
		}
		if len(got.Output) != 100_000 {
			t.Errorf("output has %d blocks; want 100,000", len(got.Output))
		}
	})
}
