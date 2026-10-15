package durable

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type workerTransport120 func(context.Context, CodemodeWorkerRequest, func(context.Context, []CodemodeWorkerMessage) (*CodemodeWorkerReply, error)) error

func (f workerTransport120) Execute(ctx context.Context, r CodemodeWorkerRequest, e func(context.Context, []CodemodeWorkerMessage) (*CodemodeWorkerReply, error)) error {
	return f(ctx, r, e)
}
func workerString120(s string) *string { return &s }

func TestCodemode120WorkerRemoteBatchesParallelDrainsAndTerminal(t *testing.T) {
	const fingerprint = "official-worker-test-120"
	workerCallObserved120 := make(chan struct{})
	transport := workerTransport120(func(ctx context.Context, request CodemodeWorkerRequest, exchange func(context.Context, []CodemodeWorkerMessage) (*CodemodeWorkerReply, error)) error {
		if request.Data.Code != "script" || request.Data.Deterministic == nil || request.Data.Deterministic.Seed != 7 || request.Data.Store["previous"] != "true" || len(request.Data.Tools) != 1 || request.Data.Tools[0].JSName != "leaf" {
			return errors.New("incorrect worker start")
		}
		raw, err := json.Marshal(request)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), "RuntimeIdentity") {
			return errors.New("native start leaked")
		}
		if _, err = exchange(ctx, []CodemodeWorkerMessage{{Type: "started", Fingerprint: fingerprint}, {Type: "output", Item: &CodemodeWorkerOutput{Type: "text", Text: "before\n"}}}); err != nil {
			return err
		}
		var wg sync.WaitGroup
		replies := make(chan *CodemodeWorkerReply, 2)
		errs := make(chan error, 2)
		// These concurrent RPCs wait for host delivery. Starting idle must not wait
		// for either result, because idle is what authorises both host effects.
		for id := 1; id <= 2; id++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				reply, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "call", ID: id, Target: "tool", Name: "leaf", Args: workerString120(`{}`)}})
				if err != nil {
					errs <- err
					return
				}
				replies <- reply
			}(id)
			// Wait for adapter registration in deterministic call order.
			// The host test observes each call before allowing the next RPC.
			select {
			case <-workerCallObserved120:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if _, err = exchange(ctx, []CodemodeWorkerMessage{{Type: "idle", Seq: 0, Issued: 2}}); err != nil {
			return err
		}
		select {
		case <-replies:
		case err := <-errs:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
		if _, err = exchange(ctx, []CodemodeWorkerMessage{{Type: "idle", Seq: 1, Issued: 2}}); err != nil {
			return err
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				return err
			}
		}
		second := <-replies
		// Replies may reach goroutines in either scheduling order; delivery sequence
		// is the deterministic VM application order, not RPC completion timing.
		if second.Seq != 2 || second.Payload == nil {
			return errors.New("invalid worker replies")
		}
		_, err = exchange(ctx, []CodemodeWorkerMessage{{Type: "output", Item: &CodemodeWorkerOutput{Type: "text", Text: "after"}}, {Type: "done", OK: true, Value: workerString120(`{"ok":true}`), Writes: workerString120(`[["saved","true"],["removed"]]`), During: &CodemodeWorkerDuring{Seq: 2, Issued: 2}}})
		return err
	})
	adapter := &CodemodeWorkerRemote{Transport: transport, Fingerprint: fingerprint}
	sandbox := &RemoteCodemodeSandbox{Remote: adapter, Identity: fingerprint}
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	vm, err := sandbox.Start(ctx, "script", CodemodeStart{RuntimeIdentity: fingerprint, ParallelCalls: true, DrainAcknowledgements: true, Seed: 7, Store: JSON{"previous": true}, Tools: []CodemodeToolDescription{{Name: "leaf", Identifier: "leaf"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	for id := 1; id <= 2; id++ {
		event, err := vm.Next(ctx)
		if err != nil || event.Call == nil || event.Call.ID != id {
			t.Fatal(event, err)
		}
		workerCallObserved120 <- struct{}{}
	}
	event, err := vm.Next(ctx)
	if err != nil || event.Drain == nil || event.Drain.Issued != 2 {
		t.Fatal(event, err)
	}
	if err := vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *event.Drain); err != nil {
		t.Fatal(err)
	}
	for seq, id := range []int{2, 1} {
		if err := vm.Deliver(ctx, CodemodeDelivery{Seq: seq + 1, Call: CodemodeCall{ID: id, Name: "leaf", Arguments: JSON{}}, HasValue: true, Value: JSON{"id": id}, Now: int64(seq + 10)}); err != nil {
			t.Fatal(err)
		}
		event, err = vm.Next(ctx)
		if err != nil || event.Drain == nil || event.Drain.DeliveryID != id {
			t.Fatal(event, err)
		}
		if err = vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *event.Drain); err != nil {
			t.Fatal(err)
		}
	}
	event, err = vm.Next(ctx)
	if err != nil || event.Result == nil {
		t.Fatal(event, err)
	}
	result := event.Result
	if !strings.Contains(result.Output, "==> text 1/3 <==\nbefore") || !strings.Contains(result.Output, "==> text 2/3 <==\nafter") || !strings.Contains(result.Output, "==> text 3/3 <==\n{\n  \"ok\": true\n}") || !equalJSONValue(result.Value, JSON{"ok": true}) || !equalJSONValue(result.StoreWrites, JSON{"saved": true}) || len(result.StoreDeletes) != 1 || result.StoreDeletes[0] != "removed" {
		t.Fatal(result)
	}
}

func TestCodemode120WorkerRemoteRejectsProtocolAndUnsettledTransport(t *testing.T) {
	for _, kind := range []string{"fingerprint", "unknown", "batch", "early", "terminal-transport"} {
		t.Run(kind, func(t *testing.T) {
			cause := errors.New("transport after terminal")
			transport := workerTransport120(func(ctx context.Context, _ CodemodeWorkerRequest, exchange func(context.Context, []CodemodeWorkerMessage) (*CodemodeWorkerReply, error)) error {
				if kind == "early" {
					return nil
				}
				fingerprint := "expected"
				if kind == "fingerprint" {
					fingerprint = "forged"
				}
				if _, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "started", Fingerprint: fingerprint}}); err != nil {
					return err
				}
				if kind == "unknown" {
					_, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "unexpected"}})
					return err
				}
				if kind == "batch" {
					_, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "idle"}, {Type: "output", Item: &CodemodeWorkerOutput{Type: "text"}}})
					return err
				}
				_, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "done", OK: false, Error: json.RawMessage(`"{\"message\":\"script failed\"}"`), During: &CodemodeWorkerDuring{}}})
				if err != nil {
					return err
				}
				return cause
			})
			vm, err := (&RemoteCodemodeSandbox{Remote: &CodemodeWorkerRemote{Transport: transport, Fingerprint: "expected"}, Identity: "expected"}).Start(bg, "code", CodemodeStart{RuntimeIdentity: "expected", ParallelCalls: true, DrainAcknowledgements: true})
			if err != nil {
				t.Fatal(err)
			}
			defer vm.Close()
			ctx, cancel := context.WithTimeout(bg, 3*time.Second)
			defer cancel()
			event, err := vm.Next(ctx)
			if err == nil || event.Result != nil {
				t.Fatal("invalid transport accepted", event, err)
			}
			if kind == "terminal-transport" && !errors.Is(err, cause) {
				t.Fatal(err)
			}
		})
	}
}
