package durable

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

// Source: pi v1.6.0 packages/codemode/test/sandbox.test.ts:230–261.
func TestSourceV160CodemodeScriptStacksAcrossPlacements(t *testing.T) {
	for _, tc := range []struct {
		name, code, errorName, message string
		withConsole                    bool
	}{
		{"syntax", "const a = 1;\nconst b = ;\nreturn a", "SyntaxError", "", false},
		{"thrown", "const a = 1;\nthrow new TypeError('boom ' + a)", "TypeError", "boom 1", false},
		{"v8-stack", "console.log(new Error('inner'));\nthrow new RangeError('outer')", "RangeError", "outer", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertStack := func(t *testing.T, name, message, stack string) {
				t.Helper()
				if name != tc.errorName || (tc.message != "" && message != tc.message) || !strings.Contains(stack, "codemode.js:2") {
					t.Errorf("script error name=%q message=%q stack=%q; want %s and line 2", name, message, stack, tc.errorName)
				}
				if tc.withConsole && (!regexp.MustCompile(`^RangeError: outer\n {4}at .*codemode\.js:2`).MatchString(stack) || strings.Contains(stack, "codemode-prelude.js")) {
					t.Errorf("script stack = %q; want V8-style line 2 without prelude frames", stack)
				}
			}
			assertConsole := func(t *testing.T, text string) {
				t.Helper()
				if !regexp.MustCompile(`^Error: inner\n {4}at .*codemode\.js:1`).MatchString(text) || strings.Contains(text, "codemode-prelude.js") {
					t.Errorf("console stack = %q; want V8-style line 1 without prelude frames", text)
				}
			}
			t.Run("inline", func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				defer cancel()
				engine, err := quickjs.NewEngine(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer engine.Close(context.Background())
				result := engine.Execute(ctx, tc.code, quickjs.Options{})
				if result.OK || result.Error == nil || result.Error.Kind != "script" {
					t.Fatalf("inline result = %+v, want script failure", result)
				}
				assertStack(t, result.Error.Name, result.Error.Message, result.Error.Stack)
				if tc.withConsole {
					if len(result.Output) != 1 || result.Output[0].Type != "console" {
						t.Fatalf("inline console items = %+v, want one", result.Output)
					}
					assertConsole(t, result.Output[0].Text)
				}
			})
			for _, placement := range v160StandaloneWorkerPlacements() {
				t.Run(placement.name, func(t *testing.T) {
					process := officialWorkerTransport120{root: officialWorkerPackageRootV160(t)}
					output, done, result := executeV160StandaloneScript(t, process, placement.launcher, placement.identity, tc.code)
					var encoded string
					var detail struct{ Name, Message, Stack string }
					if done.Type != "done" || done.OK || json.Unmarshal(done.Error, &encoded) != nil || json.Unmarshal([]byte(encoded), &detail) != nil {
						t.Fatalf("worker result = %+v, want script failure", done)
					}
					assertStack(t, detail.Name, detail.Message, detail.Stack)
					if tc.withConsole {
						if len(output) != 1 || output[0].Type != "text" || !output[0].Console {
							t.Fatalf("worker console items = %+v, want one", output)
						}
						assertConsole(t, output[0].Text)
					}
					if result.Error != detail.Message || !strings.Contains(result.Output, "codemode.js:2") || strings.Contains(result.Output, "codemode-prelude.js") {
						t.Errorf("Go error projection = %q, output=%q; want script line 2 without prelude frames", result.Error, result.Output)
					}
				})
			}
		})
	}
}
