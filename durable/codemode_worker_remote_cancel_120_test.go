package durable

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestCodemode120WorkerRemoteGlobalArgumentsAndCancellationJoin(t *testing.T) {
	joined := make(chan struct{})
	transport := workerTransport120(func(ctx context.Context, _ CodemodeWorkerRequest, exchange func(context.Context, []CodemodeWorkerMessage) (*CodemodeWorkerReply, error)) error {
		defer close(joined)
		if _, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "started", Fingerprint: "global-test"}}); err != nil {
			return err
		}
		reply, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "call", ID: 1, Name: "custom", Target: "global", Args: workerString120(`["one",2]`)}})
		if err != nil {
			return err
		}
		if reply == nil || reply.Type != "stop" {
			t.Error("pending RPC did not stop", reply)
		}
		return nil
	})
	vm, err := (&RemoteCodemodeSandbox{Remote: &CodemodeWorkerRemote{Transport: transport, Fingerprint: "global-test"}, Identity: "global-test"}).Start(bg, "code", CodemodeStart{RuntimeIdentity: "global-test", Globals: []CodemodeGlobalDescription{{Name: "custom"}}, ParallelCalls: true, DrainAcknowledgements: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	event, err := vm.Next(ctx)
	if err != nil || event.Call == nil || !event.Call.Global || !equalJSONValue(event.Call.Arguments, JSON{"args": []any{"one", 2}}) {
		t.Fatal(event, err)
	}
	if err := vm.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-joined:
	default:
		t.Fatal("Close returned before RPC joined")
	}
}

func TestCodemode120WorkerWireJSONFalseAndValueBounds(t *testing.T) {
	encoded, err := json.Marshal(CodemodeWorkerMessage{Type: "done", OK: false})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err = json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if ok, exists := fields["ok"]; !exists || ok != false {
		t.Fatal("missing explicit failure status", string(encoded))
	}
	for _, text := range []string{`true`, `[1,"two"]`, `null`, `{"a":1}`} {
		var value any
		if err := decodeWorkerJSON([]byte(text), DefaultLimits(), 100, &value); err != nil {
			t.Fatal(text, err)
		}
	}
	for _, text := range []string{`true false`, `{"a":1,"a":2}`, `{"a":NaN}`} {
		var value any
		if err := decodeWorkerJSON([]byte(text), DefaultLimits(), 100, &value); err == nil {
			t.Fatal("invalid JSON accepted", text)
		}
	}
}
