package quickjs

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestQuickJS120OrderedParallelSettlementAcrossToolAndGlobal(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	secondDone := make(chan struct{})
	releaseFirst := make(chan struct{})
	results := make(chan Result, 1)
	go func() {
		results <- engine.Execute(context.Background(), `const seen=[];const a=tools.first({}).then(v=>seen.push(v));const b=host.second({}).then(v=>seen.push(v));await Promise.all([a,b]);return seen;`, Options{OrderedSettlements: true, Timeout: 5 * time.Second, Tools: []Tool{{Name: "first", Execute: func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
			select {
			case <-releaseFirst:
				return json.RawMessage(`"first"`), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}}}, Globals: []Global{{Name: "host.second", Execute: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			close(secondDone)
			return json.RawMessage(`"second"`), nil
		}}}})
	}()
	select {
	case <-secondDone:
	case <-time.After(3 * time.Second):
		close(releaseFirst)
		t.Fatal("parallel callback did not execute")
	}
	select {
	case result := <-results:
		close(releaseFirst)
		t.Fatal("settled without first delivery", result.Error)
	case <-time.After(30 * time.Millisecond):
	}
	close(releaseFirst)
	select {
	case result := <-results:
		if !result.OK {
			t.Fatal(result.Error)
		}
		var value []string
		if err = json.Unmarshal(result.Value, &value); err != nil || !reflect.DeepEqual(value, []string{"first", "second"}) {
			t.Fatal(string(result.Value), err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("settlement stalled")
	}
}

func TestQuickJS120OrderedCancellationJoinsPendingCallback(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, cancelled, release, second := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	results := make(chan Result, 1)
	effects := atomic.Int64{}
	go func() {
		results <- engine.Execute(ctx, `const a=tools.first({});const b=host.second({}).then(()=>tools.effect({}));await Promise.all([a,b]);store("late",true);`, Options{OrderedSettlements: true, Timeout: 5 * time.Second, Tools: []Tool{{Name: "first", Execute: func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
			close(entered)
			<-ctx.Done()
			close(cancelled)
			<-release
			return json.RawMessage(`null`), nil
		}}, {Name: "effect", Execute: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			effects.Add(1)
			return json.RawMessage(`null`), nil
		}}}, Globals: []Global{{Name: "host.second", Execute: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			close(second)
			return json.RawMessage(`null`), nil
		}}}})
	}()
	for _, signal := range []chan struct{}{entered, second} {
		select {
		case <-signal:
		case <-time.After(3 * time.Second):
			cancel()
			t.Fatal("callbacks not started")
		}
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("callback not canceled")
	}
	select {
	case <-results:
		t.Fatal("returned before callback join")
	case <-time.After(30 * time.Millisecond):
	}
	unblock()
	select {
	case result := <-results:
		if result.OK || result.Error == nil || result.Error.Kind != ErrorAborted || effects.Load() != 0 || len(result.StoreWrites.Set) != 0 {
			t.Fatal(result.Error, effects.Load(), result.StoreWrites)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("join stalled")
	}
}

func TestQuickJS120OrderedCompletionDiscardsLaterContinuation(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	effects := atomic.Int64{}
	result := engine.Execute(context.Background(), `tools.first({});host.second({}).then(()=>tools.effect({}));return "finished";`, Options{OrderedSettlements: true, Timeout: 3 * time.Second, Tools: []Tool{{Name: "first", Execute: func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}, {Name: "effect", Execute: func(context.Context, json.RawMessage) (json.RawMessage, error) {
		effects.Add(1)
		return json.RawMessage(`null`), nil
	}}}, Globals: []Global{{Name: "host.second", Execute: func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`null`), nil }}}})
	if !result.OK || string(result.Value) != `"finished"` || effects.Load() != 0 {
		t.Fatal(result.Error, string(result.Value), effects.Load())
	}
}
