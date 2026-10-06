package tools

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestTruncateHead104PinnedPrefixTotals(t *testing.T) {
	data, err := os.ReadFile("testdata/truncate-head-104.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Revision string
		Cases    []struct {
			Prefix  string
			Totals  TruncationTotals
			Options TruncationOptions
			Result  TruncationResult
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Revision != "7c10bd4337495ee613f2224843ecdf349b80d1df" || len(fixture.Cases) != 80 {
		t.Fatal("fixture provenance", fixture.Revision, len(fixture.Cases))
	}
	for index, tc := range fixture.Cases {
		got := TruncateHeadOf(tc.Prefix, tc.Totals, tc.Options)
		if !reflect.DeepEqual(got, tc.Result) {
			t.Fatalf("pinned case %d prefix=%q totals=%v options=%v\ngot=%+v\nwant=%+v", index, tc.Prefix, tc.Totals, tc.Options, got, tc.Result)
		}
	}
}
