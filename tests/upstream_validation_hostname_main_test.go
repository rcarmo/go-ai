package goai_test

import (
	"encoding/json"
	"os"
	"testing"
)

func TestUpstreamMainValidationHostname(t *testing.T) {
	mainRunFormatFixture(t, "validation-hostname.json", 1, 23)
}

// Original c9f4c7b9 dependency fixture, unchanged assertions. The native Tool
// API takes an argument object, so wrap each scalar/data case in required h.
// Do not add type:string: the original format-only schema ignores other types.
func TestUpstreamMainValidationHostnameDependency(t *testing.T) {
	var groups []struct {
		Description string
		Schema      json.RawMessage
		Tests       []struct {
			Description string
			Data        json.RawMessage
			Valid       bool
		}
	}
	data, err := os.ReadFile("testdata/upstream-main/hostname.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &groups); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, group := range groups {
		count += len(group.Tests)
		t.Run(group.Description, func(t *testing.T) {
			schema, err := json.Marshal(map[string]interface{}{
				"type": "object", "properties": map[string]interface{}{"h": group.Schema}, "required": []string{"h"},
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range group.Tests {
				t.Run(tc.Description, func(t *testing.T) {
					t.Parallel()
					input := `{"h":` + string(tc.Data) + `}`
					got, args, err := mainValidateJSON(t, string(schema), input)
					mainAssertValidationJSON(t, args, input)
					if (err == nil) != tc.Valid {
						t.Fatalf("valid=%v, want %v; error=%v", err == nil, tc.Valid, err)
					}
					if err == nil {
						mainAssertValidationJSON(t, got, input)
					}
				})
			}
		})
	}
	if len(groups) != 2 || count != 66 {
		t.Fatal("unexpected pinned dependency fixture shape")
	}
}
