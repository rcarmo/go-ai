package durable

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCodemode120QuickJSTextConsoleReturnAndBudget(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	for _, tc := range []struct{ code, want string }{{`text("one");console.log("middle");text("two");console.warn("last");return {n:1};`, "==> text 1/3 <==\none\n==> text 2/3 <==\ntwo\n==> text 3/3 <==\n{\n  \"n\": 1\n}\n<console_output>\nmiddle\nlast\n</console_output>"}, {`return "hello";`, "hello"}, {`return {z:9007199254740992,a:1e21,html:"<>&",unicode:"😀"};`, "{\n  \"z\": 9007199254740992,\n  \"a\": 1e+21,\n  \"html\": \"<>&\",\n  \"unicode\": \"😀\"\n}"}, {`return null;`, "null"}, {`console.log("only");`, "<console_output>\nonly\n</console_output>"}} {
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		vm, err := sandbox.Start(ctx, tc.code, CodemodeStart{StartedAt: 1000, Seed: 42})
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		event, err := vm.Next(ctx)
		vm.Close()
		cancel()
		if err != nil || event.Result == nil || event.Result.Error != "" || event.Result.Output != tc.want {
			t.Fatalf("code %s result %+v err %v", tc.code, event.Result, err)
		}
	}
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		def := taskDefinition(t, "output-budget-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, err := r.RunCodemode(ctx, "run", "// @options: {\"max_output_tokens\":1}\nconsole.log('long console');return 'long result';", sandbox)
			if err != nil {
				return err
			}
			if result.Error != "" || result.Output != "long" || !strings.Contains(result.Value.(string), "result") {
				t.Errorf("format budget %+v", result)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatal(record.State.Outcome)
		}
	})
}
