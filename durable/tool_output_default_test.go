package durable

import (
	"context"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestToolDefaultOutputLimitsTruncateAndReturnedContentReplacesProgress(t *testing.T) {
	for _, mode := range []string{"bytes", "lines", "returned"} {
		t.Run(mode, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				registry := NewRegistry()
				reg := wrapRegistration("default-output")
				reg.Execute = func(_ context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
					if mode == "returned" {
						if err := api.Output("transient progress"); err != nil {
							return ToolResult{}, err
						}
						return ToolResult{Content: "returned result"}, nil
					}
					text := strings.Repeat("x", 60*1024)
					if mode == "lines" {
						text = strings.Repeat("line\n", 2100)
					}
					if err := api.Output(text); err != nil {
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
						ch <- toolAnswer("default-call", "default-output", JSON{})
					} else {
						ch <- terminal("done")
					}
					close(ch)
					return ch
				})
				options.Registry = registry
				h := openHarness(t, b.store, options)
				sub, err := root(t, h, ref).Submit(bg, Input{Content: "defaults"})
				if err != nil {
					t.Fatal(err)
				}
				if result := waitSubmission(t, sub); result.Submission.Status != "done" {
					t.Fatal(result)
				}
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				for _, task := range state.Tasks {
					if task.Kind != "pi.tool" {
						continue
					}
					var cp toolCheckpoint
					if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
						t.Fatal(err)
					}
					if cp.Result == nil || cp.Result.IsError || cp.ErrorCode != "" {
						t.Fatal("default truncation rejected tool", cp)
					}
					text := cp.Result.Content[0].Text
					switch mode {
					case "bytes":
						if len(text) != 50*1024 || cp.DroppedBytes != 10*1024 {
							t.Fatal("default byte retention", len(text), cp.DroppedBytes)
						}
					case "lines":
						if outputLines(text) != 2000 || cp.DroppedLines != 100 {
							t.Fatal("default line retention", outputLines(text), cp.DroppedLines)
						}
					case "returned":
						if text != "returned result" || len(cp.Diagnostics) != 0 {
							t.Fatal("returned content did not replace progress", cp)
						}
					}
					if mode != "returned" && (len(cp.Diagnostics) == 0 || cp.Diagnostics[len(cp.Diagnostics)-1].Code != "truncated") {
						t.Fatal("truncation remark missing", cp.Diagnostics)
					}
				}
			})
		})
	}
}
