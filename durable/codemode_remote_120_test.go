package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
	"time"
)

type failedRemote120 struct{ panic bool }

func (r failedRemote120) Execute(context.Context, string, CodemodeStart, func(context.Context, CodemodeEvent) (CodemodeRemoteReply, error), func(context.Context) (CodemodeDelivery, error)) error {
	if r.panic {
		panic("private")
	}
	return errors.New("transport failed")
}
func TestCodemode120RemoteTransportFailureAndClose(t *testing.T) {
	for _, panicRemote := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		sandbox := &RemoteCodemodeSandbox{Remote: failedRemote120{panicRemote}, Identity: "fixture"}
		vm, err := sandbox.Start(ctx, "", CodemodeStart{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := vm.Next(ctx); err == nil {
			t.Fatal("transport failure lost")
		}
		if err := vm.Close(); err != nil {
			t.Fatal(err)
		}
		cancel()
	}
}

func TestCodemode120RemoteCloseDuringDrainJoins(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	sandbox := &RemoteCodemodeSandbox{Remote: &CodemodeRemoteServer{Sandbox: engine}, Identity: engine.RuntimeIdentity()}
	vm, err := sandbox.Start(ctx, `return "done";`, CodemodeStart{ParallelCalls: true, DrainAcknowledgements: true})
	if err != nil {
		t.Fatal(err)
	}
	event, err := vm.Next(ctx)
	if err != nil || event.Drain == nil {
		t.Fatal(event, err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- vm.Close() }()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("remote close failed to join", ctx.Err())
	}
}

func TestCodemode120RemoteRejectsForgedEventsAndDeliveries(t *testing.T) {
	start := CodemodeStart{Tools: []CodemodeToolDescription{{Name: "leaf"}}}
	for _, call := range []CodemodeCall{{ID: 2, Name: "leaf", Arguments: JSON{}}, {ID: 1, Name: "private", Arguments: JSON{}}, {ID: 1, Name: "leaf", Arguments: nil}} {
		e := &remoteCodemodeExecution{}
		if err := e.admitEvent(CodemodeEvent{Call: &call}, start); err == nil {
			t.Fatal("forged call accepted", call)
		}
	}
	e := &remoteCodemodeExecution{ctx: bg, deliveries: make(chan CodemodeDelivery, 1)}
	call := CodemodeCall{ID: 1, Name: "leaf", Arguments: JSON{"n": 1}}
	if err := e.admitEvent(CodemodeEvent{Call: &call}, start); err != nil {
		t.Fatal(err)
	}
	call.Arguments["n"] = 99
	for _, drain := range []CodemodeDrain{{Issued: 0}, {Issued: 1, DeliveryID: 1}} {
		if err := e.admitEvent(CodemodeEvent{Drain: &drain}, start); err == nil {
			t.Fatal("forged drain accepted", drain)
		}
	}
	delivery := CodemodeDelivery{Seq: 1, Call: CodemodeCall{ID: 1, Name: "leaf", Arguments: JSON{"n": 1}}, HasValue: true, Value: "ok"}
	if err := e.Deliver(bg, delivery); err == nil {
		t.Fatal("unacknowledged delivery accepted")
	}
	e.acknowledged = true
	bad := delivery
	bad.Call.Name = "other"
	if err := e.Deliver(bg, bad); err == nil {
		t.Fatal("forged delivery accepted")
	}
	if err := e.Deliver(bg, delivery); err != nil {
		t.Fatal(err)
	}
	if err := e.Deliver(bg, delivery); err == nil {
		t.Fatal("duplicate delivery accepted")
	}
}

type remoteBudgetSandbox120 struct{ budget, memory int }

func (s *remoteBudgetSandbox120) Start(_ context.Context, _ string, start CodemodeStart) (CodemodeExecution, error) {
	s.budget = start.InterruptBudget
	s.memory = start.MemoryLimitBytes
	return remoteBudgetExecution120{}, nil
}

type remoteBudgetExecution120 struct{}

func (remoteBudgetExecution120) Next(context.Context) (CodemodeEvent, error) {
	return CodemodeEvent{Result: &CodemodeResult{}}, nil
}
func (remoteBudgetExecution120) Deliver(context.Context, CodemodeDelivery) error       { return nil }
func (remoteBudgetExecution120) AcknowledgeDrain(context.Context, CodemodeDrain) error { return nil }
func (remoteBudgetExecution120) Close() error                                          { return nil }
func TestCodemode120RemoteServerCapsCompute(t *testing.T) {
	for _, requested := range []int{0, 10, 1000000} {
		sandbox := &remoteBudgetSandbox120{}
		server := &CodemodeRemoteServer{Sandbox: sandbox, MaxInterruptBudget: 20}
		err := server.Execute(bg, "", CodemodeStart{InterruptBudget: requested}, func(context.Context, CodemodeEvent) (CodemodeRemoteReply, error) {
			return CodemodeRemoteReply{Stop: true}, nil
		}, func(ctx context.Context) (CodemodeDelivery, error) {
			<-ctx.Done()
			return CodemodeDelivery{}, ctx.Err()
		})
		if err != nil {
			t.Fatal(err)
		}
		want := requested
		if want == 0 || want > 20 {
			want = 20
		}
		if sandbox.budget != want {
			t.Fatal("server cap bypassed", requested, sandbox.budget)
		}
	}
}

func TestCodemode120RemoteEventStateMachine(t *testing.T) {
	start := CodemodeStart{Tools: []CodemodeToolDescription{{Name: "leaf"}}}
	for _, mode := range []string{"duplicate-drain", "call-before-ack", "success-before-ack", "call-after-ack", "event-after-terminal"} {
		t.Run(mode, func(t *testing.T) {
			e := &remoteCodemodeExecution{ctx: bg}
			drain := CodemodeDrain{}
			if err := e.admitEvent(CodemodeEvent{Drain: &drain}, start); err != nil {
				t.Fatal(err)
			}
			var event CodemodeEvent
			switch mode {
			case "duplicate-drain":
				event.Drain = &drain
			case "call-before-ack":
				event.Call = &CodemodeCall{ID: 1, Name: "leaf", Arguments: JSON{}}
			case "success-before-ack":
				event.Result = &CodemodeResult{}
			case "call-after-ack":
				e.drain = nil
				e.acknowledged = true
				event.Call = &CodemodeCall{ID: 1, Name: "leaf", Arguments: JSON{}}
			case "event-after-terminal":
				e.drain = nil
				e.acknowledged = true
				if err := e.admitEvent(CodemodeEvent{Result: &CodemodeResult{}}, start); err != nil {
					t.Fatal(err)
				}
				event.Drain = &drain
			}
			if err := e.admitEvent(event, start); err == nil {
				t.Fatal("invalid transition accepted", mode)
			}
		})
	}
}

type ignoringRemote120 struct{}

func (ignoringRemote120) Execute(ctx context.Context, _ string, _ CodemodeStart, exchange func(context.Context, CodemodeEvent) (CodemodeRemoteReply, error), _ func(context.Context) (CodemodeDelivery, error)) error {
	_, _ = exchange(ctx, CodemodeEvent{})
	_, _ = exchange(ctx, CodemodeEvent{Result: &CodemodeResult{Error: "pretend terminal"}})
	return nil
}
func TestCodemode120RemoteCannotIgnoreProtocolRejection(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	sandbox := &RemoteCodemodeSandbox{Remote: ignoringRemote120{}, Identity: "fixture"}
	vm, err := sandbox.Start(ctx, "", CodemodeStart{})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	event, err := vm.Next(ctx)
	if err == nil || event.Result != nil {
		t.Fatal("peer hid protocol rejection", event, err)
	}
}

func TestCodemode120RemoteServerCapsMemory(t *testing.T) {
	for _, requested := range []int{0, 8 << 20, 128 << 20} {
		sandbox := &remoteBudgetSandbox120{}
		server := &CodemodeRemoteServer{Sandbox: sandbox, MaxMemoryLimitBytes: 16 << 20}
		err := server.Execute(bg, "", CodemodeStart{MemoryLimitBytes: requested}, func(context.Context, CodemodeEvent) (CodemodeRemoteReply, error) {
			return CodemodeRemoteReply{Stop: true}, nil
		}, func(ctx context.Context) (CodemodeDelivery, error) {
			<-ctx.Done()
			return CodemodeDelivery{}, ctx.Err()
		})
		want := requested
		if want == 0 || want > 16<<20 {
			want = 16 << 20
		}
		if err != nil || sandbox.memory != want {
			t.Fatal("remote memory cap", requested, sandbox.memory, err)
		}
	}
}
func TestCodemode120QuickJSMemoryCeilingTerminates(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	vm, err := sandbox.Start(ctx, `const retained=[];for(let i=0;i<100000;i++)retained.push(new Array(1000).fill(i));return retained.length;`, CodemodeStart{MemoryLimitBytes: 4 << 20, InterruptBudget: 100000})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	event, err := vm.Next(ctx)
	if err != nil || event.Result == nil || event.Result.Error == "" {
		t.Fatal("heap ceiling did not terminate", event, err)
	}
}

type oversizedRemote120 struct{}

func (oversizedRemote120) Execute(ctx context.Context, _ string, _ CodemodeStart, exchange func(context.Context, CodemodeEvent) (CodemodeRemoteReply, error), _ func(context.Context) (CodemodeDelivery, error)) error {
	_, err := exchange(ctx, CodemodeEvent{Result: &CodemodeResult{Output: strings.Repeat("x", MaxCodemodeFullOutputBytes+1)}})
	return err
}
func TestCodemode120RemoteLargeOutputCeilingUnchanged(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	sandbox := &RemoteCodemodeSandbox{Remote: oversizedRemote120{}, Identity: "fixture"}
	vm, err := sandbox.Start(ctx, "", CodemodeStart{})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	if event, err := vm.Next(ctx); err == nil || event.Result != nil {
		t.Fatal("oversized peer result accepted", err)
	}
}

func TestCodemode120RemoteLargeTextDoesNotWidenOtherPayloads(t *testing.T) {
	limits := DefaultLimits()
	large := strings.Repeat("x", limits.MaxStringBytes+1)
	for _, event := range []CodemodeEvent{
		{Call: &CodemodeCall{ID: 1, Name: "leaf", Arguments: JSON{"large": large}}},
		{Result: &CodemodeResult{StoreWrites: JSON{"large": large}}},
		{Result: &CodemodeResult{Error: large}},
		{Result: &CodemodeResult{Images: []goai.ImageContent{{Type: "image", MimeType: "image/png", Data: large}}}},
	} {
		if err := validateRemoteEventPayload(event); err == nil {
			t.Fatal("large text allowance escaped to another field")
		}
	}
	for _, result := range []CodemodeResult{{Output: large}, {HasValue: true, Value: large}} {
		if err := validateRemoteEventPayload(CodemodeEvent{Result: &result}); err != nil {
			t.Fatal("large output/return string rejected", err)
		}
	}
}

type terminalFailureRemote120 struct{ afterEvent bool }

func (r terminalFailureRemote120) Execute(ctx context.Context, _ string, _ CodemodeStart, exchange func(context.Context, CodemodeEvent) (CodemodeRemoteReply, error), _ func(context.Context) (CodemodeDelivery, error)) error {
	_, err := exchange(ctx, CodemodeEvent{Result: &CodemodeResult{Error: "script terminal"}})
	if err != nil {
		return err
	}
	if r.afterEvent {
		_, _ = exchange(ctx, CodemodeEvent{Drain: &CodemodeDrain{}})
		return nil
	}
	return errors.New("transport failed after terminal")
}
func TestCodemode120RemoteTerminalWaitsForTransportSettlement(t *testing.T) {
	for _, afterEvent := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		sandbox := &RemoteCodemodeSandbox{Remote: terminalFailureRemote120{afterEvent: afterEvent}, Identity: "fixture"}
		vm, err := sandbox.Start(ctx, "", CodemodeStart{})
		if err != nil {
			t.Fatal(err)
		}
		event, err := vm.Next(ctx)
		if err == nil || event.Result != nil {
			t.Fatal("terminal escaped failed transport", event, err)
		}
		if err := vm.Close(); err != nil {
			t.Fatal(err)
		}
		cancel()
	}
}
