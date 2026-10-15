package durable

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

type addressFaultFS120 struct{ *codemodeFS120 }

func (f *addressFaultFS120) CreateTempDir(context.Context, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates++
	return fmt.Sprintf("/owned/tmp/run-%d", f.creates), nil
}
func TestCodemode120FilesystemAddressCommitFaultsNeverWriteBeforeConfirmation(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	for _, mode := range []string{"rejected", "uncertain-before", "uncertain-after"} {
		t.Run(mode, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				fs := &addressFaultFS120{&codemodeFS120{id: "address-fs"}}
				registry := NewRegistry()
				def := taskDefinition(t, "address-fault", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					result, err := r.RunCodemode(ctx, "script", "// @options: {\"max_output_tokens\":1}\ntext(\"long output\");", engine)
					if err != nil {
						return err
					}
					if result.FullOutputPath == "" {
						t.Error("spill path missing")
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				if _, err = registry.RegisterTask(def); err != nil {
					t.Fatal(err)
				}
				h := openHarness(t, b.store, Options{Registry: registry, Env: func(context.Context, EnvTarget) (ExecutionEnvironment, error) { return fs, nil }})
				id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				core := h.session.core
				original := core.appendFrame
				cause := reject("directory address rejected")
				var failures atomic.Int64
				installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
					if kind == 2 {
						var record commitRecord
						if err := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &record); err != nil {
							return err
						}
						for _, write := range record.Writes {
							var doc Document
							var value JSON
							if write.Document != nil {
								doc = *write.Document
								value = doc.Value
							} else if write.Delta != nil {
								doc = core.state.Documents[write.Delta.ID]
								changed, err := ApplyOperations(doc.Value, write.Delta.Ops, h.session.limits)
								if err != nil {
									return err
								}
								value = JSON(changed.(map[string]any))
							}
							if doc.Kind == codemodeRunKind && value["outputDir"] != nil && value["result"] == nil {
								failures.Add(1)
								if mode == "rejected" {
									return cause
								}
								if mode == "uncertain-after" && original != nil {
									if err := original(kind, ordinal, high, payload); err != nil {
										return err
									}
								}
								return errors.New("uncertain directory address")
							}
						}
					}
					if original != nil {
						return original(kind, ordinal, high, payload)
					}
					return nil
				})
				if err = h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(bg, 5*time.Second)
				defer cancel()
				end, err := h.session.Closed(ctx)
				if err != nil || end.Reason != "failed" {
					t.Fatal(end, err)
				}
				if mode == "rejected" && !errors.Is(end.Error, cause) || mode != "rejected" && !errors.Is(end.Error, ErrPoisoned) {
					t.Fatal(end)
				}
				if err = h.Close(ctx); err != nil {
					t.Fatal(err)
				}
				fs.mu.Lock()
				if fs.creates != 1 || fs.writes != 0 {
					t.Fatal("wrote before address confirmation", fs.creates, fs.writes)
				}
				fs.mu.Unlock()
				reopened := reopenStoreAfterHarnessClose(t, b.store)
				state, err := reopened.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				_, journal := b.store.(*JournalStorage)
				confirmed := mode == "uncertain-after" && journal
				for _, doc := range state.Documents {
					if doc.Kind == codemodeRunKind {
						has := doc.Value["outputDir"] != nil
						if has != confirmed {
							t.Fatal("recovered address confirmation", mode, doc)
						}
					}
				}
				next := openHarness(t, reopened, h.options)
				record := waitPublicTask(t, next, id)
				if record.State.Outcome.Status != "completed" || failures.Load() != 1 {
					t.Fatalf("outcome=%+v error=%+v", record.State.Outcome, record.State.Outcome.Error)
				}
				fs.mu.Lock()
				defer fs.mu.Unlock()
				wantCreates := 2
				path := "/owned/tmp/run-2/output.txt"
				if confirmed {
					wantCreates = 1
					path = "/owned/tmp/run-1/output.txt"
				}
				if fs.creates != wantCreates || fs.writes != 1 || string(fs.files[path]) != "long output" {
					t.Fatal(mode, fs.creates, fs.writes, fs.files)
				}
			})
		})
	}
}
