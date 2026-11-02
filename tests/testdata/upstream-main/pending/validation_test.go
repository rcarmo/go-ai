// Explicitly unresolved c9f4c7b9 oracles. Run with:
// go test ./tests/testdata/upstream-main/pending -run '^TestCyclicNamedID$'
// testdata is excluded from ./...; this is not a passing parity claim.
package pending_test

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestCyclicNamedID(t *testing.T) {
	runPendingValidationFixture(t, "../validation-cyclic-pending.json", 2)
}

func runPendingValidationFixture(t *testing.T, path string, count int) {
	t.Helper()
	var fixture struct {
		Upstream string
		Literal  []struct {
			Name          string
			Schema, Input json.RawMessage
			Want          string
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Upstream != "c9f4c7b9fcd0f3e6b5bbc35c7c9b6cda35580eb8" || len(fixture.Literal) != count {
		t.Fatal("unexpected pinned validation fixture")
	}
	for _, tc := range fixture.Literal {
		t.Run(tc.Name, func(t *testing.T) {
			var args map[string]interface{}
			if err := json.Unmarshal(tc.Input, &args); err != nil {
				t.Fatal(err)
			}
			tool := goai.Tool{Name: "cyclic", Parameters: tc.Schema}
			got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: args})
			var original map[string]interface{}
			if err := json.Unmarshal(tc.Input, &original); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(args, original) {
				t.Fatal("caller arguments changed")
			}
			if err != nil {
				t.Fatal(err)
			}
			var want map[string]interface{}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(tc.Want, "ok ")), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %#v, want %#v", got, want)
			}
		})
	}
}
