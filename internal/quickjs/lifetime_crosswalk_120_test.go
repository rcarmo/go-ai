package quickjs

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestQuickJS120PinnedUnawaitedCallFinishCancelsAndJoins(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	result := make(chan Result, 1)
	go func() {
		result <- engine.Execute(context.Background(), `tools.wait({});return "finished";`, Options{Timeout: 3 * time.Second, SerialCalls: true, Tools: []Tool{{Name: "wait", Execute: func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
			close(entered)
			<-ctx.Done()
			close(cancelled)
			<-release
			return json.RawMessage(`"late"`), nil
		}}}})
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		unblock()
		t.Fatal("callback not started")
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("finished script did not cancel callback")
	}
	select {
	case <-result:
		t.Fatal("VM returned before host callback joined")
	case <-time.After(30 * time.Millisecond):
	}
	// Release without a double-close on any later assertion.
	unblock()
	select {
	case got := <-result:
		if !got.OK || string(got.Value) != `"finished"` {
			t.Fatal(got.Error, string(got.Value))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("VM did not join callback")
	}
}
func TestQuickJS120PinnedNeverSettlingPromise(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	result := engine.Execute(context.Background(), `await new Promise(()=>{});`, Options{Timeout: time.Second})
	if result.OK || result.Error == nil || result.Error.Kind != ErrorScript || !strings.Contains(result.Error.Message, "settle") {
		t.Fatal(result.Error)
	}
}
