package durable

import (
	"context"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestToolStrictJSONDetailsScalarArrayNullFallbackHooksAndReopen(t *testing.T) {
	for _, details := range []any{"scalar", []any{1, "array", nil}, false, nil} {
		backends(t, func(t *testing.T, b backend) {
			registry := NewRegistry()
			reg := wrapRegistration("json-details")
			reg.Execute = func(_ context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
				if err := api.Details(details); err != nil {
					return ToolResult{}, err
				}
				return ToolResult{Content: "done"}, nil
			}
			if err := registry.Install(&Extension{Name: "details", Tools: []ToolRegistration{reg}, ToolHooks: ToolHooks{AfterTool: func(_ context.Context, _ goai.ToolCall, result ToolResult, _ *HookAPI) (*ToolResult, error) {
				if !result.HasDetails || !equalJSONValue(result.DetailsValue, details) {
					t.Errorf("hook details fallback lost: %+v", result)
				}
				return &result, nil
			}}}); err != nil {
				t.Fatal(err)
			}
			calls := 0
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				calls++
				ch := make(chan goai.Event, 1)
				if calls == 1 {
					ch <- toolAnswer("details-call", "json-details", JSON{})
				} else {
					ch <- terminal("answer")
				}
				close(ch)
				return ch
			})
			options.Registry = registry
			h := openHarness(t, b.store, options)
			sub, err := root(t, h, ref).Submit(bg, Input{Content: "details"})
			if err != nil {
				t.Fatal(err)
			}
			waitSubmission(t, sub)
			state, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			check := func(state Snapshot) {
				for _, task := range state.Tasks {
					if task.Kind != "pi.tool" {
						continue
					}
					var cp toolCheckpoint
					if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
						t.Fatal(err)
					}
					if cp.Result == nil || cp.Result.IsError || !cp.Result.HasDetails || !equalJSONValue(cp.Result.DetailsValue, details) || !equalJSONValue(receiptMessage(*cp.Result).Details, details) {
						t.Fatal("strict JSON tool details lost", cp)
					}
				}
			}
			check(state)
			if err := h.Close(bg); err != nil {
				t.Fatal(err)
			}
			h = openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
			state, err = h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			check(state)
			if calls != 2 {
				t.Fatal("details reopen redispatched", calls)
			}
		})
	}
}
