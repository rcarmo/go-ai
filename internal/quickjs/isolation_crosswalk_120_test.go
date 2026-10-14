package quickjs

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestQuickJS120PinnedIsolationCases(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	cases := []struct{ name, code, want string }{
		{"host-globals", `return [typeof process,typeof require,typeof module,typeof setTimeout,typeof fetch,typeof WebAssembly,typeof std,typeof os,typeof globalThis.constructor];`, `["undefined","undefined","undefined","undefined","undefined","undefined","undefined","undefined","function"]`},
		{"code-generation", `return [eval("typeof process"),new Function("return typeof process")(),tools.echo.constructor("return typeof require")(),(async()=>{}).constructor("return typeof setTimeout")() instanceof Promise];`, `["undefined","undefined","undefined",true]`},
		{"dynamic-import", `try{await import("node:fs");return "imported"}catch(e){return "denied"}`, `"denied"`},
		{"builtin-patching", `Array.prototype.toJSON=()=>null;Object.prototype.toJSON=()=>5;Promise.prototype.then=()=>{};Map.prototype.get=()=>undefined;globalThis.JSON={stringify:()=>"x",parse:()=>"x"};store("k",[1]);return [await tools.echo({n:2}),JSON.stringify({a:1})];`, `[{"n":2},"{\"a\":1}"]`},
		{"instance-intrinsics", `return [Object.getPrototypeOf(function*(){}).prototype,Object.getPrototypeOf(async function(){}),Object.getPrototypeOf(Int8Array).prototype,Object.getPrototypeOf([][Symbol.iterator]()),Object.getPrototypeOf(Object.getPrototypeOf([][Symbol.iterator]())),Object.getPrototypeOf(new Map()[Symbol.iterator]()),Object.getPrototypeOf(/a/[Symbol.matchAll](""))].every(o=>Object.isFrozen(o));`, `true`},
		{"instance-overrides", `const o={};o.toString=()=>"custom";function Legacy(){};Legacy.prototype=Object.create(Error.prototype);Legacy.prototype.constructor=Legacy;const bare=new Error();bare.message="set later";class MyError extends Error{constructor(m){super(m);this.name="MyError"}};let patched="silent";try{Error.prototype.name="Patched"}catch(e){patched=e.constructor.name};return [String(o),new Legacy().constructor===Legacy,bare.message,new MyError("x").name,Error.prototype.name,patched];`, `["custom",true,"set later","MyError","Error","TypeError"]`},
		{"frozen-tools", `try{tools.echo=()=>"nope"}catch{};try{tools.extra=()=>"nope"}catch{};try{globalThis.tools=null}catch{};return ["extra" in tools,await tools.echo({value:"still"}),Object.isFrozen(console)];`, `[false,{"value":"still"},true]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := engine.Execute(context.Background(), tc.code, Options{StartedAt: 1000, Timeout: 3 * time.Second, MemoryLimitBytes: 16 << 20, Tools: []Tool{{Name: "echo", Execute: func(_ context.Context, args json.RawMessage) (json.RawMessage, error) { return args, nil }}}})
			if !result.OK {
				t.Fatal(result.Error)
			}
			var got, want any
			if err = json.Unmarshal(result.Value, &got); err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			a, _ := json.Marshal(got)
			b, _ := json.Marshal(want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %s want %s", a, b)
			}
			if tc.name == "builtin-patching" && string(result.StoreWrites.Set["k"]) != "[1]" {
				t.Fatal(result.StoreWrites)
			}
		})
	}
	result := engine.Execute(context.Background(), `const e=new Error("x");e.message=42;throw e;`, Options{Timeout: time.Second})
	if result.OK || result.Error == nil || result.Error.Kind != ErrorScript || result.Error.Name != "Error" || result.Error.Message != "42" {
		t.Fatal(result.Error)
	}
}
