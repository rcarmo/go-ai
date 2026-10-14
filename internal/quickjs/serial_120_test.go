package quickjs

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestQuickJS120SerialAdmissionRejectsOverlapBeforeSecondCallback(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	var first, second atomic.Int64
	opts := Options{SerialCalls: true, Timeout: 3 * time.Second, Tools: []Tool{{Name: "first", Execute: func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		first.Add(1)
		<-ctx.Done()
		return nil, ctx.Err()
	}}, {Name: "second", Execute: func(context.Context, json.RawMessage) (json.RawMessage, error) { second.Add(1); return nil, nil }}}}
	result := engine.Execute(context.Background(), `await Promise.all([tools.first({}),tools.second({})]);`, opts)
	if result.OK || result.Error == nil || !strings.Contains(result.Error.Message, "parallel") {
		t.Fatal(result.Error)
	}
	if first.Load() != 1 || second.Load() != 0 {
		t.Fatal("VM-order admission lost", first.Load(), second.Load())
	}
}
