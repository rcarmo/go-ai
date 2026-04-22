package jsonparse_test

import (
	"testing"

	"github.com/rcarmo/go-ai/internal/jsonparse"
)

func FuzzPartialJSON(f *testing.F) {
	// Seed corpus: valid JSON, partial JSON, and edge cases
	f.Add(`{"name":"test","value":42}`)
	f.Add(`{"name":"test","value":4`)
	f.Add(`{"a":1,"b":{"c":2`)
	f.Add(`{"arr":[1,2,3`)
	f.Add(`{"arr":[1,2,3]}`)
	f.Add(`{"str":"hello \"world\""}`)
	f.Add(`{"nested":{"deep":{"deeper":`)
	f.Add(``)
	f.Add(`{`)
	f.Add(`{"key":`)
	f.Add(`{"key":"va`)
	f.Add(`[1,2,3`)
	f.Add(`null`)
	f.Add(`{"emoji":"🎉"}`)
	f.Add(`{"unicode":"日本語"}`)
	f.Add(`{"escape":"line1\nline2\ttab"}`)

	f.Fuzz(func(t *testing.T, input string) {
		// Must not panic
		result, ok := jsonparse.ParsePartialJSON(input)

		// If parse succeeded, result must be usable
		if ok && result != nil {
			// Result should be a valid map (not nil)
			_ = len(result)
		}
	})
}
