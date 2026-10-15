package quickjs

import (
	"context"
	"encoding/json"
	"testing"
)

func TestQuickJS120DeterministicEnvironmentAndVMIsolation(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	code := `return [new Date(0).getTimezoneOffset(),new Date('1970-01-01T00:00:00').getTime(),typeof WeakRef,typeof FinalizationRegistry,Date.now(),Math.random(),Math.random()];`
	var first string
	for attempt := 0; attempt < 2; attempt++ {
		result := engine.Execute(context.Background(), code, Options{StartedAt: 1000, Seed: 0})
		if result.Error != nil {
			t.Fatal(result.Error)
		}
		if attempt == 0 {
			first = string(result.Value)
		} else if string(result.Value) != first {
			t.Fatal("same start diverged", first, string(result.Value))
		}
	}
	var values []any
	if err := json.Unmarshal([]byte(first), &values); err != nil {
		t.Fatal(err)
	}
	if len(values) != 7 || values[0] != float64(0) || values[1] != float64(0) || values[2] != "undefined" || values[3] != "undefined" || values[4] != float64(1000) {
		t.Fatal(first)
	}
	result := engine.Execute(context.Background(), `globalThis.injected=7;return 1`, Options{})
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	result = engine.Execute(context.Background(), `return typeof injected`, Options{})
	if result.Error != nil || string(result.Value) != `"undefined"` {
		t.Fatal("VM state leaked", result.Error, string(result.Value))
	}
}
