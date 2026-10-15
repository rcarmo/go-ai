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
	result := engine.Execute(context.Background(), `for(;;)await discover("x");`, Options{SerialCalls: true, Timeout: 200 * time.Millisecond, Globals: []Global{{Name: "discover", Execute: func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		calls.Add(1)
		// Zero-latency calls can exhaust the separate 4096-call ceiling
		// before 200ms on fast hosts. Isolate wall time from admission.
		timer := time.NewTimer(time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			return json.RawMessage(`null`), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}}})
	if result.OK || result.Error == nil || result.Error.Kind != ErrorTimeout || calls.Load() == 0 || calls.Load() >= maxBridgeOutputItems {
		t.Fatal("global timeout misclassified", result.Error, calls.Load())
	}
}
