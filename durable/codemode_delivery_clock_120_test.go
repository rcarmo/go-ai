package durable

import (
	"context"
	"testing"
	"time"
)

func TestCodemode120DeliveryClockRejectDoesNotConsumeCall(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	for _, remote := range []bool{false, true} {
		for _, parallel := range []bool{false, true} {
			name := "local"
			if remote {
				name = "remote"
			}
			if parallel {
				name += "-parallel"
			} else {
				name += "-serial"
			}
			if remote && !parallel {
				continue
			}
			t.Run(name, func(t *testing.T) {
				local := *engine
				local.ParallelCalls = parallel
				var sandbox CodemodeSandbox = &local
				if remote {
					sandbox = &RemoteCodemodeSandbox{Remote: &CodemodeRemoteServer{Sandbox: &local}, Identity: local.RuntimeIdentity()}
				}
				ctx, cancel := context.WithTimeout(bg, 5*time.Second)
				defer cancel()
				vm, err := sandbox.Start(ctx, `const a=await tools.leaf({});const b=await tools.leaf({});return Date.now();`, CodemodeStart{RuntimeIdentity: local.RuntimeIdentity(), StartedAt: 100, Seed: 7, ParallelCalls: parallel, DrainAcknowledgements: parallel, Tools: []CodemodeToolDescription{{Name: "leaf", Identifier: "leaf"}}})
				if err != nil {
					t.Fatal(err)
				}
				defer vm.Close()
				nextCall := func() CodemodeCall {
					t.Helper()
					event, err := vm.Next(ctx)
					if err != nil || event.Call == nil {
						t.Fatal(event, err)
					}
					if parallel {
						drain, err := vm.Next(ctx)
						if err != nil || drain.Drain == nil {
							t.Fatal(drain, err)
						}
						if err = vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *drain.Drain); err != nil {
							t.Fatal(err)
						}
					}
					return *event.Call
				}
				call := nextCall()
				delivery := CodemodeDelivery{Seq: 1, Call: call, Now: 99, HasValue: true, Value: true}
				if err = vm.Deliver(ctx, delivery); err == nil {
					t.Fatal("backwards first clock accepted")
				}
				delivery.Now = 200
				cycle := JSON{}
				cycle["self"] = cycle
				delivery.Value = cycle
				if err = vm.Deliver(ctx, delivery); err == nil {
					t.Fatal("cyclic answer accepted")
				}
				delivery.Value = true
				delivery.Properties = JSON{"structuredOutput": true}
				if err = vm.Deliver(ctx, delivery); err == nil {
					t.Fatal("success with rejection properties accepted")
				}
				delivery.Properties = nil
				if err = vm.Deliver(ctx, delivery); err != nil {
					t.Fatal("rejected answer consumed call", err)
				}
				call = nextCall()
				delivery = CodemodeDelivery{Seq: 2, Call: call, Now: 199, HasValue: true, Value: true}
				if err = vm.Deliver(ctx, delivery); err == nil {
					t.Fatal("backwards second clock accepted")
				}
				delivery.Now = int64(MaxID) + 1
				if err = vm.Deliver(ctx, delivery); err == nil {
					t.Fatal("unsafe clock accepted")
				}
				delivery.Now = 200
				if err = vm.Deliver(ctx, delivery); err != nil {
					t.Fatal("valid retry rejected", err)
				}
				if parallel {
					event, err := vm.Next(ctx)
					if err != nil || event.Drain == nil {
						t.Fatal(event, err)
					}
					if err = vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *event.Drain); err != nil {
						t.Fatal(err)
					}
				}
				event, err := vm.Next(ctx)
				if err != nil || event.Result == nil || !equalJSONValue(event.Result.Value, 200) {
					t.Fatal(event, err)
				}
			})
		}
	}
}
