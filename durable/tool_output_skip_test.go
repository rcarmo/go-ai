package durable

import (
	"context"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestToolOutput104SkippedTailCountersAndWindow(t *testing.T) {
	for _, retention := range []string{"tail", "head"} {
		t.Run(retention, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				registry := NewRegistry()
				reg := wrapRegistration("skip")
				reg.OutputLimits = &ToolOutputLimits{MaxBytes: 100, MaxLines: 10, Retain: retention}
				reg.Execute = func(_ context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
					window := api.OutputWindow()
					if retention == "head" {
						if window != nil {
							t.Error("head advertised skipped tail window")
						}
						if err := api.OutputSkipped("tail", &ShellOutputSkip{Bytes: 3}); err == nil {
							t.Error("head accepted skip")
						}
						return ToolResult{Content: "head"}, nil
					}
					if window == nil || window.MaxBytes != 100 || window.MinIntervalMs != 0 || window.BytesPerSecond != 100*1024 {
						t.Error("window policy", window)
					}
					if err := api.Output("earlier\n"); err != nil {
						return ToolResult{}, err
					}
					if err := api.OutputSkipped("tail", &ShellOutputSkip{Bytes: 20, Newlines: 2, EndsWithNewline: true}); err != nil {
						return ToolResult{}, err
					}
					return ToolResult{}, nil
				}
				if err := registry.Register(reg); err != nil {
					t.Fatal(err)
				}
				calls := 0
				ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
					calls++
					ch := make(chan goai.Event, 1)
					if calls == 1 {
						ch <- toolAnswer("skip-call", "skip", JSON{})
					} else {
						ch <- terminal("answer")
					}
					close(ch)
					return ch
				})
				zero := int64(0)
				options.Registry = registry
				options.Settings = &HarnessSettings{Progress: &ProgressSettings{OutputIntervalMs: &zero}}
				h := openHarness(t, b.store, options)
				sub, err := root(t, h, ref).Submit(bg, Input{Content: "go"})
				if err != nil {
					t.Fatal(err)
				}
				waitSubmission(t, sub)
				if retention == "tail" {
					state, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					for _, task := range state.Tasks {
						if task.Kind == "pi.tool" {
							var cp toolCheckpoint
							if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
								t.Fatal(err)
							}
							if cp.Output != "tail" || cp.OutputBytes != 32 || cp.DroppedBytes != 28 || cp.DroppedLines != 3 {
								t.Fatal("skipped tail counts", cp.Output, cp.OutputBytes, cp.DroppedBytes, cp.DroppedLines)
							}
						}
					}
				}
			})
		})
	}
}
