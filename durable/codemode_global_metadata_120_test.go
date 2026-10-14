package durable

import (
	"context"
	"strings"
	"testing"
)

type metadataSandbox120 struct{ seen []string }

func (s *metadataSandbox120) Start(_ context.Context, _ string, start CodemodeStart) (CodemodeExecution, error) {
	s.seen = append(s.seen, start.CodemodeDeclarations())
	start.Globals[0].InputSchema["type"] = "number"
	return metadataExecution120{}, nil
}

type metadataExecution120 struct{}

func (metadataExecution120) Next(context.Context) (CodemodeEvent, error) {
	return CodemodeEvent{Result: &CodemodeResult{Output: "ok"}}, nil
}
func (metadataExecution120) Deliver(context.Context, CodemodeDelivery) error { return nil }
func (metadataExecution120) Close() error                                    { return nil }
func TestCodemode120GlobalMetadataDetachedAtOpenAndRun(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		schema := JSON{"type": "string"}
		globals := []CodemodeGlobal{{Name: "host.lookup", Version: 1, Description: "Look up.", InputSchema: schema, OutputSchema: JSON{"type": "boolean"}, Execute: func(context.Context, any) (any, error) { return true, nil }}}
		sandbox := &metadataSandbox120{}
		registry := NewRegistry()
		def := taskDefinition(t, "global-metadata-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			for _, key := range []string{"one", "two"} {
				if _, err := r.RunCodemode(ctx, key, "source", sandbox); err != nil {
					return err
				}
			}
			for _, text := range sandbox.seen {
				if !strings.Contains(text, "/** Look up. */") || !strings.Contains(text, "lookup(args: string): Promise<boolean>") {
					t.Error("metadata alias", text)
				}
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, CodemodeGlobals: globals})
		schema["type"] = "integer"
		globals[0].Description = "changed"
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
	})
}
