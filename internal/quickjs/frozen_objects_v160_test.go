package quickjs

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// Source: pi v1.6.0 packages/codemode/test/sandbox.test.ts:748–818.
func TestSourceV160QuickJSFrozenObjects(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	echo := []Tool{{Name: "echo", Execute: func(_ context.Context, args json.RawMessage) (json.RawMessage, error) { return args, nil }}}
	for _, tc := range []struct{ name, code, want string }{
		{"built-in-patching", `
			Array.prototype.toJSON = () => null;
			Object.prototype.toJSON = () => 5;
			Promise.prototype.then = () => {};
			Map.prototype.get = () => undefined;
			globalThis.JSON = { stringify: () => "x", parse: () => "x" };
			store("k", [1]);
			return [await tools.echo([2]), JSON.stringify({ a: 1 })];
		`, `[[2],"{\"a\":1}"]`},
		{"instance-intrinsics", `
			return [
				Object.getPrototypeOf(function* () {}).prototype,
				Object.getPrototypeOf(async function () {}),
				Object.getPrototypeOf(Int8Array).prototype,
				Object.getPrototypeOf([][Symbol.iterator]()),
				Object.getPrototypeOf(Object.getPrototypeOf([][Symbol.iterator]())),
				Object.getPrototypeOf(new Map()[Symbol.iterator]()),
				Object.getPrototypeOf(/a/[Symbol.matchAll]("")),
			].every((object) => Object.isFrozen(object));
		`, `true`},
		{"instance-overrides", `
			const object = {};
			object.toString = () => "custom";
			function Legacy() {}
			Legacy.prototype = Object.create(Error.prototype);
			Legacy.prototype.constructor = Legacy;
			const bare = new Error();
			bare.message = "set later";
			class MyError extends Error {
				constructor(message) {
					super(message);
					this.name = "MyError";
				}
			}
			let patched = "silent";
			try { Error.prototype.name = "Patched"; } catch (error) { patched = error.constructor.name; }
			return [String(object), new Legacy().constructor === Legacy, bare.message, new MyError("x").name, Error.prototype.name, patched];
		`, `["custom",true,"set later","MyError","Error","TypeError"]`},
		{"tools", `
			try { tools.echo = () => 'nope'; } catch {}
			try { tools.extra = () => 'nope'; } catch {}
			try { globalThis.tools = null; } catch {}
			return ["extra" in tools, await tools.echo('still')];
		`, `[false,"still"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := engine.Execute(ctx, tc.code, Options{MemoryLimitBytes: 32 << 20, Tools: echo})
			if !got.OK || string(got.Value) != tc.want {
				t.Errorf("ok=%v value=%s error=%+v; want %s", got.OK, got.Value, got.Error, tc.want)
			}
			if tc.name == "built-in-patching" && (len(got.StoreWrites.Set) != 1 || string(got.StoreWrites.Set["k"]) != "[1]" || len(got.StoreWrites.Delete) != 0) {
				t.Errorf("store changes=%+v; want only k=[1]", got.StoreWrites)
			}
		})
	}
}
