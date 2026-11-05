package goai_test

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Captured from the unchanged official v1.5.0 validation-probes.test.ts at
// f9a26365568127461c47a07a37dcb42c140a3ac5, executed with its pinned schema
// implementation. Successful objects compare structurally, not by key order.
// Non-JSON JS values, one unpaired-surrogate call and the second same-object
// cache-mutation call are excluded: JSON calls cannot preserve those contracts.
func TestV150ValidationProbeFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/validation_probes_v150.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Name          string
		Schema, Input json.RawMessage
		Want          string
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 166 {
		t.Fatalf("fixture count=%d", len(rows))
	}
	for i, row := range rows {
		t.Run(fmt.Sprintf("%03d-%s", i+1, row.Name), func(t *testing.T) {
			got := mainNumericOutcome(t, string(row.Schema), string(row.Input))
			if strings.HasPrefix(got, "ok ") && strings.HasPrefix(row.Want, "ok ") {
				var a, b interface{}
				if err := json.Unmarshal([]byte(got[3:]), &a); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(row.Want[3:]), &b); err != nil {
					t.Fatal(err)
				}
				if reflect.DeepEqual(a, b) {
					return
				}
			}
			if got != row.Want {
				t.Errorf("schema=%s input=%s\ngot  %s\nwant %s", row.Schema, row.Input, got, row.Want)
			}
		})
	}
}
