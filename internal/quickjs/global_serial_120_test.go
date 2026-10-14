package quickjs

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestQuickJS120SerialGlobalToolOverlap(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	var globalCalls, toolCalls atomic.Int64
	opts := Options{SerialCalls: true, Timeout: 3 * time.Second, Globals: []Global{{Name: "discover", Execute: func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		globalCalls.Add(1)
		<-ctx.Done()
		return nil, ctx.Err()
	}}}, Tools: []Tool{{Name: "leaf", Execute: func(context.Context, json.RawMessage) (json.RawMessage, error) { toolCalls.Add(1); return nil, nil }}}}
	result := engine.Execute(context.Background(), `await Promise.all([discover("x"),tools.leaf({})]);`, opts)
	if result.OK || result.Error == nil || !strings.Contains(result.Error.Message, "parallel") || globalCalls.Load() != 1 || toolCalls.Load() != 0 {
		t.Fatal(result.Error, globalCalls.Load(), toolCalls.Load())
	}
}
