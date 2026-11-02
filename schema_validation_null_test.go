package goai

import (
	"encoding/json"
	"os"
	"testing"
)

func TestValidationNullMembership(t *testing.T) {
	var f struct {
		Upstream string
		Derived  []struct {
			Name   string
			Member json.RawMessage
			Valid  bool
		}
	}
	data, err := os.ReadFile("tests/testdata/upstream-main/validation-null-membership.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if f.Upstream != "c9f4c7b9fcd0f3e6b5bbc35c7c9b6cda35580eb8" || len(f.Derived) != 61 {
		t.Fatal("unexpected pin/count")
	}
	count := 0
	for _, tc := range f.Derived {
		if len(tc.Member) == 0 {
			continue
		}
		count++
		t.Run(tc.Name, func(t *testing.T) {
			var raw interface{}
			if err = json.Unmarshal(tc.Member, &raw); err != nil {
				t.Fatal(err)
			}
			schema, _ := raw.(map[string]interface{})
			v := newToolSchemaValidator(schema)
			if got := v.acceptsRaw(nil, raw); got != tc.Valid {
				t.Fatalf("membership=%v want=%v", got, tc.Valid)
			}
		})
	}
	if count != 55 {
		t.Fatalf("membership rows=%d", count)
	}
}
