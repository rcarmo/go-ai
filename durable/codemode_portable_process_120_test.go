package durable

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodemode120OfficialPortableOutputBatching(t *testing.T) {
	root := os.Getenv("GOAI_WORKER_PACKAGE_ROOT")
	if root == "" {
		t.Skip("opt-in: exact pi-codemode@1.2.0 install required")
	}
	var exchanges, items atomic.Int64
	transport := workerTransport120(func(ctx context.Context, request CodemodeWorkerRequest, exchange func(context.Context, []CodemodeWorkerMessage) (*CodemodeWorkerReply, error)) error {
		return (officialWorkerTransport120{root, nil}).Execute(ctx, request, func(ctx context.Context, batch []CodemodeWorkerMessage) (*CodemodeWorkerReply, error) {
			exchanges.Add(1)
			for _, message := range batch {
				if message.Type == "output" {
					items.Add(1)
				}
			}
			return exchange(ctx, batch)
		})
	})
	ctx, cancel := context.WithTimeout(bg, 15*time.Second)
	defer cancel()
	vm, err := (&RemoteCodemodeSandbox{Remote: &CodemodeWorkerRemote{Transport: transport, Fingerprint: officialWorkerFingerprint120}, Identity: officialWorkerFingerprint120}).Start(ctx, `for(let i=0;i<5000;i++)text(i);`, CodemodeStart{RuntimeIdentity: officialWorkerFingerprint120, ParallelCalls: true, DrainAcknowledgements: true, MemoryLimitBytes: DefaultCodemodeMemoryLimitBytes, InterruptBudget: DefaultCodemodeInterruptBudget})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	event, err := vm.Next(ctx)
	if err != nil || event.Drain == nil {
		t.Fatal(event, err)
	}
	if err = vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *event.Drain); err != nil {
		t.Fatal(err)
	}
	event, err = vm.Next(ctx)
	if err != nil || event.Result == nil || event.Result.Error != "" {
		t.Fatal(event, err)
	}
	if exchanges.Load() > 6 || items.Load() != 5000 || !strings.Contains(event.Result.Output, "==> text 5000/5000 <==\n4999") {
		t.Fatalf("batching lost output or made per-line RPCs: exchanges=%d items=%d hasValue=%t value=%v output-tail=%q", exchanges.Load(), items.Load(), event.Result.HasValue, event.Result.Value, event.Result.Output[max(0, len(event.Result.Output)-160):])
	}
}

func TestCodemode120OfficialPortableServerResourceCaps(t *testing.T) {
	root := os.Getenv("GOAI_WORKER_PACKAGE_ROOT")
	if root == "" {
		t.Skip("opt-in: exact pi-codemode@1.2.0 install required")
	}
	t.Setenv("GOAI_WORKER_MAX_BUDGET", "1000")
	t.Setenv("GOAI_WORKER_MAX_MEMORY", "8388608")
	fingerprint := "quickjs-wasi 3.6.2, quickjs 0.17.0, prelude 8c6c9242, deterministic 1, stack 262144, heap 8388608, budget 1000"
	for _, code := range []string{`while(true){}`, `const a=[];while(true)a.push(new Array(10000).fill(1));`} {
		ctx, cancel := context.WithTimeout(bg, 15*time.Second)
		vm, err := (&RemoteCodemodeSandbox{Remote: &CodemodeWorkerRemote{Transport: officialWorkerTransport120{root, nil}, Fingerprint: fingerprint}, Identity: fingerprint}).Start(ctx, code, CodemodeStart{RuntimeIdentity: fingerprint, ParallelCalls: true, DrainAcknowledgements: true, MemoryLimitBytes: 128 << 20, InterruptBudget: 1000000})
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		event, err := vm.Next(ctx)
		if err != nil || event.Result == nil || event.Result.Error == "" {
			_ = vm.Close()
			cancel()
			t.Fatal(event, err)
		}
		if !strings.Contains(event.Result.Error, "interrupt") && !strings.Contains(event.Result.Error, "memory") {
			t.Fatal("unexpected cap failure", event.Result.Error)
		}
		if err = vm.Close(); err != nil {
			t.Fatal(err)
		}
		cancel()
	}
}

func TestCodemode120OfficialPortableComputingAbortJoins(t *testing.T) {
	root := os.Getenv("GOAI_WORKER_PACKAGE_ROOT")
	if root == "" {
		t.Skip("opt-in: exact pi-codemode@1.2.0 install required")
	}
	started := make(chan struct{})
	joined := make(chan struct{})
	transport := workerTransport120(func(ctx context.Context, request CodemodeWorkerRequest, exchange func(context.Context, []CodemodeWorkerMessage) (*CodemodeWorkerReply, error)) error {
		defer close(joined)
		return (officialWorkerTransport120{root, nil}).Execute(ctx, request, func(ctx context.Context, batch []CodemodeWorkerMessage) (*CodemodeWorkerReply, error) {
			for _, message := range batch {
				if message.Type == "started" {
					close(started)
				}
			}
			return exchange(ctx, batch)
		})
	})
	vm, err := (&RemoteCodemodeSandbox{Remote: &CodemodeWorkerRemote{Transport: transport, Fingerprint: officialWorkerFingerprint120}, Identity: officialWorkerFingerprint120}).Start(bg, `for(let i=0;i<1000;i++)text(i);while(true){}`, CodemodeStart{RuntimeIdentity: officialWorkerFingerprint120, ParallelCalls: true, DrainAcknowledgements: true, MemoryLimitBytes: DefaultCodemodeMemoryLimitBytes, InterruptBudget: DefaultCodemodeInterruptBudget})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("official VM never started")
	}
	if err = vm.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-joined:
	default:
		t.Fatal("close returned before remote process joined")
	}
}
