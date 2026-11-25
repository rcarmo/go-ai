package durable

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

func workerScriptErrorFixtureV160(name, message, stack string) json.RawMessage {
	encoded, _ := json.Marshal(JSON{"name": name, "message": message, "stack": stack})
	raw, _ := json.Marshal(string(encoded))
	return raw
}

func TestCodemodeWorkerScriptStackOmitsHostFramesV160(t *testing.T) {
	const want = "TypeError: boom\n    at <anonymous> (codemode.js:2:7)"
	raw := workerScriptErrorFixtureV160("TypeError", "boom", want+"\n    at bridge (codemode-prelude.js:28)\n    at host (/private/runtime.js:3:2)")
	got, err := decodeWorkerScriptError(raw, DefaultLimits())
	if err != nil || got.Kind != quickjs.ErrorScript || got.Stack != want {
		t.Fatalf("script error = %+v, %v; want only %q", got, err, want)
	}
}

func TestCodemodeWorkerPositionlessOutcomeFenceV160(t *testing.T) {
	const identity = "script-error-fixture"
	syntax := workerScriptErrorFixtureV160("SyntaxError", "bad", "SyntaxError: bad\n    at codemode.js:2:11")
	thrown := workerScriptErrorFixtureV160("TypeError", "bad", "TypeError: bad\n    at codemode.js:2:11")
	hostOnly := workerScriptErrorFixtureV160("SyntaxError", "bad", "SyntaxError: bad\n    at host (/private/runtime.js:3:2)")
	value := "42"
	for _, tc := range []struct {
		name, fingerprint string
		firstDrain        bool
		terminal          CodemodeWorkerMessage
	}{
		{"no-handshake", "", false, CodemodeWorkerMessage{Type: "done", Error: syntax}},
		{"success", identity, false, CodemodeWorkerMessage{Type: "done", OK: true, Value: &value}},
		{"failure-with-value", identity, false, CodemodeWorkerMessage{Type: "done", Error: syntax, Value: &value}},
		{"syntax-after-drain", identity, true, CodemodeWorkerMessage{Type: "done", Error: syntax}},
		{"ordinary-throw", identity, false, CodemodeWorkerMessage{Type: "done", Error: thrown}},
		{"host-stack", identity, false, CodemodeWorkerMessage{Type: "done", Error: hostOnly}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &CodemodeWorkerRemote{Fingerprint: identity, Transport: workerTransport120(func(ctx context.Context, _ CodemodeWorkerRequest, exchange func(context.Context, []CodemodeWorkerMessage) (*CodemodeWorkerReply, error)) error {
				if tc.fingerprint != "" {
					if _, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "started", Fingerprint: tc.fingerprint}}); err != nil {
						return err
					}
				}
				if tc.firstDrain {
					if _, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "idle", Seq: 0, Issued: 0}}); err != nil {
						return err
					}
				}
				_, err := exchange(ctx, []CodemodeWorkerMessage{tc.terminal})
				return err
			})}
			err := r.Execute(t.Context(), "", CodemodeStart{}, func(_ context.Context, ev CodemodeEvent) (CodemodeRemoteReply, error) {
				if ev.Result != nil || ev.Call != nil {
					t.Error("an invalid positionless frame crossed the worker fence")
				}
				return CodemodeRemoteReply{Acknowledged: ev.Drain}, nil
			}, func(ctx context.Context) (CodemodeDelivery, error) {
				<-ctx.Done()
				return CodemodeDelivery{}, ctx.Err()
			})
			if err == nil || !strings.Contains(err.Error(), "worker done lacks deterministic position") {
				t.Fatalf("invalid positionless result error = %v", err)
			}
		})
	}
}
