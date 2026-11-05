package goai_test

import (
	"testing"
)

// Exact upstream mcp-invalid-regex outcome at v1.5.0.
func TestMainValidationInvalidPatternCompilation(t *testing.T) {
	schema := `{"type":"object","properties":{"s":{"type":"string","pattern":"["}}}`
	got := mainNumericOutcome(t, schema, `{"s":"y"}`)
	want := "THROW Invalid regular expression: /[/: Unterminated character class"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
