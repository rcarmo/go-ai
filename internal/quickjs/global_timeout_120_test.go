package quickjs

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"
)

func TestQuickJS120GlobalLoopTimeoutClassification(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	var calls atomic.Int64
	result := engine.Execute(context.Background(), `for(;;)await discover("x");`, Options{SerialCalls: true, Timeout: 200 * time.Millisecond, Globals: []Global{{Name: "discover", Execute: func(context.Context, json.RawMessage) (json.RawMessage, error) {
		calls.Add(1)
		return json.RawMessage(`null`), nil
	}}}})
	if result.OK || result.Error == nil || result.Error.Kind != ErrorTimeout || calls.Load() == 0 {
		t.Fatal("global timeout misclassified", result.Error, calls.Load())
	}
}
