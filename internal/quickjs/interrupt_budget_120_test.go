package quickjs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestQuickJS120ComputeBudgetLoopsAndWaitPersistence(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	for _, code := range []string{`for(;;){}`, `for(;;)await null;`, `await tools.wait({});for(;;){}`} {
		calls := 0
		started := time.Now()
		result := engine.Execute(context.Background(), code, Options{InterruptBudget: 10, Timeout: 5 * time.Second, Tools: []Tool{{Name: "wait", Execute: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			calls++
			time.Sleep(30 * time.Millisecond)
			return json.RawMessage(`null`), nil
		}}}})
		if result.OK || result.Error == nil || result.Error.Kind != ErrorTimeout || !strings.Contains(result.Error.Message, "interrupt budget of 10 polls") || time.Since(started) > 3*time.Second {
			t.Fatal(code, result.Error, time.Since(started))
		}
		if strings.Contains(code, "tools.wait") && calls != 1 {
			t.Fatal(calls)
		}
	}
	result := engine.Execute(context.Background(), `return 7`, Options{InterruptBudget: 10})
	if !result.OK || string(result.Value) != "7" {
		t.Fatal(result.Error)
	}
	if result = engine.Execute(context.Background(), `return 1`, Options{InterruptBudget: -1}); result.OK || result.Error == nil {
		t.Fatal("negative budget accepted")
	}
}
