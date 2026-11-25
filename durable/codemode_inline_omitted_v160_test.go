package durable

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCodemodeInlineOmittedArgumentPolicyV160(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(context.Background())
	// This saved v2 shape has no omission bit. Decoding it must not select
	// today's policy just because the older start reopens in a new host.
	var saved CodemodeStart
	if err := json.Unmarshal([]byte(`{"version":2,"startedAt":1000,"seed":7,"tools":[{"name":"echo","identifier":"echo"}]}`), &saved); err != nil {
		t.Fatal(err)
	}
	saved.RuntimeIdentity = sandbox.RuntimeIdentity()
	for _, tc := range []struct {
		name, expression, wantError string
		version                     int
		pinned                      bool
	}{
		{name: "standalone-omitted"},
		{name: "saved-legacy-omitted", version: 2, wantError: "incomplete JSON"},
		{name: "saved-pinned-omitted", version: 2, pinned: true},
		{name: "saved-pinned-undefined", expression: "undefined", version: 2, pinned: true},
		{name: "saved-pinned-null", expression: "null", version: 2, pinned: true, wantError: "tool arguments must be an object"},
		{name: "saved-pinned-number", expression: "1", version: 2, pinned: true, wantError: "tool arguments must be an object"},
		{name: "saved-pinned-array", expression: "[]", version: 2, pinned: true, wantError: "tool arguments must be an object"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			start := saved
			start.Version, start.InlineOmittedToolArguments = tc.version, tc.pinned
			vm, err := sandbox.Start(ctx, "return await tools.echo("+tc.expression+");", start)
			if err != nil {
				t.Fatal(err)
			}
			defer vm.Close()
			first, err := vm.Next(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantError != "" {
				if first.Call != nil || first.Result == nil || !strings.Contains(first.Result.Error, tc.wantError) {
					t.Fatalf("first event = %+v; want %q without a call", first, tc.wantError)
				}
				return
			}
			if first.Call == nil || first.Call.Name != "echo" || first.Call.Global || first.Call.UndefinedArgument || first.Call.Arguments == nil || len(first.Call.Arguments) != 0 {
				t.Fatalf("first event = %+v; want one tool call with an empty object", first)
			}
			if err := vm.Deliver(ctx, CodemodeDelivery{Call: *first.Call, Seq: first.Call.ID, Now: start.StartedAt, Value: "ok", HasValue: true}); err != nil {
				t.Fatal(err)
			}
			final, err := vm.Next(ctx)
			if err != nil || final.Result == nil || final.Result.Error != "" || !final.Result.HasValue || final.Result.Value != "ok" {
				t.Fatalf("final event=%+v err=%v; want successful reply ok", final, err)
			}
		})
	}
}

func TestCodemodeInlineGlobalArgumentPresenceUnaffectedV160(t *testing.T) {
	if value, undefined, err := decodeQuickJSCallArguments(nil, true, true); err != nil || !undefined || value == nil || len(value) != 0 {
		t.Fatalf("omitted global = %v, %v, %v; want undefined", value, undefined, err)
	}
	value, undefined, err := decodeQuickJSCallArguments([]byte("null"), true, true)
	arg, found := value["args"]
	if err != nil || undefined || !found || arg != nil {
		t.Fatalf("explicit null global = %v, %v, %v; want present null", value, undefined, err)
	}
}
