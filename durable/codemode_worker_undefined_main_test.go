package durable

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// Pinned codemode runtime prelude-source.ts:477: a missing worker payload
// resolves undefined, while the JSON text "null" resolves null. This uses the
// actual Go remote adapter with a protocol transport, not the official worker.
func TestMainCodemodeWorkerUndefinedAndNullReply(t *testing.T) {
	const fingerprint = "undefined-worker"
	observed := make(chan struct{})
	transport := workerTransport120(func(ctx context.Context, _ CodemodeWorkerRequest, exchange func(context.Context, []CodemodeWorkerMessage) (*CodemodeWorkerReply, error)) error {
		if _, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "started", Fingerprint: fingerprint}}); err != nil {
			return err
		}
		for id, name := range []string{"nothing", "nullvalue"} {
			n := id + 1
			replies := make(chan *CodemodeWorkerReply, 1)
			errs := make(chan error, 1)
			go func() {
				reply, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "call", ID: n, Target: "global", Name: name, Args: workerString120(`null`)}})
				if err != nil {
					errs <- err
				} else {
					replies <- reply
				}
			}()
			select {
			case <-observed:
			case <-ctx.Done():
				return ctx.Err()
			}
			if _, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "idle", Seq: n - 1, Issued: n}}); err != nil {
				return err
			}
			var reply *CodemodeWorkerReply
			select {
			case reply = <-replies:
			case err := <-errs:
				return err
			case <-ctx.Done():
				return ctx.Err()
			}
			if reply == nil || reply.Type != "result" || !reply.OK || reply.ID != n || reply.Seq != n {
				return errors.New("incorrect worker result metadata")
			}
			encoded, err := json.Marshal(reply)
			if err != nil {
				return err
			}
			var wire map[string]any
			if err := json.Unmarshal(encoded, &wire); err != nil {
				return err
			}
			payload, present := wire["payload"]
			if n == 1 && (reply.Payload != nil || present) {
				return errors.New("undefined result gained payload")
			}
			if n == 2 && (reply.Payload == nil || *reply.Payload != "null" || !present || payload != "null") {
				return errors.New("null result lost payload")
			}
		}
		if _, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "idle", Seq: 2, Issued: 2}}); err != nil {
			return err
		}
		_, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "done", OK: true, Value: workerString120(`[true,true]`), Writes: workerString120(`[]`), During: &CodemodeWorkerDuring{Seq: 2, Issued: 2}}})
		return err
	})
	ctx, cancel := context.WithTimeout(bg, 10*time.Second)
	defer cancel()
	vm, err := (&RemoteCodemodeSandbox{Remote: &CodemodeWorkerRemote{Transport: transport, Fingerprint: fingerprint}, Identity: fingerprint}).Start(ctx, "script", CodemodeStart{RuntimeIdentity: fingerprint, ParallelCalls: true, DrainAcknowledgements: true, Globals: []CodemodeGlobalDescription{{Name: "nothing", Version: 1}, {Name: "nullvalue", Version: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	for id, name := range []string{"nothing", "nullvalue"} {
		event, err := vm.Next(ctx)
		if err != nil || event.Call == nil || !event.Call.Global || event.Call.ID != id+1 || event.Call.Name != name {
			t.Fatal(event, err)
		}
		call := *event.Call
		observed <- struct{}{}
		event, err = vm.Next(ctx)
		if err != nil || event.Drain == nil || event.Drain.Issued != id+1 {
			t.Fatal(event, err)
		}
		if err = vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *event.Drain); err != nil {
			t.Fatal(err)
		}
		if err = vm.Deliver(ctx, CodemodeDelivery{Seq: id + 1, Call: call, HasValue: id == 1, Value: nil}); err != nil {
			t.Fatal(err)
		}
	}
	event, err := vm.Next(ctx)
	if err != nil || event.Drain == nil || event.Drain.DeliveryID != 2 {
		t.Fatal(event, err)
	}
	if err = vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *event.Drain); err != nil {
		t.Fatal(err)
	}
	event, err = vm.Next(ctx)
	if err != nil || event.Result == nil || event.Result.Error != "" || !equalJSONValue(event.Result.Value, []any{true, true}) {
		t.Fatal(event, err)
	}
}
