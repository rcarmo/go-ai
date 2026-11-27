package goai

import (
	"encoding/json"
	"testing"
)

func TestV161GoogleCompatCloneDetachesOptionalOverride(t *testing.T) {
	for _, tc := range []struct {
		input    string
		presence bool
		value    bool
	}{
		{`{"googleCompat":{}}`, false, false},
		{`{"googleCompat":{"supportsStrictMode":false}}`, true, false},
		{`{"googleCompat":{"supportsStrictMode":true}}`, true, true},
	} {
		var source Model
		if err := json.Unmarshal([]byte(tc.input), &source); err != nil {
			t.Fatal(err)
		}
		detached := cloneModel(&source)
		if detached.GoogleCompat == nil || detached.GoogleCompat == source.GoogleCompat ||
			(detached.GoogleCompat.SupportsStrictMode != nil) != tc.presence {
			t.Fatalf("override not copied with matching presence: %s", tc.input)
		}
		if tc.presence {
			if got := *detached.GoogleCompat.SupportsStrictMode; got != tc.value {
				t.Errorf("copied override = %v; want %v", got, tc.value)
			}
			*detached.GoogleCompat.SupportsStrictMode = !tc.value
			if *source.GoogleCompat.SupportsStrictMode != tc.value {
				t.Error("mutating the detached override mutated the original")
			}
		}
	}
}
