package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"
)

const v160NamespacedGlobalScript = `
await models.list("classifier", undefined, 3);
await models.list();
try { models.extra = 1; } catch {}
return [Object.keys(models), await models.first("a", "ignored"), "extra" in models];`

// Source: sandbox.test.ts, "groups namespaced globals and spreads arguments on
// request". This tests the Go adapter's public protocol seam, not the guest
// implementation. Both spread arguments must stay arrays, while the non-spread
// call sends only its first argument as a JSON string.
func TestSourceV160CodemodeWorkerGlobalArguments(t *testing.T) {
	const fingerprint = "namespaced-global-wire-v160"
	calls := []struct{ name, args, payload string }{
		{"models.list", `["classifier",null,3]`, ""},
		{"models.list", `[]`, ""},
		{"models.first", `"a"`, `"a"`},
		// Supplemental non-spread object crossing; never coerce a JSON value
		// or unwrap a user object that happens to contain an args member.
		{"models.first", `{"args":[1]}`, `{"args":[1]}`},
		{"models.first", `[1,2]`, `[1,2]`},
		// These historical shapes must still match existing replay records.
		{"models.first", `null`, `[]`},
		{"models.first", "", `[]`},
	}
	transport := workerTransport120(func(ctx context.Context, request CodemodeWorkerRequest, exchange func(context.Context, []CodemodeWorkerMessage) (*CodemodeWorkerReply, error)) error {
		want := []CodemodeWorkerGlobal{{Name: "models.list", Spread: true}, {Name: "models.first"}}
		if !reflect.DeepEqual(request.Data.Globals, want) {
			return fmt.Errorf("worker globals = %+v; want %+v", request.Data.Globals, want)
		}
		if _, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "started", Fingerprint: fingerprint}}); err != nil {
			return err
		}
		for i, call := range calls {
			id := i + 1
			if _, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "idle", Seq: i, Issued: i}}); err != nil {
				return err
			}
			message := CodemodeWorkerMessage{Type: "call", ID: id, Target: "global", Name: call.name}
			if call.args != "" {
				message.Args = &call.args
			}
			reply, err := exchange(ctx, []CodemodeWorkerMessage{message})
			if err != nil {
				return err
			}
			want := &CodemodeWorkerReply{Type: "result", ID: id, OK: true, Seq: id}
			if call.payload != "" {
				want.Payload = &call.payload
			}
			if !reflect.DeepEqual(reply, want) {
				return fmt.Errorf("worker reply %d = %+v; want %+v", id, reply, want)
			}
		}
		_, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "done", OK: true, Writes: workerString120(`[]`), During: &CodemodeWorkerDuring{Seq: len(calls), Issued: len(calls)}}})
		return err
	})
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	remote := &CodemodeWorkerRemote{Transport: transport, Fingerprint: fingerprint}
	start := CodemodeStart{Globals: []CodemodeGlobalDescription{{Name: "models.list", Spread: true, Version: 1}, {Name: "models.first", Version: 1}}}
	deliveries := make(chan CodemodeDelivery, 1)
	seen := 0
	terminal := false
	err := remote.Execute(ctx, v160NamespacedGlobalScript, start, func(ctx context.Context, event CodemodeEvent) (CodemodeRemoteReply, error) {
		switch {
		case event.Call != nil:
			call := *event.Call
			if seen >= len(calls) || call.ID != seen+1 || !call.Global || call.Name != calls[seen].name {
				return CodemodeRemoteReply{}, fmt.Errorf("unexpected worker call %+v at %d", call, seen)
			}
			wire, err := json.Marshal(call.Arguments)
			args := calls[seen].args
			if call.Name == "models.first" {
				args = calls[seen].payload
			}
			want := `{"args":` + args + `}`
			if err != nil || string(wire) != want {
				return CodemodeRemoteReply{}, fmt.Errorf("worker call %d args = %s, %v; want %s", call.ID, wire, err, want)
			}
			seen++
			delivery := CodemodeDelivery{Seq: call.ID, Call: call}
			if call.Name == "models.first" {
				delivery.HasValue = true
				delivery.Value = call.Arguments["args"]
			}
			select {
			case deliveries <- delivery:
			case <-ctx.Done():
				return CodemodeRemoteReply{}, ctx.Err()
			}
		case event.Drain != nil:
			return CodemodeRemoteReply{Acknowledged: event.Drain}, nil
		case event.Result != nil:
			terminal = event.Result.Error == "" && !event.Result.HasValue
		}
		return CodemodeRemoteReply{}, nil
	}, func(ctx context.Context) (CodemodeDelivery, error) {
		select {
		case delivery := <-deliveries:
			return delivery, nil
		case <-ctx.Done():
			return CodemodeDelivery{}, ctx.Err()
		}
	})
	if err != nil || !terminal || seen != len(calls) {
		t.Fatalf("worker calls = %d, terminal = %t, error = %v", seen, terminal, err)
	}
}

func TestV160CodemodeWorkerSpreadRejectsScalar(t *testing.T) {
	const fingerprint = "namespaced-global-wire-v160"
	transport := workerTransport120(func(ctx context.Context, _ CodemodeWorkerRequest, exchange func(context.Context, []CodemodeWorkerMessage) (*CodemodeWorkerReply, error)) error {
		if _, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "started", Fingerprint: fingerprint}}); err != nil {
			return err
		}
		_, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "call", ID: 1, Target: "global", Name: "models.list", Args: workerString120(`"not a list"`)}})
		return err
	})
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	remote := &CodemodeWorkerRemote{Transport: transport, Fingerprint: fingerprint}
	start := CodemodeStart{Globals: []CodemodeGlobalDescription{{Name: "models.list", Spread: true, Version: 1}}}
	err := remote.Execute(ctx, v160NamespacedGlobalScript, start, func(context.Context, CodemodeEvent) (CodemodeRemoteReply, error) {
		return CodemodeRemoteReply{}, fmt.Errorf("scalar was forwarded to the host")
	}, func(ctx context.Context) (CodemodeDelivery, error) {
		<-ctx.Done()
		return CodemodeDelivery{}, ctx.Err()
	})
	const want = "json: cannot unmarshal string into Go value of type []interface {}"
	if err == nil || err.Error() != want {
		t.Fatalf("worker error = %v; want %q", err, want)
	}
}
