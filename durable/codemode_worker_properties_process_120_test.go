package durable

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestCodemode120OfficialWorkerRejectionPropertiesReplay(t *testing.T) {
	root := os.Getenv("GOAI_WORKER_PACKAGE_ROOT")
	if root == "" {
		t.Skip("opt-in: exact pi-codemode@1.2.0 install required")
	}
	// Repeat the identical start and journal delivery. The caught value and VM
	// clock must agree independently of the process's real time.
	code := `try{await tools.leaf({});throw Error('resolved')}catch(e){if(e.message!=='failed'||e.structuredOutput.reason!=='detail')throw Error('lost properties');return {caught:e.structuredOutput,clock:Date.now()}}`
	delivery := CodemodeDelivery{Seq: 1, Now: 1234, Call: CodemodeCall{ID: 1, Name: "leaf", Arguments: JSON{}}, Error: "failed", Properties: JSON{"structuredOutput": JSON{"reason": "detail"}, "message": "forged", "name": "Forged"}}
	var previous any
	for attempt := 0; attempt < 2; attempt++ {
		observed := make(chan int)
		sandbox := &RemoteCodemodeSandbox{Remote: &CodemodeWorkerRemote{Transport: officialWorkerTransport120{root, observed}, Fingerprint: officialWorkerFingerprint120}, Identity: officialWorkerFingerprint120}
		ctx, cancel := context.WithTimeout(bg, 10*time.Second)
		vm, err := sandbox.Start(ctx, code, CodemodeStart{RuntimeIdentity: officialWorkerFingerprint120, ParallelCalls: true, DrainAcknowledgements: true, StartedAt: 1000, Seed: 7, MemoryLimitBytes: DefaultCodemodeMemoryLimitBytes, InterruptBudget: DefaultCodemodeInterruptBudget, Tools: []CodemodeToolDescription{{Name: "leaf", Identifier: "leaf"}}})
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		event, err := vm.Next(ctx)
		if err != nil || event.Call == nil || !equalJSONValue(event.Call.Arguments, delivery.Call.Arguments) {
			_ = vm.Close()
			cancel()
			t.Fatal(event, err)
		}
		observed <- 1
		event, err = vm.Next(ctx)
		if err != nil || event.Drain == nil || event.Drain.Issued != 1 {
			_ = vm.Close()
			cancel()
			t.Fatal(event, err)
		}
		if err = vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *event.Drain); err != nil {
			t.Fatal(err)
		}
		if err = vm.Deliver(ctx, delivery); err != nil {
			t.Fatal(err)
		}
		event, err = vm.Next(ctx)
		if err != nil || event.Drain == nil || event.Drain.DeliveryID != 1 {
			_ = vm.Close()
			cancel()
			t.Fatal(event, err)
		}
		if err = vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *event.Drain); err != nil {
			t.Fatal(err)
		}
		event, err = vm.Next(ctx)
		if err != nil || event.Result == nil || event.Result.Error != "" || !equalJSONValue(event.Result.Value, JSON{"caught": JSON{"reason": "detail"}, "clock": 1234}) {
			_ = vm.Close()
			cancel()
			t.Fatal(event, err)
		}
		if attempt > 0 && !equalJSONValue(previous, event.Result.Value) {
			t.Fatal("worker replay diverged")
		}
		previous = event.Result.Value
		if err = vm.Close(); err != nil {
			t.Fatal(err)
		}
		cancel()
	}
}
