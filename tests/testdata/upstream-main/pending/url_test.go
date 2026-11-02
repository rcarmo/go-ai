package pending_test

import (
	"encoding/json"
	"os"
	"testing"
)

func TestURLFormatOracle(t *testing.T) {
	type row struct {
		Name          string
		Schema, Input json.RawMessage
		Want          string
	}
	var f struct {
		Upstream         string
		Literal, Derived []row
	}
	data, err := os.ReadFile("../validation-url-pending.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if f.Upstream != "c9f4c7b9fcd0f3e6b5bbc35c7c9b6cda35580eb8" || len(f.Literal) != 1 || len(f.Derived) != 103 {
		t.Fatal("unexpected pin/count")
	}
	for _, group := range []struct {
		name string
		rows []row
	}{{"literal", f.Literal}, {"source-derived", f.Derived}} {
		t.Run(group.name, func(t *testing.T) {
			for _, tc := range group.rows {
				t.Run(tc.Name, func(t *testing.T) { assertPendingOutcome(t, regexOutcome(t, tc.Schema, tc.Input), tc.Want) })
			}
		})
	}
}
