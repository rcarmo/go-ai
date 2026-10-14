package quickjs

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestQuickJS120CancellationJoinsActualHostCallback(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	entered, cancelled, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan Result, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	opts := Options{SerialCalls: true, Timeout: 5 * time.Second, Tools: []Tool{{Name: "leaf", Execute: func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return nil, ctx.Err()
	}}}}
	go func() { returned <- engine.Execute(ctx, `await tools.leaf({});store("late",true);`, opts) }()
	select {
	case <-entered:
	case <-time.After(4 * time.Second):
		t.Fatal("callback not entered")
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("callback cancellation missing")
	}
	select {
	case result := <-returned:
		t.Fatal("execution escaped live callback", result.Error)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case result := <-returned:
		if result.OK || result.Error == nil || result.Error.Kind != ErrorAborted || len(result.StoreWrites.Set) != 0 {
			t.Fatal(result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("callback join failed")
	}
	fresh := engine.Execute(context.Background(), `return 1`, Options{})
	if !fresh.OK {
		t.Fatal("engine unusable after joined callback", fresh.Error)
	}
}
