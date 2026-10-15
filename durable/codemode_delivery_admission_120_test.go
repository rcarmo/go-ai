package durable

import (
	"context"
	"testing"
	"time"
)

func TestCodemode120QuickJSRejectsStaleMalformedAndDuplicateDelivery(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	vm, err := sandbox.Start(ctx, `await tools.leaf({n:1});await tools.leaf({n:2});return Date.now();`, CodemodeStart{StartedAt: 1000, Seed: 1, Tools: []CodemodeToolDescription{{Name: "leaf", Identifier: "leaf"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	first, err := vm.Next(ctx)
	if err != nil || first.Call == nil {
		t.Fatal(first, err)
	}
	valid := CodemodeDelivery{Seq: 1, Call: *first.Call, Now: 2000, HasValue: true, Value: "ok"}
	for _, change := range []func(*CodemodeDelivery){func(d *CodemodeDelivery) { d.Seq = 2 }, func(d *CodemodeDelivery) { d.Call.Name = "other" }, func(d *CodemodeDelivery) { d.Call.Global = true }, func(d *CodemodeDelivery) { d.Call.Arguments = JSON{"n": 99} }, func(d *CodemodeDelivery) { d.Now = -1 }, func(d *CodemodeDelivery) { d.Error = "failed" }, func(d *CodemodeDelivery) { d.HasValue = false }} {
		bad := valid
		change(&bad)
		if err = vm.Deliver(ctx, bad); err == nil {
			t.Fatal("malformed delivery accepted", bad)
		}
	}
	if err = vm.Deliver(ctx, valid); err != nil {
		t.Fatal(err)
	}
	second, err := vm.Next(ctx)
	if err != nil || second.Call == nil {
		t.Fatal(second, err)
	}
	if err = vm.Deliver(ctx, valid); err == nil {
		t.Fatal("stale delivery accepted")
	}
	if err = vm.Deliver(ctx, CodemodeDelivery{Seq: 2, Call: *second.Call, Now: 3000, HasValue: true, Value: nil}); err != nil {
		t.Fatal(err)
	}
	done, err := vm.Next(ctx)
	if err != nil || done.Result == nil || done.Result.Error != "" || !equalJSONValue(done.Result.Value, 3000) {
		t.Fatal(done, err)
	}
}
