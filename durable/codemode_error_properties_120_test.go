package durable

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCodemode120RemoteWorkerRejectionProperties(t *testing.T) {
	observed := make(chan struct{})
	transport := workerTransport120(func(ctx context.Context, _ CodemodeWorkerRequest, exchange func(context.Context, []CodemodeWorkerMessage) (*CodemodeWorkerReply, error)) error {
		if _, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "started", Fingerprint: "properties"}}); err != nil {
			return err
		}
		result := make(chan error, 1)
		go func() {
			reply, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "call", ID: 1, Target: "tool", Name: "leaf", Args: workerString120(`{}`)}})
			if err == nil && (reply == nil || reply.OK || reply.Properties == nil || *reply.Properties != `{"structuredOutput":{"reason":"detail"}}` || reply.Payload == nil || *reply.Payload != "failed") {
				err = reject("worker rejection properties lost")
			}
			result <- err
		}()
		select {
		case <-observed:
		case <-ctx.Done():
			return ctx.Err()
		}
		if _, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "idle", Seq: 0, Issued: 1}}); err != nil {
			return err
		}
		if err := <-result; err != nil {
			return err
		}
		_, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "done", OK: true, Value: workerString120(`"caught"`), Writes: workerString120(`[]`), During: &CodemodeWorkerDuring{Seq: 1, Issued: 1}}})
		return err
	})
	vm, err := (&RemoteCodemodeSandbox{Remote: &CodemodeWorkerRemote{Transport: transport, Fingerprint: "properties"}, Identity: "properties"}).Start(bg, "code", CodemodeStart{RuntimeIdentity: "properties", Tools: []CodemodeToolDescription{{Name: "leaf", Identifier: "leaf"}}, ParallelCalls: true, DrainAcknowledgements: true})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	event, err := vm.Next(ctx)
	if err != nil || event.Call == nil {
		t.Fatal(event, err)
	}
	call := *event.Call
	close(observed)
	event, err = vm.Next(ctx)
	if err != nil || event.Drain == nil {
		t.Fatal(event, err)
	}
	if err = vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *event.Drain); err != nil {
		t.Fatal(err)
	}
	if err = vm.Deliver(ctx, CodemodeDelivery{Call: call, Seq: 1, Error: "failed", Properties: JSON{"structuredOutput": JSON{"reason": "detail"}}}); err != nil {
		t.Fatal(err)
	}
	event, err = vm.Next(ctx)
	if err != nil || event.Drain == nil {
		t.Fatal(event, err)
	}
	if err = vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *event.Drain); err != nil {
		t.Fatal(err)
	}
	event, err = vm.Next(ctx)
	if err != nil || event.Result == nil || event.Result.Value != "caught" || !strings.Contains(event.Result.Output, "caught") {
		t.Fatal(event, err)
	}
}
