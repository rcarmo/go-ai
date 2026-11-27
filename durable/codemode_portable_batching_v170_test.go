package durable

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Source: pi v1.7.0 packages/codemode/test/portable.test.ts:83–91.
func TestSourceV170CodemodePublishedPortableOutputBatching(t *testing.T) {
	root := officialWorkerPackageRoot(t, "GOAI_CODEMODE_V170_PACKAGE_ROOT", "1.7.0")
	var mu sync.Mutex
	var exchanges int
	var items []CodemodeWorkerOutput
	transport := workerTransport120(func(ctx context.Context, request CodemodeWorkerRequest, exchange func(context.Context, []CodemodeWorkerMessage) (*CodemodeWorkerReply, error)) error {
		return (officialWorkerTransport120{root: root}).Execute(ctx, request, func(ctx context.Context, batch []CodemodeWorkerMessage) (*CodemodeWorkerReply, error) {
			mu.Lock()
			exchanges++
			for _, message := range batch {
				if message.Type == "output" && message.Item != nil {
					items = append(items, *message.Item)
				}
			}
			mu.Unlock()
			return exchange(ctx, batch)
		})
	})
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	// The v1.7.0 package has the same compiled guest files and fingerprint
	// as v1.6.1. Pin it before starting the subprocess, rather than learning it.
	const fingerprint = officialWorkerFingerprintV160
	sandbox := &RemoteCodemodeSandbox{
		Remote: &CodemodeWorkerRemote{Transport: transport, Fingerprint: fingerprint}, Identity: fingerprint,
	}
	const source = "for (let i = 0; i < 5000; i++) console.log(i); return 'ok'"
	vm, err := sandbox.Start(ctx, source, CodemodeStart{
		RuntimeIdentity: fingerprint, ParallelCalls: true, DrainAcknowledgements: true,
		MemoryLimitBytes: DefaultCodemodeMemoryLimitBytes, InterruptBudget: DefaultCodemodeInterruptBudget,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	final, err := vm.Next(ctx)
	if err != nil || final.Drain == nil || *final.Drain != (CodemodeDrain{Final: true}) || final.Result != nil || final.Call != nil {
		t.Fatalf("drain event = %+v, %v; want a final drain with no issued calls", final, err)
	}
	if err := vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *final.Drain); err != nil {
		t.Fatal(err)
	}
	event, err := vm.Next(ctx)
	if err != nil || event.Result == nil || event.Drain != nil || event.Call != nil || event.Result.Error != "" || !event.Result.HasValue || event.Result.Value != "ok" {
		t.Fatalf("terminal event = %+v, %v; want successful 'ok'", event, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(items) != 5000 || exchanges < 1 || exchanges > 6 {
		t.Fatalf("worker produced %d items in %d exchanges; want 5000 in at most six", len(items), exchanges)
	}
	var output strings.Builder
	output.WriteString("ok\n<console_output>")
	for i, item := range items {
		line := strconv.Itoa(i)
		want := CodemodeWorkerOutput{Type: "text", Text: line, Console: true}
		if item != want {
			t.Fatalf("worker item %d = %+v; want %+v", i, item, want)
		}
		output.WriteByte('\n')
		output.WriteString(line)
	}
	output.WriteString("\n</console_output>")
	if event.Result.Output != output.String() {
		t.Errorf("Go output projection differs from the exact console block (%d bytes)", len(event.Result.Output))
	}
	t.Logf("5000 ordered console items in %d exchanges", exchanges)
}
