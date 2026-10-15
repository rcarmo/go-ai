package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const officialWorkerFingerprint120 = "quickjs-wasi 3.6.2, quickjs 0.17.0, prelude 8c6c9242, deterministic 1, stack 262144, heap 33554432, budget 100000"

// This opt-in transport fixture requires an exact official npm installation in
// the project-owned run directory. The ordinary suite needs no Node/network.
// Admission gates keep concurrently waiting RPC callbacks in wire order.
type officialWorkerTransport120 struct {
	root     string
	observed <-chan int
}

func (p officialWorkerTransport120) Execute(ctx context.Context, request CodemodeWorkerRequest, exchange func(context.Context, []CodemodeWorkerMessage) (*CodemodeWorkerReply, error)) error {
	linked, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(linked, "node", filepath.Join("testdata", "worker-remote-120.mjs"))
	cmd.Env = append(os.Environ(), "GOAI_WORKER_PACKAGE_ROOT="+p.root)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	input, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	var writes sync.Mutex
	encoder := json.NewEncoder(input)
	if err = encoder.Encode(struct {
		Request CodemodeWorkerRequest `json:"request"`
	}{request}); err != nil {
		cancel()
		_ = cmd.Wait()
		return err
	}
	var callbacks sync.WaitGroup
	failures := make(chan error, 32)
	decoder := json.NewDecoder(output)
	settled := false
	for {
		var frame struct {
			ID       int                     `json:"id"`
			Messages []CodemodeWorkerMessage `json:"messages"`
			Settled  bool                    `json:"settled"`
		}
		if err = decoder.Decode(&frame); err != nil {
			break
		}
		if frame.Settled {
			settled = true
			break
		}
		callbacks.Add(1)
		go func(id int, messages []CodemodeWorkerMessage) {
			defer callbacks.Done()
			reply, err := exchange(linked, messages)
			if err != nil {
				failures <- err
				cancel()
				return
			}
			writes.Lock()
			err = encoder.Encode(struct {
				ID    int                  `json:"id"`
				Reply *CodemodeWorkerReply `json:"reply"`
			}{id, reply})
			writes.Unlock()
			if err != nil {
				failures <- err
				cancel()
			}
		}(frame.ID, frame.Messages)
		hasCall := false
		for _, message := range frame.Messages {
			if message.Type == "call" {
				hasCall = true
			}
		}
		if hasCall {
			select {
			case <-p.observed:
			case <-linked.Done():
			}
		}
	}
	if !settled {
		cancel()
	}
	_ = input.Close()
	waitErr := cmd.Wait()
	callbacks.Wait()
	select {
	case failure := <-failures:
		return failure
	default:
	}
	if waitErr != nil {
		return fmt.Errorf("official worker: %w: %s", waitErr, stderr.String())
	}
	if !settled {
		return fmt.Errorf("official worker did not settle: %w", err)
	}
	return nil
}

func TestCodemode120OfficialWorkerVMIntegration(t *testing.T) {
	root := os.Getenv("GOAI_WORKER_PACKAGE_ROOT")
	if root == "" {
		t.Skip("opt-in: exact pi-codemode@1.3.0 install required")
	}
	data, err := os.ReadFile(filepath.Join(root, "node_modules", "@earendil-works", "pi-codemode", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &metadata) != nil || metadata.Version != "1.3.0" {
		t.Fatal("wrong official package")
	}
	observed := make(chan int)
	sandbox := &RemoteCodemodeSandbox{Remote: &CodemodeWorkerRemote{Transport: officialWorkerTransport120{root, observed}, Fingerprint: officialWorkerFingerprint120}, Identity: officialWorkerFingerprint120}
	ctx, cancel := context.WithTimeout(bg, 15*time.Second)
	defer cancel()
	code := `text('before');const a=tools.leaf({n:1});const b=tools.leaf({n:2});const values=await Promise.all([a,b]);text(values.map(x=>x.n).join(','));store('saved',values);return values;`
	vm, err := sandbox.Start(ctx, code, CodemodeStart{RuntimeIdentity: officialWorkerFingerprint120, ParallelCalls: true, DrainAcknowledgements: true, MemoryLimitBytes: DefaultCodemodeMemoryLimitBytes, InterruptBudget: DefaultCodemodeInterruptBudget, Seed: 7, Tools: []CodemodeToolDescription{{Name: "leaf", Identifier: "leaf"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	calls := map[int]CodemodeCall{}
	for id := 1; id <= 2; id++ {
		event, err := vm.Next(ctx)
		if err != nil || event.Call == nil || event.Call.ID != id {
			t.Fatal(event, err)
		}
		calls[id] = *event.Call
		observed <- id
	}
	event, err := vm.Next(ctx)
	if err != nil || event.Drain == nil || event.Drain.DeliveryID != 0 || event.Drain.Issued != 2 {
		t.Fatal(event, err)
	}
	if err = vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *event.Drain); err != nil {
		t.Fatal(err)
	}
	for seq, id := range []int{2, 1} {
		if err = vm.Deliver(ctx, CodemodeDelivery{Seq: seq + 1, Call: calls[id], Now: int64(seq + 10), HasValue: true, Value: calls[id].Arguments}); err != nil {
			t.Fatal(err)
		}
		event, err = vm.Next(ctx)
		if err != nil || event.Drain == nil || event.Drain.DeliveryID != id {
			t.Fatal(event, err)
		}
		if err = vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *event.Drain); err != nil {
			t.Fatal(err)
		}
	}
	event, err = vm.Next(ctx)
	if err != nil || event.Result == nil {
		t.Fatal(event, err)
	}
	if event.Result.Error != "" || !event.Result.HasValue || !equalJSONValue(event.Result.Value, []any{JSON{"n": 1}, JSON{"n": 2}}) || !equalJSONValue(event.Result.StoreWrites, JSON{"saved": []any{JSON{"n": 1}, JSON{"n": 2}}}) || !strings.Contains(event.Result.Output, "==> text 1/3 <==\nbefore") || !strings.Contains(event.Result.Output, "==> text 2/3 <==\n1,2") || !strings.Contains(event.Result.Output, "==> text 3/3 <==\n[") {
		t.Fatal(event.Result)
	}
	if err = vm.Close(); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestCodemode120OfficialWorkerTerminalAndConsole(t *testing.T) {
	root := os.Getenv("GOAI_WORKER_PACKAGE_ROOT")
	if root == "" {
		t.Skip("opt-in: exact pi-codemode@1.3.0 install required")
	}
	for _, script := range []struct {
		name, code string
		failed     bool
	}{
		{"console", `console.log('console-line');text('text-line');return "returned";`, false},
		{"script-error", `text('before-error');throw new Error('expected failure');`, true},
	} {
		t.Run(script.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(bg, 10*time.Second)
			defer cancel()
			vm, err := (&RemoteCodemodeSandbox{Remote: &CodemodeWorkerRemote{Transport: officialWorkerTransport120{root, nil}, Fingerprint: officialWorkerFingerprint120}, Identity: officialWorkerFingerprint120}).Start(ctx, script.code, CodemodeStart{RuntimeIdentity: officialWorkerFingerprint120, ParallelCalls: true, DrainAcknowledgements: true, MemoryLimitBytes: DefaultCodemodeMemoryLimitBytes, InterruptBudget: DefaultCodemodeInterruptBudget, Seed: 7})
			if err != nil {
				t.Fatal(err)
			}
			defer vm.Close()
			event, err := vm.Next(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !script.failed {
				if event.Drain == nil {
					t.Fatal("missing terminal drain", event)
				}
				if err = vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *event.Drain); err != nil {
					t.Fatal(err)
				}
				event, err = vm.Next(ctx)
			}
			if err != nil || event.Result == nil {
				t.Fatal(event, err)
			}
			result := event.Result
			if script.failed {
				if !strings.Contains(result.Error, "expected failure") || !strings.Contains(result.Output, "Script error:\nError: expected failure") || !strings.Contains(result.Output, "before-error") || len(result.StoreWrites) != 0 {
					t.Fatal(result)
				}
			} else {
				if result.Error != "" || !strings.Contains(result.Output, "<console_output>\nconsole-line\n</console_output>") || !strings.Contains(result.Output, "==> text 2/2 <==\nreturned") {
					t.Fatal(result)
				}
			}
		})
	}
}
