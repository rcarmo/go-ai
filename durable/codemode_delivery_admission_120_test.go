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

func TestCodemode120QuickJSCallEventCannotRewriteAdmission(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	vm, err := sandbox.Start(ctx, `const v=await tools.leaf({n:1});return v.n;`, CodemodeStart{StartedAt: 1000, Tools: []CodemodeToolDescription{{Name: "leaf", Identifier: "leaf"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	event, err := vm.Next(ctx)
	if err != nil || event.Call == nil {
		t.Fatal(event, err)
	}
	original := *event.Call
	original.Arguments = JSON{"n": 1}
	event.Call.Arguments["n"] = 99
	event.Call.Name = "changed"
	if err = vm.Deliver(ctx, CodemodeDelivery{Seq: 1, Now: 2000, Call: *event.Call, HasValue: true, Value: JSON{"n": 7}}); err == nil {
		t.Fatal("event mutation rewrote expected call")
	}
	value := JSON{"n": 7}
	if err = vm.Deliver(ctx, CodemodeDelivery{Seq: 1, Now: 2000, Call: original, HasValue: true, Value: value}); err != nil {
		t.Fatal(err)
	}
	value["n"] = 99
	done, err := vm.Next(ctx)
	if err != nil || done.Result == nil || done.Result.Error != "" || !equalJSONValue(done.Result.Value, 7) {
		t.Fatal(done, err)
	}
}

type deliveryMarshaler120 struct{ calls *int }

func (v deliveryMarshaler120) MarshalJSON() ([]byte, error) {
	*v.calls++
	return []byte(`"private"`), nil
}
func TestCodemode120QuickJSDeliveryRejectsMarshalAuthority(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	vm, err := sandbox.Start(ctx, `return await tools.leaf({});`, CodemodeStart{StartedAt: 1000, Tools: []CodemodeToolDescription{{Name: "leaf", Identifier: "leaf"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	event, err := vm.Next(ctx)
	if err != nil || event.Call == nil {
		t.Fatal(event, err)
	}
	calls := 0
	bad := CodemodeDelivery{Seq: 1, Now: 2000, Call: *event.Call, HasValue: true, Value: deliveryMarshaler120{&calls}}
	if err = vm.Deliver(ctx, bad); err == nil || calls != 0 {
		t.Fatal("delivery marshaler executed", err, calls)
	}
	valid := CodemodeDelivery{Seq: 1, Now: 2000, Call: *event.Call, HasValue: true, Value: "safe"}
	if err = vm.Deliver(ctx, valid); err != nil {
		t.Fatal(err)
	}
	done, err := vm.Next(ctx)
	if err != nil || done.Result == nil || done.Result.Value != "safe" {
		t.Fatal(done, err)
	}
}

func TestCodemode120QuickJSStartRejectsStoreMarshalAuthority(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	calls := 0
	execution, err := sandbox.Start(bg, `return load("key")`, CodemodeStart{Store: JSON{"key": deliveryMarshaler120{&calls}}})
	if err == nil || execution != nil || calls != 0 {
		t.Fatal("start marshaler invoked", err, calls)
	}
	var missing *QuickJSSandbox
	if execution, err = missing.Start(bg, `return 1`, CodemodeStart{RuntimeIdentity: "mismatch"}); err == nil || execution != nil {
		t.Fatal("nil sandbox accepted")
	}
	//lint:ignore SA1012 Deliberate nil-context admission rejection test.
	if execution, err = sandbox.Start(nil, `return 1`, CodemodeStart{}); err == nil || execution != nil {
		t.Fatal("nil context accepted")
	}
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	store := JSON{"key": JSON{"n": 1}}
	execution, err = sandbox.Start(ctx, `return load("key").n`, CodemodeStart{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer execution.Close()
	store["key"].(JSON)["n"] = 99
	result, err := execution.Next(ctx)
	if err != nil || result.Result == nil || !equalJSONValue(result.Result.Value, 1) {
		t.Fatal(result, err)
	}
}
