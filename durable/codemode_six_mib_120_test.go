package durable

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodemode120SixMiBReturnSpillsAndReplays(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		writer := &outputWriter120{}
		var calls atomic.Int64
		def := taskDefinition(t, "six-mib", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			for i := 0; i < 2; i++ {
				result, err := r.RunCodemode(ctx, "large", `return "x".repeat(6*1024*1024);`, engine)
				if err != nil {
					return err
				}
				text, ok := result.Value.(string)
				if result.Error != "" || !result.HasValue || !ok || len(text) != 6<<20 || result.FullOutputPath != "/owned/output.txt" {
					t.Errorf("result error=%q value=%T length=%d path=%q", result.Error, result.Value, len(text), result.FullOutputPath)
				}
				calls.Add(1)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		registry := NewRegistry()
		registry.RegisterTask(def)
		h := openHarness(t, b.store, Options{Registry: registry, OnReport: func(err error) { t.Log("report", err) }, Env: func(context.Context, EnvTarget) (ExecutionEnvironment, error) { return writer, nil }})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		ctx, cancel := context.WithTimeout(bg, 60*time.Second)
		defer cancel()
		record, err := h.WaitForTask(ctx, id)
		if err != nil {
			state, _ := h.Snapshot(bg)
			for _, task := range state.Tasks {
				t.Logf("task %d status %s", task.ID, task.Status)
			}
			t.Fatal(err)
		}
		if record.State.Outcome.Status != "completed" || calls.Load() != 2 {
			t.Fatalf("outcome %+v error %+v", record.State.Outcome, record.State.Outcome.Error)
		}
		writer.mu.Lock()
		defer writer.mu.Unlock()
		if writer.count != 1 || writer.writes["output.txt"] != strings.Repeat("x", 6<<20) {
			t.Fatal("full spill missing or repeated", writer.count)
		}
	})
}
