package quickjs

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestQuickJS120PinnedReturnsAndFailureOutput(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	for _, tc := range []struct{ code, want string }{{`return {answer:42,values:[true,null,"x"]}`, `{"answer":42,"values":[true,null,"x"]}`}, {`await Promise.resolve();return "done"`, `"done"`}} {
		result := engine.Execute(context.Background(), tc.code, Options{Timeout: time.Second})
		if !result.OK {
			t.Fatal(result.Error)
		}
		var got, want any
		json.Unmarshal(result.Value, &got)
		json.Unmarshal([]byte(tc.want), &want)
		if !reflect.DeepEqual(got, want) {
			t.Fatal(string(result.Value), tc.want)
		}
	}
	result := engine.Execute(context.Background(), `text("before");console.warn("still here");throw Error("later");`, Options{Timeout: time.Second})
	if result.OK || result.Error == nil || result.Error.Kind != ErrorScript || result.Error.Message != "later" || len(result.Output) != 2 {
		t.Fatal(result.Error, result.Output)
	}
	for _, tc := range []struct{ code, name, message string }{{`throw "nope";`, "", "nope"}, {`return 10n;`, "TypeError", ""}} {
		result := engine.Execute(context.Background(), tc.code, Options{Timeout: time.Second})
		if result.OK || result.Error == nil || result.Error.Kind != ErrorScript || tc.name != "" && result.Error.Name != tc.name || tc.message != "" && !strings.Contains(result.Error.Message, tc.message) {
			t.Fatal(tc.code, result.Error)
		}
	}
}
func TestQuickJS120PinnedErrorSourceAndStack(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	code := "const first=1;\nconst second=2;\nthrow new Error(\"line three\");"
	result := engine.Execute(context.Background(), code, Options{Timeout: time.Second})
	if result.OK || result.Error == nil || !strings.Contains(result.Error.Stack, "codemode.js:3") {
		t.Fatal(result.Error)
	}
	result = engine.Execute(context.Background(), "function explode(){\n throw new Error(\"boom\");\n}\nexplode();", Options{Timeout: time.Second})
	if result.OK || result.Error == nil || !strings.Contains(result.Error.Stack, "codemode.js:2") || strings.Contains(result.Error.Stack, "codemode-prelude") {
		t.Fatal(result.Error)
	}
}

func TestQuickJS120PortableOutputCountAndStackRecovery(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	result := engine.Execute(context.Background(), `for(let i=0;i<5000;i++)text(i);return "ok";`, Options{MemoryLimitBytes: 32 << 20, InterruptBudget: 100000})
	if result.Error != nil || len(result.Output) != 5000 || result.Output[4999].Text != "4999" {
		t.Fatal("portable output lost", len(result.Output), result.Error)
	}
	result = engine.Execute(context.Background(), `try{(function recurse(){return 1+recurse()})()}catch(e){if(!(e instanceof RangeError))throw e;return "caught"}`, Options{MemoryLimitBytes: 32 << 20, InterruptBudget: 100000})
	if result.Error != nil || string(result.Value) != `"caught"` {
		t.Fatal("stack overflow not catchable", result.Error, string(result.Value))
	}
	result = engine.Execute(context.Background(), `return 7`, Options{})
	if result.Error != nil || string(result.Value) != "7" {
		t.Fatal("stack failure contaminated engine", result.Error)
	}
}
