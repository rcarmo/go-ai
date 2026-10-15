package quickjs

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestQuickJS120OrdinaryClockRandomAndExplicitZeroDeterminism(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	before := time.Now().UnixMilli()
	var previous float64
	for i := 0; i < 2; i++ {
		result := engine.Execute(context.Background(), `return [Date.now(),Math.random(),performance.now()]`, Options{})
		if result.Error != nil {
			t.Fatal(result.Error)
		}
		var values []float64
		if err = json.Unmarshal(result.Value, &values); err != nil {
			t.Fatal(err)
		}
		if len(values) != 3 || values[0] < float64(before) || values[0] > float64(time.Now().UnixMilli()) || values[1] < 0 || values[1] >= 1 || values[2] < 0 {
			t.Fatal(values)
		}
		if i > 0 && values[1] == previous {
			t.Fatal("ordinary random repeated deterministic seed")
		}
		previous = values[1]
	}
	var first string
	for i := 0; i < 2; i++ {
		result := engine.Execute(context.Background(), `return [Date.now(),Math.random()]`, Options{Deterministic: true})
		if result.Error != nil {
			t.Fatal(result.Error)
		}
		if i == 0 {
			first = string(result.Value)
		} else if string(result.Value) != first {
			t.Fatal("explicit zero deterministic start diverged")
		}
	}
}

func TestQuickJS120NoDeadlineStillCancelsAndDetectsStall(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := engine.Execute(ctx, `return await tools.wait({})`, Options{NoDeadline: true, Timeout: time.Nanosecond, Tools: []Tool{{Name: "wait", Execute: func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		select {
		case <-time.After(20 * time.Millisecond):
			return json.RawMessage(`"late"`), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}}})
	if result.Error != nil || string(result.Value) != `"late"` {
		t.Fatal(result.Error, string(result.Value))
	}
	result = engine.Execute(ctx, `await new Promise(()=>{});return 1`, Options{NoDeadline: true})
	if result.Error == nil || result.Error.Kind != ErrorScript {
		t.Fatal("unsettleable promise did not fail", result.Error)
	}
	aborted, abort := context.WithCancel(context.Background())
	abort()
	result = engine.Execute(aborted, `while(true){}`, Options{NoDeadline: true})
	if result.Error == nil || result.Error.Kind != ErrorAborted {
		t.Fatal("parent abort ignored", result.Error)
	}
}
