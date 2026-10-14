package quickjs

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestQuickJS120PinnedStoreMutationAndFailureReports(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	opts := Options{Timeout: 3 * time.Second, Store: map[string]json.RawMessage{"counter": json.RawMessage(`3`), "to_delete": json.RawMessage(`"bye"`), "value": json.RawMessage(`{"n":1}`)}}
	result := engine.Execute(context.Background(), `store("counter",load("counter")+1);store("value",{n:2});const copy=load("value");copy.n=7;store("to_delete",undefined);text(load("counter"));return [load("value"),load("missing")===undefined];`, opts)
	if !result.OK {
		t.Fatal(result.Error)
	}
	var got any
	if err = json.Unmarshal(result.Value, &got); err != nil {
		t.Fatal(err)
	}
	want := []any{map[string]any{"n": float64(2)}, true}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	if string(opts.Store["counter"]) != "3" || string(opts.Store["value"]) != "{\"n\":1}" || string(result.StoreWrites.Set["counter"]) != "4" || string(result.StoreWrites.Set["value"]) != "{\"n\":2}" || !reflect.DeepEqual(result.StoreWrites.Delete, []string{"to_delete"}) {
		t.Fatal(opts.Store, result.StoreWrites)
	}
	failed := engine.Execute(context.Background(), `store("pending",1);throw Error("later");`, Options{Timeout: time.Second})
	if failed.OK || failed.Error == nil || len(failed.StoreWrites.Set) != 0 || len(failed.StoreWrites.Delete) != 0 {
		t.Fatal(failed.Error, failed.StoreWrites)
	}
}
func TestQuickJS120PinnedStoreLimitsAndRecovery(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	codes := []string{`store(1,"x")`, `store("big","x".repeat(262144))`, `store("k".repeat(1048577),1)`, `for(let i=0;i<10;i++)store(String(i),"x".repeat(200000));`, `const cycle={};cycle.self=cycle;store("cycle",cycle);`}
	for _, code := range codes {
		result := engine.Execute(context.Background(), code, Options{Timeout: 3 * time.Second, MemoryLimitBytes: 32 << 20})
		if result.OK || result.Error == nil || result.Error.Kind != ErrorScript {
			t.Fatal(code, result.Error)
		}
	}
	result := engine.Execute(context.Background(), `store("ok",null);return load("ok")`, Options{Timeout: time.Second})
	if !result.OK || string(result.Value) != "null" || string(result.StoreWrites.Set["ok"]) != "null" {
		t.Fatal(result)
	}
}
