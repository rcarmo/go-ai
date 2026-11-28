package durable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/rcarmo/go-ai/durable"
)

const deterministicStartedAtV170 int64 = 1768471200000

func startDeterministicV170(t *testing.T, sandbox *durable.QuickJSSandbox, source string, seed uint64) durable.CodemodeExecution {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	vm, err := sandbox.Start(ctx, source, durable.CodemodeStart{
		StartedAt:     deterministicStartedAtV170,
		Seed:          seed,
		ParallelCalls: true,
		Tools: []durable.CodemodeToolDescription{
			{Name: "echo", Identifier: "echo"},
			{Name: "wait", Identifier: "wait"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := vm.Close(); err != nil {
			t.Errorf("close deterministic execution: %v", err)
		}
	})
	return vm
}

func nextDeterministicV170(t *testing.T, vm durable.CodemodeExecution) durable.CodemodeEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	event, err := vm.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func deliverDeterministicV170(t *testing.T, vm durable.CodemodeExecution, seq int, now int64, call durable.CodemodeCall, value any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := vm.Deliver(ctx, durable.CodemodeDelivery{
		Seq: seq, Now: now, Call: call, HasValue: true, Value: value,
	}); err != nil {
		t.Fatal(err)
	}
}

func resultDeterministicV170(t *testing.T, event durable.CodemodeEvent) durable.CodemodeResult {
	t.Helper()
	if event.Result == nil || event.Result.Error != "" {
		t.Fatalf("unexpected result event: %+v", event)
	}
	return *event.Result
}

func jsonValueV170(t *testing.T, value any) any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestPublicCodemodeV170DeterministicSeedAndClockBoundaries(t *testing.T) {
	sandbox, err := durable.NewQuickJSSandbox(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(context.Background())

	const source = `
		const initial = [Date.now(), performance.now(), Math.random(), Math.random(), new Date().toISOString()];
		const answered = await tools.echo({value: initial});
		return [initial, Date.now(), performance.now(), Math.random(), answered];
	`
	first := startDeterministicV170(t, sandbox, source, 1)
	callEvent := nextDeterministicV170(t, first)
	if callEvent.Call == nil {
		t.Fatalf("missing first call: %+v", callEvent)
	}
	call := *callEvent.Call
	got, ok := jsonValueV170(t, call.Arguments["value"]).([]any)
	if !ok || len(got) != 5 {
		t.Fatalf("initial deterministic values: %#v", call.Arguments["value"])
	}
	if got[0] != json.Number("1768471200000") || got[1] != json.Number("0") ||
		got[2] != json.Number("0.34227450605335275") || got[4] != "2026-01-15T10:00:00.000Z" {
		t.Fatalf("initial deterministic values: %#v", got)
	}

	deliverDeterministicV170(t, first, 1, deterministicStartedAtV170+1000, call, "ok")
	result := resultDeterministicV170(t, nextDeterministicV170(t, first))
	values, ok := result.Value.([]any)
	if !ok || len(values) != 5 {
		t.Fatalf("unexpected result: %#v", result.Value)
	}
	if values[1] != json.Number("1768471201000") || values[2] != json.Number("1000") {
		t.Fatalf("delivery clock: %#v", values)
	}

	replay := startDeterministicV170(t, sandbox, source, 1)
	replayCallEvent := nextDeterministicV170(t, replay)
	if replayCallEvent.Call == nil || !reflect.DeepEqual(jsonValueV170(t, replayCallEvent.Call.Arguments), jsonValueV170(t, call.Arguments)) {
		t.Fatalf("replay call diverged: %+v %+v", call, replayCallEvent.Call)
	}
	deliverDeterministicV170(t, replay, 1, deterministicStartedAtV170+1000, *replayCallEvent.Call, "ok")
	replayResult := resultDeterministicV170(t, nextDeterministicV170(t, replay))
	if !reflect.DeepEqual(jsonValueV170(t, result.Value), jsonValueV170(t, replayResult.Value)) {
		t.Fatalf("replay result diverged: %#v %#v", result.Value, replayResult.Value)
	}

	// A lagging host delivery is admitted; the VM clock never rewinds.
	lagging := startDeterministicV170(t, sandbox, `await tools.wait({}); return [Date.now(), performance.now()]`, 1)
	laggingCall := nextDeterministicV170(t, lagging)
	if laggingCall.Call == nil {
		t.Fatalf("missing lagging-clock call: %+v", laggingCall)
	}
	deliverDeterministicV170(t, lagging, 1, deterministicStartedAtV170-2000, *laggingCall.Call, "ok")
	laggingResult := resultDeterministicV170(t, nextDeterministicV170(t, lagging))
	if !reflect.DeepEqual(jsonValueV170(t, laggingResult.Value), jsonValueV170(t, []any{deterministicStartedAtV170, 0})) {
		t.Fatalf("lagging clock rewound: %#v", laggingResult.Value)
	}

	zero := startDeterministicV170(t, sandbox, `return [Math.random(), Math.random()]`, 0)
	zeroResult := resultDeterministicV170(t, nextDeterministicV170(t, zero))
	if !reflect.DeepEqual(jsonValueV170(t, zeroResult.Value), jsonValueV170(t, []float64{
		0.6711372530266764, 0.7258461452833667,
	})) {
		t.Fatalf("seed zero: %#v", zeroResult.Value)
	}
}

func TestPublicCodemodeV170DeterministicCallOrderBoundaries(t *testing.T) {
	sandbox, err := durable.NewQuickJSSandbox(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(context.Background())

	vm := startDeterministicV170(t, sandbox, `
		const order = [];
		await Promise.all([
			tools.wait({value: "a"}).then(() => order.push("a")),
			tools.wait({value: "b"}).then(() => order.push("b")),
		]);
		return order;
	`, 1)
	first := nextDeterministicV170(t, vm)
	second := nextDeterministicV170(t, vm)
	if first.Call == nil || second.Call == nil {
		t.Fatalf("missing issued calls: %+v %+v", first, second)
	}
	if first.Call.Arguments["value"] != "a" || second.Call.Arguments["value"] != "b" {
		t.Fatalf("unexpected call order: %+v %+v", first.Call, second.Call)
	}

	// b is delivery 1 and a is delivery 2. Deliver seq 2 first: admission is
	// independent of transport arrival, then the VM applies both in seq order.
	deliverDeterministicV170(t, vm, 2, deterministicStartedAtV170+2000, *first.Call, nil)
	deliverDeterministicV170(t, vm, 1, deterministicStartedAtV170+1000, *second.Call, nil)
	result := resultDeterministicV170(t, nextDeterministicV170(t, vm))
	if !reflect.DeepEqual(jsonValueV170(t, result.Value), jsonValueV170(t, []string{"b", "a"})) {
		t.Fatalf("application order: %#v", result.Value)
	}
}
