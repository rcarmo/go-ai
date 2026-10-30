package durable

import (
	"context"
	"sync/atomic"
	"testing"
)

type legacyUTF8Sandbox struct {
	base   *QuickJSSandbox
	starts atomic.Int64
}

func (s *legacyUTF8Sandbox) RuntimeIdentity() string             { return s.base.RuntimeIdentity() }
func (s *legacyUTF8Sandbox) SupportsParallelCalls() bool         { return false }
func (s *legacyUTF8Sandbox) SupportsDrainAcknowledgements() bool { return true }
func (s *legacyUTF8Sandbox) Start(ctx context.Context, code string, start CodemodeStart) (CodemodeExecution, error) {
	execution, err := s.base.Start(ctx, code, start)
	if err != nil {
		return nil, err
	}
	return &legacyUTF8Execution{CodemodeExecution: execution, legacy: s.starts.Add(1) == 1}, nil
}

type legacyUTF8Execution struct {
	CodemodeExecution
	legacy bool
}

func (e *legacyUTF8Execution) Next(ctx context.Context) (CodemodeEvent, error) {
	event, err := e.CodemodeExecution.Next(ctx)
	if event.Call != nil && e.legacy {
		copy := *event.Call
		copy.StreamingArguments = false
		event.Call = &copy
	}
	return event, err
}
func (e *legacyUTF8Execution) AcknowledgeDrain(ctx context.Context, d CodemodeDrain) error {
	return e.CodemodeExecution.(CodemodeDrainExecution).AcknowledgeDrain(ctx, d)
}

func TestCodemodeUTF16MainLegacyUTF8Reopen(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		base, err := NewQuickJSSandbox(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer base.Close(bg)
		base.AcknowledgeDrains = true
		sandbox := &legacyUTF8Sandbox{base: base}
		var effects atomic.Int64
		entered := make(chan struct{})
		registry := NewRegistry()
		reg := wrapRegistration("ordinary_leaf")
		reg.ReplaySafe = true
		reg.Execute = func(ctx context.Context, args JSON, _ *ToolAPI) (ToolResult, error) {
			if args["text"] != "ordinary" {
				t.Error("old arguments changed", args)
			}
			if effects.Add(1) == 1 {
				close(entered)
				<-ctx.Done()
				return ToolResult{}, ctx.Err()
			}
			return ToolResult{Content: "done"}, nil
		}
		if err = registry.Register(reg); err != nil {
			t.Fatal(err)
		}
		definition := taskDefinition(t, "codemode-legacy-utf8-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, err := r.RunCodemode(ctx, "legacy", `return await tools.ordinary_leaf({text:"ordinary"});`, sandbox)
			if err != nil {
				return err
			}
			if result.Error != "" || result.Value != "done" {
				t.Error("legacy result changed", result)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err = registry.RegisterTask(definition); err != nil {
			t.Fatal(err)
		}
		first := openHarness(t, b.store, Options{Registry: registry})
		id := createPublicTask(t, first, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err = first.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		state, err := first.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, doc := range state.Documents {
			if doc.Kind == codemodeJournalKind {
				var journal codemodeJournalState
				if err = fromObject(doc.Value, &journal, first.session.limits); err != nil {
					t.Fatal(err)
				}
				if len(journal.Issued) != 1 || journal.Issued[0].StreamingArguments {
					t.Fatal("legacy fixture contains new marker")
				}
			}
		}
		if err = first.Close(bg); err != nil {
			t.Fatal(err)
		}
		second := openHarness(t, utf16ReopenStore(t, b.store), Options{Registry: registry})
		result := waitPublicTask(t, second, id)
		if result.State.Outcome.Status != "completed" || effects.Load() != 2 || sandbox.starts.Load() != 2 {
			t.Fatal("old journal replay rejected or duplicated effects", result.State.Outcome, effects.Load(), sandbox.starts.Load())
		}
	})
}
