package quickjs

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestQuickJS120IndependentExecutionsAndRecursion(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	codes := []string{`globalThis.shared="a";await null;return globalThis.shared;`, `globalThis.shared="b";await null;return globalThis.shared;`, `return typeof globalThis.shared;`}
	results := make([]Result, len(codes))
	var wg sync.WaitGroup
	for i, code := range codes {
		wg.Add(1)
		go func(i int, code string) {
			defer wg.Done()
			results[i] = engine.Execute(context.Background(), code, Options{Timeout: 3 * time.Second})
		}(i, code)
	}
	wg.Wait()
	for i, want := range []string{`"a"`, `"b"`, `"undefined"`} {
		if !results[i].OK || string(results[i].Value) != want {
			t.Fatal(i, results[i].Error, string(results[i].Value))
		}
	}
	result := engine.Execute(context.Background(), `let depth=0;function dive(){depth++;dive()};try{dive()}catch(e){return [e.name,depth>1000]}`, Options{Timeout: 3 * time.Second})
	if !result.OK {
		t.Fatal(result.Error)
	}
	var value []any
	if err = json.Unmarshal(result.Value, &value); err != nil || !reflect.DeepEqual(value, []any{"RangeError", true}) {
		t.Fatal(string(result.Value), err)
	}
}
