package quickjs

import (
	"context"
	"encoding/json"
	"math"
	"sync/atomic"
	"testing"
)

func TestQuickJS120PerformanceClockNanosecondOverflow(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	var clock atomic.Int64
	clock.Store(1000)
	result := engine.Execute(context.Background(), `const before=performance.now();await tools.advance({});return [before,performance.now()];`, Options{StartedAt: 1000, Clock: clock.Load, Tools: []Tool{{Name: "advance", Execute: func(context.Context, json.RawMessage) (json.RawMessage, error) {
		clock.Store(math.MaxInt64)
		return json.RawMessage(`null`), nil
	}}}})
	if !result.OK {
		t.Fatal(result.Error)
	}
	var values []float64
	if err := json.Unmarshal(result.Value, &values); err != nil || len(values) != 2 {
		t.Fatal(string(result.Value), err)
	}
	if values[0] != 0 || values[1] <= 0 || math.IsInf(values[1], 0) || math.IsNaN(values[1]) {
		t.Fatal("elapsed clock overflow", values)
	}
}
