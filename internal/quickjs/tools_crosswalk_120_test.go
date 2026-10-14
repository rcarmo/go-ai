package quickjs

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestQuickJS120PinnedToolAliasesListingsAndErrors(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	calls := 0
	opts := Options{Timeout: 3 * time.Second, SerialCalls: true, Tools: []Tool{{Name: "read-file", JSName: "read_file", Description: "Read a file", Execute: func(_ context.Context, args json.RawMessage) (json.RawMessage, error) { calls++; return args, nil }}, {Name: "fail", Execute: func(context.Context, json.RawMessage) (json.RawMessage, error) { return nil, errors.New("boom") }}}}
	result := engine.Execute(context.Background(), `ALL_TOOLS.push("bad");return [await tools.read_file({value:7}),ALL_TOOLS];`, opts)
	// Frozen metadata must reject mutation without releasing a host effect.
	if result.OK || result.Error == nil || calls != 0 {
		t.Fatal(result.Error, calls)
	}
	result = engine.Execute(context.Background(), `return [await tools.read_file({value:7}),ALL_TOOLS];`, opts)
	if !result.OK {
		t.Fatal(result.Error)
	}
	var values []any
	if err = json.Unmarshal(result.Value, &values); err != nil || len(values) != 2 {
		t.Fatal(string(result.Value), err)
	}
	if !reflect.DeepEqual(values[0], map[string]any{"value": float64(7)}) {
		t.Fatal(values)
	}
	if !reflect.DeepEqual(values[1], []any{map[string]any{"name": "read_file", "description": "Read a file"}, map[string]any{"name": "fail", "description": ""}}) {
		t.Fatal("tool listing", values[1])
	}
	result = engine.Execute(context.Background(), `try{await tools.fail({});return "resolved"}catch(e){return [e instanceof Error,e.message]}`, opts)
	if !result.OK {
		t.Fatal(result.Error)
	}
	if err = json.Unmarshal(result.Value, &values); err != nil || !reflect.DeepEqual(values, []any{true, "boom"}) {
		t.Fatal(string(result.Value), err)
	}
	result = engine.Execute(context.Background(), `return ["missing" in tools,"read_file" in tools,typeof tools.toString,JSON.stringify(tools)];`, opts)
	if !result.OK {
		t.Fatal(result.Error)
	}
	if err = json.Unmarshal(result.Value, &values); err != nil || !reflect.DeepEqual(values, []any{false, true, "undefined", "{}"}) {
		t.Fatal(string(result.Value), err)
	}
	result = engine.Execute(context.Background(), `try{await tools.missing({});return "resolved"}catch(e){return e.message}`, opts)
	if !result.OK || string(result.Value) == `"resolved"` {
		t.Fatal(result.Error, string(result.Value))
	}
}
