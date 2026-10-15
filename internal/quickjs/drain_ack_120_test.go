package quickjs

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestQuickJS120DrainAcknowledgesIssuedCounts(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	initial := make(chan struct{})
	var acknowledged [][2]int
	callback := func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		select {
		case <-initial:
			return json.RawMessage(`null`), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	result := engine.Execute(context.Background(), `const a=tools.first({}).then(()=>tools.third({}));const b=host.second({});await Promise.all([a,b]);return "done";`, Options{Timeout: 5 * time.Second, OrderedSettlements: true, Tools: []Tool{{Name: "first", Execute: callback}, {Name: "third", Execute: callback}}, Globals: []Global{{Name: "host.second", Execute: callback}}, OnDrain: func(ctx context.Context, id, issued int) error {
		acknowledged = append(acknowledged, [2]int{id, issued})
		if id == 0 {
			close(initial)
		}
		return nil
	}})
	if !result.OK {
		t.Fatal(result.Error)
	}
	expected := [][2]int{{0, 2}, {1, 3}, {2, 3}, {3, 3}}
	if !reflect.DeepEqual(acknowledged, expected) {
		t.Fatalf("drain counts %v; want %v", acknowledged, expected)
	}
}

func TestQuickJS120DrainFailureStopsContinuationAndJoins(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	active, joined := make(chan struct{}), make(chan struct{})
	effects := atomic.Int64{}
	result := engine.Execute(context.Background(), `await tools.first({});await tools.effect({});`, Options{Timeout: 5 * time.Second, Tools: []Tool{{Name: "first", Execute: func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		close(active)
		<-ctx.Done()
		close(joined)
		return nil, ctx.Err()
	}}, {Name: "effect", Execute: func(context.Context, json.RawMessage) (json.RawMessage, error) {
		effects.Add(1)
		return json.RawMessage(`null`), nil
	}}}, OnDrain: func(ctx context.Context, id, issued int) error {
		if id != 0 || issued != 1 {
			return errors.New("unexpected count")
		}
		select {
		case <-active:
			return errors.New("journal commit rejected")
		case <-ctx.Done():
			return ctx.Err()
		}
	}})
	if result.OK || result.Error == nil || effects.Load() != 0 {
		t.Fatal(result.Error, effects.Load())
	}
	select {
	case <-joined:
	default:
		t.Fatal("failed acknowledgement did not join callback")
	}
}

func TestQuickJS120DrainCancellation(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	results := make(chan Result, 1)
	go func() {
		results <- engine.Execute(ctx, `store("uncommitted",true);return "done";`, Options{Timeout: 5 * time.Second, OnDrain: func(ctx context.Context, id, issued int) error { close(entered); <-ctx.Done(); return ctx.Err() }})
	}()
	select {
	case <-entered:
		cancel()
	case <-time.After(3 * time.Second):
		t.Fatal("missing initial acknowledgement")
	}
	select {
	case result := <-results:
		if result.OK || result.Error == nil || result.Error.Kind != ErrorAborted || len(result.StoreWrites.Set) != 0 {
			t.Fatal(result.Error)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("acknowledgement ignored cancellation")
	}
}
