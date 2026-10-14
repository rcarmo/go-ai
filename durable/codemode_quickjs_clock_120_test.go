package durable

import (
	"context"
	"testing"
	"time"
)

func TestCodemode120QuickJSClockAdvancesOnlyOnDelivery(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	vm, err := sandbox.Start(ctx, `const before=Date.now();await tools.leaf({});return [before,Date.now()];`, CodemodeStart{StartedAt: 1000, Seed: 42, Tools: []CodemodeToolDescription{{Name: "leaf", Identifier: "leaf"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	event, err := vm.Next(ctx)
	if err != nil || event.Call == nil {
		t.Fatal(event, err)
	}
	if err = vm.Deliver(ctx, CodemodeDelivery{Seq: 1, Now: 2000, Call: *event.Call, HasValue: true, Value: "ok"}); err != nil {
		t.Fatal(err)
	}
	event, err = vm.Next(ctx)
	if err != nil || event.Result == nil || event.Result.Error != "" {
		t.Fatal(event, err)
	}
	values, ok := event.Result.Value.([]any)
	if !ok || len(values) != 2 || !equalJSONValue(values[0], 1000) || !equalJSONValue(values[1], 2000) {
		t.Fatal(event.Result)
	}
}
