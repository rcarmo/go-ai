package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodemode120OutputWriterRejectedResultReusesStableAddress(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		writer := &addressWriter120{}
		def := taskDefinition(t, "store-fault-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			_, e := r.RunCodemode(ctx, "run", "// @options: {\"max_output_tokens\":1}\nstore('valid',true);return 'long output';", engine)
			if e != nil {
				return e
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry, Env: func(context.Context, EnvTarget) (ExecutionEnvironment, error) { return writer, nil }})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		core := h.session.core
		original := core.appendFrame
		cause := reject("codemode store/result rejected")
		var failures atomic.Int64
		installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
			if kind == 2 {
				var record commitRecord
				if e := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &record); e != nil {
					return e
				}
				for _, write := range record.Writes {
					var doc Document
					var value JSON
					if write.Document != nil {
						doc = *write.Document
						value = doc.Value
					} else if write.Delta != nil {
						doc = core.state.Documents[write.Delta.ID]
						changed, e := ApplyOperations(doc.Value, write.Delta.Ops, h.session.limits)
						if e != nil {
							return e
						}
						value = JSON(changed.(map[string]any))
					}
					if doc.Kind == codemodeRunKind && value["result"] != nil {
						failures.Add(1)
						return cause
					}
				}
			}
			if original != nil {
				return original(kind, ordinal, high, payload)
			}
			return nil
		})
		if e := h.Resume(bg); e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		end, e := h.session.Closed(ctx)
		if e != nil || !errors.Is(end.Error, cause) {
			t.Fatal(end, e)
		}
		if e = h.Close(ctx); e != nil {
			t.Fatal(e)
		}
		reopened := reopenStoreAfterHarnessClose(t, b.store)
		state, e := reopened.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		for _, doc := range state.Documents {
			if doc.Kind == codemodeRunKind && doc.Value["result"] != nil {
				t.Fatal("rejected result adopted")
			}
			if doc.Kind == "pi.codemode.store" && len(doc.Value["values"].(map[string]any)) != 0 {
				t.Fatal("store partially adopted", doc.Value)
			}
		}
		next := openHarness(t, reopened, h.options)
		record := waitPublicTask(t, next, id)
		if record.State.Outcome.Status != "completed" || failures.Load() != 1 || writer.count != 2 || writer.drift || writer.task != id || writer.key != "run" || writer.name != "output.txt" || writer.data != "long output" {
			t.Fatalf("%+v %+v starts:%d writes:%d", record.State.Outcome, record.State.Outcome.Error, writer.count, failures.Load())
		}
	})
}

type addressWriter120 struct {
	count           int
	task            ID
	key, name, data string
	drift           bool
}

func (*addressWriter120) Cwd() string { return "/owned" }
func (w *addressWriter120) WriteCodemodeOutput(_ context.Context, task ID, key, name string, data []byte) (string, error) {
	if w.count == 0 {
		w.task, w.key, w.name, w.data = task, key, name, string(data)
	} else if w.task != task || w.key != key || w.name != name || w.data != string(data) {
		w.drift = true
	}
	w.count++
	return "/owned/output.txt", nil
}
