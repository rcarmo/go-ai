package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
)

func TestToolTruncationRemarksPinnedFallbackExplicitAndHookReplacement(t *testing.T) {
	for _, mode := range []string{"retained", "explicit", "hook-replacement", "hook-preserve"} {
		t.Run(mode, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				limits := ToolOutputLimits{4, 10, "tail"}
				registry := NewRegistry()
				extension := &Extension{Name: "truncate", Tools: []ToolRegistration{{Definition: goai.Tool{Name: "truncate", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "truncation.test", Version: 1, OutputLimits: &limits, Execute: func(_ context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
					if err := api.Output("abcdef"); err != nil {
						return ToolResult{}, err
					}
					if err := api.Diagnostic(ToolDiagnostic{Severity: "info", Message: "before"}); err != nil {
						return ToolResult{}, err
					}
					result := ToolResult{Diagnostics: []ToolDiagnostic{{Severity: "info", Message: "after"}}, IsError: true}
					if mode == "explicit" {
						result.Content = "123456"
					}
					return result, nil
				}}}}
				if strings.HasPrefix(mode, "hook-") {
					extension.ToolHooks.AfterTool = func(_ context.Context, _ goai.ToolCall, result ToolResult, _ *HookAPI) (*ToolResult, error) {
						if result.Content != "cdef" || !result.IsError || len(result.Diagnostics) != 2 || result.Diagnostics[0].Message != "before" || result.Diagnostics[1].Message != "after" {
							t.Error("hook lost resolved content or flags", result)
						}
						if mode == "hook-replacement" {
							result.Content = "ok"
						}
						return &result, nil
					}
				}
				if err := registry.Install(extension); err != nil {
					t.Fatal(err)
				}
				calls := 0
				ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					calls++
					ch := make(chan goai.Event, 1)
					if calls == 1 {
						ch <- toolAnswer("truncate-call", "truncate", JSON{})
					} else {
						for _, m := range input.Messages {
							if m.Role != goai.RoleToolResult {
								continue
							}
							want := "cdef"
							if mode == "explicit" {
								want = "3456"
							}
							if mode == "hook-replacement" {
								want = "ok"
							}
							remarks := "<harness>\n[info] before\n[info] after"
							if mode != "hook-replacement" {
								remarks += "\n[warn] Output truncated to its end: 0 lines, 2 bytes dropped"
							}
							remarks += "\n</harness>"
							if !m.IsError || len(m.Content) != 2 || m.Content[0].Text != want || m.Content[1].Text != remarks {
								t.Error("reference truncation receipt", mode, m)
							}
						}
						ch <- terminal("done")
					}
					close(ch)
					return ch
				})
				options.Registry = registry
				h := openHarness(t, b.store, options)
				c := root(t, h, ref)
				sub, err := c.Submit(bg, Input{Content: "go"})
				if err != nil {
					t.Fatal(err)
				}
				waitSubmission(t, sub)
				snapshot, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				for _, task := range snapshot.Tasks {
					if task.Kind == "pi.tool" {
						var cp toolCheckpoint
						if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
							t.Fatal(err)
						}
						want := 3
						if mode == "hook-replacement" {
							want = 2
						}
						if len(cp.Diagnostics) != want {
							t.Fatal("remarks checkpoint", cp.Diagnostics)
						}
					}
				}
			})
		})
	}
}
