package goai

import (
	"encoding/json"
	"os"
	"testing"
)

// Source-derived canonical URI/fallback controls from the actual pinned
// resolver, in addition to the public format and named-resource assertions.
func TestValidationURLHostURIs(t *testing.T) {
	var f struct {
		Upstream string
		URIs     []struct{ Name, Ref, Base, Want string }
	}
	data, err := os.ReadFile("tests/testdata/upstream-main/validation-url-idna.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if f.Upstream != "c9f4c7b9fcd0f3e6b5bbc35c7c9b6cda35580eb8" || len(f.URIs) != 96 {
		t.Fatal("unexpected pin/count")
	}
	for _, tc := range f.URIs {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			if got := resolveSchemaURI(tc.Ref, tc.Base); got != tc.Want {
				t.Fatalf("got %q want %q", got, tc.Want)
			}
		})
	}
}
