package durable

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

// Source: pi v1.6.0 packages/codemode/test/sandbox.test.ts:224–228,269–273.
func TestSourceV160CodemodeFailedScriptValuesAcrossPlacements(t *testing.T) {
	for _, tc := range []struct {
		name, code, errorName string
		wantOutput            []CodemodeWorkerOutput
	}{
		{"partial-output", "text(\"partial\");\nthrow new Error(\"boom\")", "Error", []CodemodeWorkerOutput{{Type: "text", Text: "partial"}}},
		{"non-serializable-return", "return 10n", "TypeError", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("inline", func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				defer cancel()
				engine, err := quickjs.NewEngine(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer engine.Close(context.Background())
				result := engine.Execute(ctx, tc.code, quickjs.Options{})
				if result.OK || result.Error == nil || result.Error.Kind != "script" || result.Error.Name != tc.errorName {
					t.Fatalf("inline outcome = %+v, want script %s", result, tc.errorName)
				}
				var want []quickjs.OutputItem
				for _, item := range tc.wantOutput {
					want = append(want, quickjs.OutputItem{Type: "text", Text: item.Text})
				}
				if !reflect.DeepEqual(result.Output, want) {
					t.Errorf("inline output = %v, want %v", result.Output, want)
				}
			})
			for _, placement := range v160StandaloneWorkerPlacements() {
				t.Run(placement.name, func(t *testing.T) {
					process := officialWorkerTransport120{root: officialWorkerPackageRootV160(t)}
					output, done, result := executeV160StandaloneScript(t, process, placement.launcher, placement.identity, tc.code)
					var encoded string
					var detail struct{ Name, Message string }
					if done.Type != "done" || done.OK || json.Unmarshal(done.Error, &encoded) != nil || json.Unmarshal([]byte(encoded), &detail) != nil || detail.Name != tc.errorName {
						t.Fatalf("published outcome = %+v, decoded=%+v; want script %s", done, detail, tc.errorName)
					}
					if !reflect.DeepEqual(output, tc.wantOutput) {
						t.Errorf("published output = %v, want %v", output, tc.wantOutput)
					}
					if result.HasValue || result.Error == "" || result.Error != detail.Message {
						t.Errorf("Go projection = %+v, want the script error and no value", result)
					}
					if len(tc.wantOutput) != 0 && !strings.Contains(result.Output, "partial") {
						t.Errorf("Go projection lost partial output: %q", result.Output)
					}
				})
			}
		})
	}
}
