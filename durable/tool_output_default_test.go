package durable

import (
	"context"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestToolDefaultOutputLimitsTruncateAndReturnedContentReplacesProgress(t *testing.T) {
	for _, mode := range []string{"bytes", "lines", "returned", "hook-kept", "hook-replaced", "hook-empty", "hook-copy"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
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
				if err := registry.Install(&Extension{Name: "output-hooks", ToolHooks: ToolHooks{AfterTool: func(_ context.Context, _ goai.ToolCall, result ToolResult, _ *HookAPI) (*ToolResult, error) {
					switch mode {
					case "hook-kept":
						return &result, nil
					case "hook-replaced":
						return &ToolResult{Blocks: []goai.ContentBlock{{Type: "text", Text: "replacement"}}}, nil
					case "hook-empty":
						return &ToolResult{Blocks: []goai.ContentBlock{}}, nil
					case "hook-copy":
						// Same text, a distinct explicit output array: source
						// final.output !== retained output, so no retained warning.
						return &ToolResult{Blocks: []goai.ContentBlock{{Type: "text", Text: result.Content}}}, nil
					}
					return nil, nil
				}}}); err != nil {
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
					receipt := canonicalModelToolReceipt(t, h, task.ID)
					if receipt.IsError || (len(receipt.Content) == 0 && mode != "hook-empty") {
						t.Fatal("default truncation rejected tool", receipt)
					}
					text := ""
					if len(receipt.Content) > 0 {
						text = receipt.Content[0].Text
					}
					want := ""
					switch mode {
					case "bytes", "hook-kept":
						if len(text) != 50*1024 {
							t.Fatal("default byte retention", len(text))
						}
						want = "Output truncated to its beginning: 0 lines, 10240 bytes dropped"
					case "lines":
						if outputLines(text) != 2000 {
							t.Fatal("default line retention", outputLines(text))
						}
						want = "Output truncated to its beginning: 100 lines, 500 bytes dropped"
					case "hook-replaced":
						if text != "replacement" || len(receipt.Diagnostics) != 0 {
							t.Fatal("hook output did not replace retained truncation", receipt)
						}
					case "hook-empty":
						if len(receipt.Content) != 0 || len(receipt.Diagnostics) != 0 {
							t.Fatal("explicit empty hook output retained progress", receipt)
						}
					case "hook-copy":
						if len(text) != 50*1024 || len(receipt.Diagnostics) != 0 {
							t.Fatal("equal text in distinct output retained progress warning", receipt)
						}
					case "returned":
						if text != "returned result" || len(receipt.Diagnostics) != 0 {
							t.Fatal("returned content did not replace progress", receipt)
						}
					}
					if want != "" && (len(receipt.Diagnostics) != 1 || receipt.Diagnostics[0].Code != "truncated" || receipt.Diagnostics[0].Message != want) {
						t.Fatal("truncation remark/counts missing", receipt.Diagnostics, want)
					}
				}
			})
		})
	}
}
