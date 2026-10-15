package durable

import (
	"context"
	"testing"
	"time"
)

func TestCodemode120QuickJSDrainAdmission(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	vm, err := sandbox.Start(ctx, `await tools.leaf({});return "done";`, CodemodeStart{DrainAcknowledgements: true, Tools: []CodemodeToolDescription{{Name: "leaf"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	ack := vm.(CodemodeDrainExecution)
	var call *CodemodeCall
	var drain *CodemodeDrain
	for i := 0; i < 2; i++ {
		event, err := vm.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if event.Call != nil {
			call = event.Call
		}
		if event.Drain != nil {
			drain = event.Drain
		}
	}
	if call == nil || drain == nil || *drain != (CodemodeDrain{Issued: 1}) {
		t.Fatal(call, drain)
	}
	delivery := CodemodeDelivery{Seq: 1, Call: *call, HasValue: true, Value: "ok"}
	if err := vm.Deliver(ctx, delivery); err == nil {
		t.Fatal("unacknowledged delivery accepted")
	}
	original := *drain
	drain.Issued = 999
	if err := ack.AcknowledgeDrain(ctx, *drain); err == nil {
		t.Fatal("mutated drain accepted")
	}
	if err := ack.AcknowledgeDrain(ctx, original); err != nil {
		t.Fatal(err)
	}
	if err := ack.AcknowledgeDrain(ctx, original); err == nil {
		t.Fatal("duplicate acknowledgement accepted")
	}
	if err := vm.Deliver(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	event, err := vm.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if event.Drain == nil || *event.Drain != (CodemodeDrain{DeliveryID: 1, Issued: 1}) {
		t.Fatal(event)
	}
	if err := ack.AcknowledgeDrain(ctx, *event.Drain); err != nil {
		t.Fatal(err)
	}
	event, err = vm.Next(ctx)
	if err != nil || event.Result == nil {
		t.Fatal(event, err)
	}
}

func TestCodemode120QuickJSDrainCloseJoins(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	vm, err := sandbox.Start(ctx, `return "done";`, CodemodeStart{DrainAcknowledgements: true})
	if err != nil {
		t.Fatal(err)
	}
	event, err := vm.Next(ctx)
	if err != nil || event.Drain == nil {
		t.Fatal(event, err)
	}
	if err := vm.Close(); err != nil {
		t.Fatal(err)
	}
	if err := vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *event.Drain); err == nil {
		t.Fatal("closed acknowledgement accepted")
	}
}
