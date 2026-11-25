package quickjs

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Source: pi v1.6.1 packages/codemode/test/sandbox.test.ts:318–347.
func TestSourceV161QuickJSToolAliases(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	t.Run("first-normalized-name", func(t *testing.T) {
		var dash, shadow, mcp atomic.Int64
		constant := func(calls *atomic.Int64, data string) func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
				calls.Add(1)
				return json.RawMessage(data), nil
			}
		}
		opts := Options{Tools: []Tool{
			{Name: "my-tool", Description: "Dashes", Execute: constant(&dash, `"dash"`)},
			{Name: "my_tool", Description: "Shadowed", Execute: constant(&shadow, `"underscore"`)},
			{Name: "mcp__docs__search", Execute: constant(&mcp, `"mcp"`)},
		}}
		got := engine.Execute(ctx, `
			try { ALL_TOOLS.push({}); } catch {}
			return {
				all: ALL_TOOLS,
				calls: [await tools.my_tool(), await tools["my-tool"](), await tools.mcp__docs__search()],
			};`, opts)
		const want = `{"all":[{"name":"my_tool","description":"Dashes"},{"name":"mcp__docs__search","description":""}],"calls":["dash","dash","mcp"]}`
		if !got.OK || string(got.Value) != want {
			t.Errorf("ok=%v value=%s error=%+v; want the first normalized alias and exact catalog", got.OK, got.Value, got.Error)
		}
		if dash.Load() != 2 || shadow.Load() != 0 || mcp.Load() != 1 {
			t.Errorf("callbacks dash/shadow/mcp=%d/%d/%d; want 2/0/1", dash.Load(), shadow.Load(), mcp.Load())
		}
		// Explicit catalogue aliases must remain strict and admit no effect.
		opts.Tools[0].JSName = "my_tool"
		strict := engine.ExecuteStart(ctx, `return await tools.my_tool()`, opts)
		if strict.OK || strict.Error == nil || strict.Error.Kind != ErrorSandbox || len(strict.Calls) != 0 {
			t.Errorf("explicit collision result=%+v; want setup refusal", strict)
		}
		if dash.Load() != 2 || shadow.Load() != 0 || mcp.Load() != 1 {
			t.Error("the rejected start dispatched a callback")
		}

		// With the names reversed the normalized alias follows registration
		// order, while the distinct raw name continues to call its own tool.
		opts.Tools[0].JSName = ""
		opts.Tools[0], opts.Tools[1] = opts.Tools[1], opts.Tools[0]
		reversed := engine.Execute(ctx, `return [await tools.my_tool(), await tools["my-tool"](), ALL_TOOLS[0].description]`, opts)
		if !reversed.OK || string(reversed.Value) != `["underscore","dash","Shadowed"]` {
			t.Errorf("reversed registrations = %+v; want the first normalized alias and the distinct raw alias", reversed)
		}
		if dash.Load() != 3 || shadow.Load() != 1 || mcp.Load() != 1 {
			t.Error("the reversed run did not dispatch exactly one callback per alias")
		}

		// A shadowed descriptor still contributes to the existing setup bound.
		opts.Tools[1].Description = strings.Repeat("x", int(maxBridgeStringBytes)+1)
		oversized := engine.Execute(ctx, `return await tools.my_tool()`, opts)
		if oversized.OK || oversized.Error == nil || oversized.Error.Kind != ErrorSandbox || len(oversized.Calls) != 0 || shadow.Load() != 1 {
			t.Errorf("oversized shadowed descriptor = %+v; want setup refusal without dispatch", oversized)
		}
	})
	t.Run("undefined-versus-null", func(t *testing.T) {
		seen := make(chan string, 2)
		got := engine.Execute(ctx, `return [await tools.noop(), await tools.noop(null)]`, Options{Tools: []Tool{{Name: "noop", Execute: func(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
			seen <- string(args)
			return args, nil
		}}}})
		if !got.OK || string(got.Value) != `[null,null]` {
			t.Errorf("ok=%v value=%s error=%+v; want [null,null]", got.OK, got.Value, got.Error)
		}
		close(seen)
		var args []string
		for arg := range seen {
			args = append(args, arg)
		}
		if !reflect.DeepEqual(args, []string{"", "null"}) {
			t.Errorf("arguments=%q; want missing then explicit null", args)
		}
	})
}
