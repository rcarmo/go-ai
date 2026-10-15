package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// Test-only framing for the native interface. This is deliberately not a new
// production transport or a claim of TypeScript wire interoperability.
type remoteProcessFrame120 struct {
	Code     string
	Start    *CodemodeStart
	Event    *CodemodeEvent
	Reply    *CodemodeRemoteReply
	Delivery *CodemodeDelivery
	Error    string
}

func TestCodemode120RemoteProcessHelper(t *testing.T) {
	if os.Getenv("GOAI_REMOTE_PROCESS_TEST") != "1" {
		return
	}
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	decoder := json.NewDecoder(os.Stdin)
	var request remoteProcessFrame120
	if err := decoder.Decode(&request); err != nil {
		os.Exit(2)
	}
	encoder := json.NewEncoder(os.Stdout)
	var mu sync.Mutex
	send := func(frame remoteProcessFrame120) error { mu.Lock(); defer mu.Unlock(); return encoder.Encode(frame) }
	replies := make(chan CodemodeRemoteReply, 1)
	deliveries := make(chan CodemodeDelivery, 1)
	go func() {
		for {
			var frame remoteProcessFrame120
			if err := decoder.Decode(&frame); err != nil {
				cancel()
				return
			}
			if frame.Reply != nil {
				select {
				case replies <- *frame.Reply:
				case <-ctx.Done():
					return
				}
			} else if frame.Delivery != nil {
				select {
				case deliveries <- *frame.Delivery:
				case <-ctx.Done():
					return
				}
			} else {
				cancel()
				return
			}
		}
	}()
	sandbox, err := NewQuickJSSandbox(ctx)
	if err != nil {
		_ = send(remoteProcessFrame120{Error: err.Error()})
		os.Exit(2)
	}
	server := &CodemodeRemoteServer{Sandbox: sandbox}
	err = server.Execute(ctx, request.Code, *request.Start, func(ctx context.Context, event CodemodeEvent) (CodemodeRemoteReply, error) {
		if err := send(remoteProcessFrame120{Event: &event}); err != nil {
			return CodemodeRemoteReply{}, err
		}
		if event.Call != nil {
			return CodemodeRemoteReply{}, nil
		}
		if event.Result != nil {
			return CodemodeRemoteReply{Stop: true}, nil
		}
		select {
		case reply := <-replies:
			return reply, nil
		case <-ctx.Done():
			return CodemodeRemoteReply{}, ctx.Err()
		}
	}, func(ctx context.Context) (CodemodeDelivery, error) {
		select {
		case delivery := <-deliveries:
			return delivery, nil
		case <-ctx.Done():
			return CodemodeDelivery{}, ctx.Err()
		}
	})
	if err != nil {
		_ = send(remoteProcessFrame120{Error: err.Error()})
	}
	_ = sandbox.Close(bg)
	// Exit without testing's PASS output contaminating the framed stream.
	os.Exit(0)
}

type pipeRemote120 struct{}

func (pipeRemote120) Execute(ctx context.Context, code string, start CodemodeStart, exchange func(context.Context, CodemodeEvent) (CodemodeRemoteReply, error), receive func(context.Context) (CodemodeDelivery, error)) error {
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	linked, cancel := context.WithCancel(ctx)
	defer cancel()
	command := exec.CommandContext(linked, binary, "-test.run=^TestCodemode120RemoteProcessHelper$")
	command.Env = append(os.Environ(), "GOAI_REMOTE_PROCESS_TEST=1")
	input, err := command.StdinPipe()
	if err != nil {
		return err
	}
	output, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return err
	}
	var jobs sync.WaitGroup
	defer func() { cancel(); _ = input.Close(); jobs.Wait(); _ = command.Wait() }()
	encoder := json.NewEncoder(input)
	decoder := json.NewDecoder(output)
	var mu sync.Mutex
	send := func(frame remoteProcessFrame120) error { mu.Lock(); defer mu.Unlock(); return encoder.Encode(frame) }
	if err := send(remoteProcessFrame120{Code: code, Start: &start}); err != nil {
		return err
	}
	jobs.Add(1)
	go func() {
		defer jobs.Done()
		for {
			delivery, err := receive(linked)
			if err != nil {
				return
			}
			if err := send(remoteProcessFrame120{Delivery: &delivery}); err != nil {
				cancel()
				return
			}
		}
	}()
	for {
		var frame remoteProcessFrame120
		if err := decoder.Decode(&frame); err != nil {
			return err
		}
		if frame.Error != "" {
			return fmt.Errorf("remote helper: %s", frame.Error)
		}
		if frame.Event == nil {
			return fmt.Errorf("missing remote event")
		}
		reply, err := exchange(linked, *frame.Event)
		if err != nil {
			return err
		}
		if frame.Event.Result != nil {
			return nil
		}
		if frame.Event.Drain != nil {
			if err := send(remoteProcessFrame120{Reply: &reply}); err != nil {
				return err
			}
		}
	}
}

func TestCodemode120RemoteCrossProcessCallsAndClose(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	ctx, cancel := context.WithTimeout(bg, 15*time.Second)
	defer cancel()
	remote := &RemoteCodemodeSandbox{Remote: pipeRemote120{}, Identity: engine.RuntimeIdentity()}
	vm, err := remote.Start(ctx, `const seen=[];const a=tools.leaf({n:1}).then(v=>seen.push(v));const b=tools.leaf({n:2}).then(v=>seen.push(v));await Promise.all([a,b]);return seen;`, CodemodeStart{RuntimeIdentity: engine.RuntimeIdentity(), ParallelCalls: true, DrainAcknowledgements: true, InterruptBudget: 1000, Tools: []CodemodeToolDescription{{Name: "leaf"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	calls := map[int]CodemodeCall{}
	for i := 0; i < 2; i++ {
		event, err := vm.Next(ctx)
		if err != nil || event.Call == nil {
			t.Fatal(event, err)
		}
		calls[event.Call.ID] = *event.Call
	}
	event, err := vm.Next(ctx)
	if err != nil || event.Drain == nil || event.Drain.Issued != 2 {
		t.Fatal(event, err)
	}
	if err := vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *event.Drain); err != nil {
		t.Fatal(err)
	}
	for i, id := range []int{2, 1} {
		if err := vm.Deliver(ctx, CodemodeDelivery{Seq: i + 1, Call: calls[id], HasValue: true, Value: id}); err != nil {
			t.Fatal(err)
		}
		event, err = vm.Next(ctx)
		if err != nil || event.Drain == nil || event.Drain.DeliveryID != id {
			t.Fatal(event, err)
		}
		if err := vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *event.Drain); err != nil {
			t.Fatal(err)
		}
	}
	event, err = vm.Next(ctx)
	if err != nil || event.Result == nil || event.Result.Error != "" || !equalJSONValue(event.Result.Value, []any{2, 1}) {
		t.Fatal(event, err)
	}
}

func TestCodemode120RemoteCrossProcessCancellationJoins(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	ctx, cancel := context.WithTimeout(bg, 15*time.Second)
	defer cancel()
	remote := &RemoteCodemodeSandbox{Remote: pipeRemote120{}, Identity: engine.RuntimeIdentity()}
	vm, err := remote.Start(ctx, `await tools.leaf({});`, CodemodeStart{RuntimeIdentity: engine.RuntimeIdentity(), ParallelCalls: true, DrainAcknowledgements: true, Tools: []CodemodeToolDescription{{Name: "leaf"}}})
	if err != nil {
		t.Fatal(err)
	}
	event, err := vm.Next(ctx)
	if err != nil || event.Call == nil {
		t.Fatal(event, err)
	}
	event, err = vm.Next(ctx)
	if err != nil || event.Drain == nil {
		t.Fatal(event, err)
	}
	joined := make(chan error, 1)
	go func() { joined <- vm.Close() }()
	select {
	case err := <-joined:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("child process did not join", ctx.Err())
	}
}
