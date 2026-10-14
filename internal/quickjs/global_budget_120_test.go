package quickjs

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestQuickJS120GlobalCallsShareAdmissionBudget(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	var calls atomic.Int64
	result := engine.Execute(context.Background(), `for(let i=0;i<100;i++)await discover("x");`, Options{MaxCalls: 32, SerialCalls: true, Timeout: 10 * time.Second, Globals: []Global{{Name: "discover", Execute: func(context.Context, json.RawMessage) (json.RawMessage, error) {
		calls.Add(1)
		return json.RawMessage(`null`), nil
	}}}})
	if result.OK || result.Error == nil || result.Error.Kind != ErrorSandbox || !strings.Contains(result.Error.Message, "resource") || calls.Load() != 32 {
		t.Fatal("global budget", result.Error, calls.Load())
	}
}
