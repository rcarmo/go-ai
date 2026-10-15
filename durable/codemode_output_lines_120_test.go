package durable

import (
	"context"
	"strings"
	"testing"
)

func TestCodemode120OutputLineLimitSpillsFullText(t *testing.T) {
	text := "START\n" + strings.Repeat("x\n", 200000) + "FINISH"
	projection := projectCodemodeOutput(text, 1000000)
	if strings.Count(projection, "\n")+1 >= MaxCodemodeOutputLines || !strings.HasPrefix(projection, "START") || !strings.HasSuffix(projection, "FINISH") || !strings.Contains(projection, "truncated") {
		t.Fatal("line projection", len(projection), strings.Count(projection, "\n"))
	}
	backends(t, func(t *testing.T, b backend) {
		writer := &outputWriter120{}
		registry := NewRegistry()
		def := taskDefinition(t, "line-output", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, err := r.prepareCodemodeOutput(ctx, "script", CodemodeResult{Output: text}, 1000000)
			if err != nil {
				return err
			}
			if result.FullOutputPath != "/owned/output.txt" || strings.Count(result.Output, "\n")+1 >= MaxCodemodeOutputLines || !strings.Contains(result.Output, "START") || !strings.Contains(result.Output, "FINISH") {
				t.Error("line result", len(result.Output), result.FullOutputPath)
			}
			writer.mu.Lock()
			full := writer.writes["output.txt"]
			writer.mu.Unlock()
			if full != text {
				t.Error("spill not full")
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, Env: func(context.Context, EnvTarget) (ExecutionEnvironment, error) { return writer, nil }})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "completed" {
			t.Fatal(record.State.Outcome)
		}
	})
}
