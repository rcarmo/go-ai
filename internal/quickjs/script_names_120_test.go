package quickjs

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestQuickJS120ExplicitScriptNameAndCollision(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	calls := 0
	tool := Tool{Name: "mcp.original-name", JSName: "custom_name", Execute: func(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
		calls++
		if string(args) != `{"n":7}` {
			t.Errorf("arguments %s", args)
		}
		return json.RawMessage(`{"ok":true}`), nil
	}}
	result := engine.Execute(context.Background(), `const value=await tools.custom_name({n:7});let hidden=false;try{tools.mcp_original_name}catch{hidden=true};return [value,hidden];`, Options{Timeout: 5 * time.Second, Tools: []Tool{tool}})
	if result.Error != nil || string(result.Value) != `[{"ok":true},true]` || calls != 1 {
		t.Fatal(result.Error, string(result.Value), calls)
	}
	other := tool
	other.Name = "other-host-name"
	result = engine.Execute(context.Background(), `return tools.custom_name({n:7});`, Options{Timeout: 5 * time.Second, Tools: []Tool{tool, other}})
	if result.Error == nil || result.Error.Kind != ErrorSandbox || calls != 1 {
		t.Fatal("collision reached host", result.Error, calls)
	}
}
