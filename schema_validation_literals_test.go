package goai

import (
	"encoding/json"
	"os"
	"testing"
)

// Exact pinned draft2020-12 fixture schemas/data/valid flags; no coercion.
func TestValidationLiteralFixtures(t *testing.T) {
	for _, fixture := range []struct {
		name          string
		groups, cases int
	}{{"const", 17, 54}, {"enum", 16, 53}} {
		t.Run(fixture.name, func(t *testing.T) {
			var groups []struct {
				Description string
				Schema      map[string]interface{}
				Tests       []struct {
					Description string
					Data        interface{}
					Valid       bool
				}
			}
			data, err := os.ReadFile("tests/testdata/upstream-main/validation-" + fixture.name + "-fixture.json")
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(data, &groups); err != nil {
				t.Fatal(err)
			}
			if len(groups) != fixture.groups {
				t.Fatal("unexpected group count")
			}
			count := 0
			for _, group := range groups {
				for _, tc := range group.Tests {
					count++
					t.Run(group.Description+"/"+tc.Description, func(t *testing.T) {
						v := newToolSchemaValidator(group.Schema)
						if got := v.schemaAcceptsValue(tc.Data, group.Schema); got != tc.Valid {
							t.Fatalf("membership=%v want=%v", got, tc.Valid)
						}
					})
				}
			}
			if count != fixture.cases {
				t.Fatal("unexpected assertion count")
			}
		})
	}
}
