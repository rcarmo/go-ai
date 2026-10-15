package quickjs

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestQuickJS120PinnedGlobalsAndFrozenNamespaces(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	calls := 0
	var received []any
	opts := Options{Timeout: 3 * time.Second, SerialCalls: true, Globals: []Global{{Name: "double", Execute: func(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
		calls++
		var n int
		if err := json.Unmarshal(args, &n); err != nil {
			return nil, err
		}
		return json.Marshal(n * 2)
	}}, {Name: "models.inspect", Spread: true, Execute: func(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
		if err := json.Unmarshal(args, &received); err != nil {
			return nil, err
		}
		return json.RawMessage(`7`), nil
	}}}}
	result := engine.Execute(context.Background(), `const twice=await double(4);try{models.extra=()=>1}catch{};const value=await models.inspect("model",{n:1},undefined);return [twice,value,"extra" in models,"missing" in models,Object.isFrozen(models)];`, opts)
	if !result.OK {
		t.Fatal(result.Error)
	}
	var got []any
	if err = json.Unmarshal(result.Value, &got); err != nil || !reflect.DeepEqual(got, []any{float64(8), float64(7), false, false, true}) {
		t.Fatal(string(result.Value), err)
	}
	if !reflect.DeepEqual(received, []any{"model", map[string]any{"n": float64(1)}, nil}) || calls != 1 || len(result.Calls) != 0 {
		t.Fatal(received, calls, result.Calls)
	}
	result = engine.Execute(context.Background(), `try{await models.inspec("x")}catch(e){return e.message}`, opts)
	if !result.OK {
		t.Fatal(result.Error)
	}
	var message string
	if err = json.Unmarshal(result.Value, &message); err != nil || !strings.Contains(message, "models.inspect") || !strings.Contains(message, "does not exist") {
		t.Fatal(message, err)
	}
	for _, name := range []string{"store", "load"} {
		result = engine.Execute(context.Background(), `return 1`, Options{Globals: []Global{{Name: name, Execute: func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`null`), nil }}}})
		if result.OK || result.Error == nil {
			t.Fatal("helper shadow accepted", name)
		}
	}
}
