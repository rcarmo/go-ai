package durable

import (
	"context"
	"errors"
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
