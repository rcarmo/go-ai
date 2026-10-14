package durable

import (
	"context"
	"strings"
	"sync"
	"testing"
)

type outputWriter120 struct {
	mu     sync.Mutex
	writes map[string]string
	count  int
	fail   bool
}

func (*outputWriter120) Cwd() string { return "/owned" }
func (w *outputWriter120) WriteCodemodeOutput(_ context.Context, _ ID, _ string, name string, data []byte) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.count++
	if w.fail {
		return "", reject("private writer details")
	}
	if w.writes == nil {
		w.writes = map[string]string{}
	}
	w.writes[name] = string(data)
	return "/owned/" + name, nil
}
func TestCodemode120OutputSpillCapabilitySettledReuse(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		writer := &outputWriter120{}
		registry := NewRegistry()
		def := taskDefinition(t, "spill-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			code := "// @options: {\"max_output_tokens\":1}\nreturn 'long output';"
			result, err := r.RunCodemode(ctx, "run", code, engine)
			if err != nil {
				return err
			}
			if result.Output != "long" || result.FullOutputPath != "/owned/output.txt" {
				t.Error(result)
			}
			if _, err = r.RunCodemode(ctx, "run", code, engine); err != nil {
				return err
			}
			writer.mu.Lock()
			defer writer.mu.Unlock()
			if writer.count != 1 || writer.writes["output.txt"] != "long output" {
				t.Error(writer.count, writer.writes)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err = registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, Env: func(context.Context, EnvTarget) (ExecutionEnvironment, error) { return writer, nil }})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
	})
}
func TestCodemode120OutputWriterPanicHidesPrivateData(t *testing.T) {
	_, err := callCodemodeOutputWriter(bg, &panicOutputWriter120{}, 1, "key", "output.txt", nil)
	if err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal(err)
	}
}

type panicOutputWriter120 struct{}

func (*panicOutputWriter120) Cwd() string { return "" }
func (*panicOutputWriter120) WriteCodemodeOutput(context.Context, ID, string, string, []byte) (string, error) {
	panic("private")
}
