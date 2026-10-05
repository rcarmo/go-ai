package durable

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// Install internal storage fault control while BOTH admission lines are held;
// OnReport is supplied at Open, never mutated beside the live scheduler.
func installTaskAppend(t *testing.T, h *Harness, core *storeCore, appendFrame func(byte, uint64, uint64, []byte) error) {
	t.Helper()
	h.session.taskBookkeeping(func() { <-core.line; core.appendFrame = appendFrame; core.leave() })
}

func TestTaskRecoveryOpenNoEffectsAndCloseJoinsStubbornHost(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		var calls atomic.Int64
		def := taskDefinition(t, "task.recovery.host", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if calls.Add(1) == 1 {
				close(entered)
				<-release
				return ctx.Err()
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("reopened"), nil })
		})
		h := taskTestHarness(t, b.store, def)
		cleanupTaskGates(t, release)
		id := createPublicTask(t, h, def, []any{nil, "input"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if calls.Load() != 0 {
			t.Fatal("Open/creation dispatched")
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		closing := make(chan error, 1)
		go func() { closing <- h.Close(bg) }()
		seal, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for !h.closing.Load() {
			select {
			case <-seal.Done():
				t.Fatal("Close seal acknowledgement")
			default:
				runtime.Gosched()
			}
		}
		h.session.taskBookkeeping(func() {
			if len(h.scheduler.invocations) != 1 {
				t.Error("Close lost actual host join")
			}
		})
		select {
		case err := <-closing:
			t.Fatal("Close released stubborn host", err)
		default:
		}
		close(release)
		if err := <-closing; err != nil {
			t.Fatal(err)
		}
		reopened := reopenStoreAfterHarnessClose(t, b.store)
		second := taskTestHarness(t, reopened, def)
		record, ok, err := second.Task(bg, id)
		if err != nil || !ok || record.State.Status != "pending" || calls.Load() != 1 {
			t.Fatal("Open recovery effects/state", record, err)
		}
		if record := waitPublicTask(t, second, id); record.State.Outcome.Status != "completed" || calls.Load() != 2 {
			t.Fatal(record, calls.Load())
		}
	})
}

func TestTaskRecoveryInvalidMigrationCachesDefinitionIdentityAndNoReadEffects(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		original := taskDefinition(t, "task.migration.identity", func(context.Context, TaskRecord, *TaskRuntime) error { return nil })
		var migrations atomic.Int64
		reports := make(chan error, 4)
		h := taskTestHarnessOptions(t, b.store, Options{OnReport: func(err error) {
			select {
			case reports <- err:
			default:
			}
		}})
		id := createPublicTask(t, h, original, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		options := original.options
		options.Version = 2
		options.Migrate = func(any, JSON, uint64) (any, JSON, error) {
			migrations.Add(1)
			return func() {}, JSON{"phase": "work"}, nil
		}
		broken, err := DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
		dispose, err := h.options.Registry.RegisterTask(broken)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.InspectTasks(bg); err != nil || migrations.Load() != 0 {
			t.Fatal("inspect migrated", err)
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		select {
		case <-reports:
		case <-time.After(3 * time.Second):
			t.Fatal("invalid migration not reported")
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "migration.wake", Value: JSON{}})
		})
		if err != nil {
			t.Fatal(err)
		}
		inspection, err := h.InspectTasks(bg)
		if err != nil || migrations.Load() != 1 || len(inspection.Tasks) != 1 || inspection.Tasks[0].Reason != "migration_failed" {
			t.Fatal("same definition retried", migrations.Load(), inspection, err)
		}
		options.Migrate = func(input any, checkpoint JSON, _ uint64) (any, JSON, error) {
			migrations.Add(1)
			return input, checkpoint, nil
		}
		options.Phases = map[string]TaskPhase{"work": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("v2"), nil })
		}}
		replacement, err := DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
		dispose()
		if _, err := h.options.Registry.RegisterTask(replacement); err != nil {
			t.Fatal(err)
		}
		record := waitPublicTask(t, h, id)
		if record.Version != 2 || migrations.Load() != 2 {
			t.Fatal("replacement migration identity", record, migrations.Load())
		}
	})
}

func TestTaskRecoveryRejectedReservationDoesNotSpinAndPoisonCloseJoins(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "rejected", true: "uncertain"}[uncertain], func(t *testing.T) {
			store, err := NewMemory()
			if err != nil {
				t.Fatal(err)
			}
			var runs atomic.Int64
			def := taskDefinition(t, "task.reservation.failure", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				runs.Add(1)
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			})
			report := make(chan error, 1)
			h := taskTestHarnessOptions(t, store, Options{OnReport: func(err error) {
				select {
				case report <- err:
				default:
				}
			}}, def)
			id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			var attempts atomic.Int64
			var once sync.Once
			installTaskAppend(t, h, store.storeCore, func(kind byte, _ uint64, _ uint64, _ []byte) error {
				if kind != 2 {
					return nil
				}
				attempts.Add(1)
				var failure error
				once.Do(func() {
					if uncertain {
						failure = errors.New("uncertain reservation")
					} else {
						failure = reject("definite reservation rejection")
					}
				})
				return failure
			})
			if err := h.Resume(bg); err != nil {
				t.Fatal(err)
			}
			select {
			case <-report:
			case <-time.After(3 * time.Second):
				t.Fatal("reservation failure missing")
			}
			h.session.taskBookkeeping(func() {
				if len(h.scheduler.invocations) != 0 {
					t.Error("failed reservation retained permit")
				}
			})
			if runs.Load() != 0 || attempts.Load() != 1 {
				t.Fatal("failed reservation dispatched/retried", runs.Load(), attempts.Load())
			}
			if uncertain {
				if err := h.Close(bg); err != nil {
					t.Fatal(err)
				}
				return
			}
			installTaskAppend(t, h, store.storeCore, nil)
			record := waitPublicTask(t, h, id)
			if record.State.Outcome.Status != "completed" || runs.Load() != 1 {
				t.Fatal(record, runs.Load())
			}
		})
	}
}

func TestTaskRecoveryPoisonUnrelatedCommitRetainsActualJoin(t *testing.T) {
	store, err := NewMemory()
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	def := taskDefinition(t, "task.poison.stubborn", func(context.Context, TaskRecord, *TaskRuntime) error { close(entered); <-release; return nil })
	h := taskTestHarness(t, store, def)
	cleanupTaskGates(t, release)
	_ = createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	if err := h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, entered)
	installTaskAppend(t, h, store.storeCore, func(kind byte, _ uint64, _ uint64, _ []byte) error {
		if kind == 2 {
			return errors.New("poison unrelatedcommit")
		}
		return nil
	})
	_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
		id, err := tx.MintID()
		if err != nil {
			return err
		}
		return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "poison", Value: JSON{}})
	})
	if !errors.Is(err, ErrPoisoned) {
		t.Fatal(err)
	}
	closing := make(chan error, 1)
	go func() { closing <- h.Close(bg) }()
	for !h.closing.Load() {
		runtime.Gosched()
	}
	h.session.taskBookkeeping(func() {
		if len(h.scheduler.invocations) != 1 {
			t.Error("poison erased join")
		}
	})
	select {
	case err := <-closing:
		t.Fatal("poison Close skipped actual join", err)
	default:
	}
	close(release)
	if err := <-closing; err != nil {
		t.Fatal(err)
	}
}

func TestTaskRecoveryMarkedBlockedMigrationOrphansSamePass(t *testing.T) {
	for _, variant := range []string{"missing", "too-old", "no-migrate", "thrown", "invalid-output"} {
		t.Run(variant, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				var migrations atomic.Int64
				original := taskDefinition(t, "task.marked.migration", func(context.Context, TaskRecord, *TaskRuntime) error { return errors.New("blocked task executed") })
				originalOptions := original.options
				originalOptions.Version = 2
				var err error
				original, err = DefineTask(originalOptions)
				if err != nil {
					t.Fatal(err)
				}
				h := taskTestHarness(t, b.store)
				id := createPublicTask(t, h, original, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if variant != "missing" {
					options := originalOptions
					if variant == "too-old" {
						options.Version = 1
					} else {
						options.Version = 3
					}
					if variant == "thrown" || variant == "invalid-output" {
						options.Migrate = func(any, JSON, uint64) (any, JSON, error) {
							migrations.Add(1)
							if variant == "thrown" {
								return nil, nil, errors.New("rejected migration")
							}
							return func() {}, JSON{"phase": "work"}, nil
						}
					}
					def, err := DefineTask(options)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := h.options.Registry.RegisterTask(def); err != nil {
						t.Fatal(err)
					}
				}
				// Admit the durable mark BEFORE enabling scheduling. No synthetic
				// later Resume/Wait kick rescues a no-op failed resolution pass.
				_, err = h.session.Commit(bg, func(tx *Tx) error {
					task := markTask(tx.state.Tasks[id])
					return tx.stage(Write{Op: "put-task", Task: &task})
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				record := observeTaskState(t, h, id, "terminal")
				if record.State.Outcome.Status != "orphaned" || !record.AbortRequested {
					t.Fatal("marked blocked task not orphaned", record)
				}
				want := "migration_failed"
				if variant == "missing" {
					want = "missing_task"
				}
				if variant == "too-old" {
					want = "task_too_old"
				}
				if record.State.Outcome.Reason != want {
					t.Fatal("orphan reason", record)
				}
				if (variant == "thrown" || variant == "invalid-output") && migrations.Load() != 1 {
					t.Fatal("migration retries", migrations.Load())
				}
			})
		})
	}
}

func TestTaskRecoveryReopenedAbortMarkedMissingDefinitionOrphansOnResume(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var runs atomic.Int64
		entered, marked, returnRun := make(chan struct{}), make(chan struct{}), make(chan struct{})
		def := taskDefinition(t, "task.reopen.marked.missing", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			runs.Add(1)
			close(entered)
			<-ctx.Done()
			close(marked)
			<-returnRun
			return ctx.Err()
		})
		first := taskTestHarness(t, b.store, def)
		cleanupTaskGates(t, returnRun)
		id := createPublicTask(t, first, def, JSON{"kept": "input"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := first.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		caller, cancel := context.WithCancel(bg)
		defer cancel()
		abort := make(chan error, 1)
		go func() { _, err := first.AbortTask(caller, id); abort <- err }()
		awaitTaskSignal(t, marked)
		cancel()
		if err := <-abort; !errors.Is(err, context.Canceled) {
			t.Fatal("marked caller cancel", err)
		}
		closing := make(chan error, 1)
		go func() { closing <- first.Close(bg) }()
		deadline, stop := context.WithTimeout(bg, 3*time.Second)
		defer stop()
		for !first.closing.Load() {
			select {
			case <-deadline.Done():
				t.Fatal("Close seal missing")
			default:
				runtime.Gosched()
			}
		}
		releaseTaskGate(returnRun)
		if err := <-closing; err != nil {
			t.Fatal(err)
		}
		second := taskTestHarness(t, reopenStoreAfterHarnessClose(t, b.store))
		record, ok, err := second.Task(bg, id)
		if err != nil || !ok || !record.AbortRequested || record.State.Status != "pending" || runs.Load() != 1 {
			t.Fatal("reopened marked prefix", record, err, runs.Load())
		}
		if err := second.Resume(bg); err != nil {
			t.Fatal(err)
		}
		record = waitPublicTask(t, second, id)
		if record.State.Outcome.Status != "orphaned" || record.State.Outcome.Reason != "missing_task" || runs.Load() != 1 {
			t.Fatal("resume did not orphan missing definition", record, runs.Load())
		}
	})
}

func assertTaskToolSpend(t *testing.T, state Snapshot, conversation ID, name string, total int) {
	t.Helper()
	for _, doc := range state.Documents {
		if doc.Kind == "pi.usage" && doc.Owner == conversation && !doc.Retired {
			tools, ok := doc.Value["tools"].(map[string]any)
			if !ok {
				t.Fatal("tools usage shape", doc.Value)
			}
			spent, ok := tools[name].(map[string]any)
			if !ok || !equalJSONValue(spent["totalTokens"], total) {
				t.Fatal("aggregate tool usage", tools, total)
			}
			return
		}
	}
	t.Fatal("tool aggregate usage missing")
}

func TestTaskRecoveryBuiltinHoldFinalFailureAndReopen(t *testing.T) {
	for _, backendName := range []string{"memory", "journal"} {
		for _, failure := range []string{"rejected", "uncertain-before", "uncertain-after"} {
			t.Run(backendName+"/"+failure, func(t *testing.T) {
				image := &MemoryImage{}
				directory := t.TempDir() + "/private"
				var store Storage
				var core *storeCore
				var err error
				open := func() Storage {
					var s Storage
					var e error
					if backendName == "memory" {
						s, e = OpenMemory(MemoryOptions{Image: image})
					} else {
						s, e = OpenJournal(directory, JournalOptions{})
					}
					if e != nil {
						t.Fatal(e)
					}
					return s
				}
				store = open()
				if m, ok := store.(*MemoryStorage); ok {
					core = m.storeCore
				} else {
					core = store.(*JournalStorage).storeCore
				}
				var requests, effects, callbacks, childRuns atomic.Int64
				childEntered, releaseChild := make(chan struct{}), make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					if requests.Add(1) == 1 {
						taskHTTPRound(w, []string{"hold-recovery"})
					} else {
						taskHTTPRound(w, nil)
					}
				}))
				defer server.Close()
				model := fakeModel(goai.ApiOpenAICompletions)
				model.BaseURL = server.URL
				model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
				registry := NewRegistry()
				var h *Harness
				var toolID, taskDocID ID
				child := taskDefinition(t, "task.hold.recovery.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					if childRuns.Add(1) == 1 {
						close(childEntered)
						select {
						case <-releaseChild:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("child"), nil })
				})
				if _, err := registry.RegisterTask(child); err != nil {
					t.Fatal(err)
				}
				err = registry.Register(ToolRegistration{Definition: goai.Tool{Name: "hold-recovery", Description: "held result", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "hold.recovery", Version: 1, ReplaySafe: false, Execute: func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
					effects.Add(1)
					toolID = api.TaskID()
					// Raw authority forgery is rejected even BEFORE Memo adds Execution.
					public, ok, err := api.Task(ctx, toolID)
					if err != nil || !ok {
						return ToolResult{}, errors.New("active tool missing")
					}
					if err := api.Commit(ctx, func(tx *Tx) error {
						return tx.PutTask(Task{ID: public.ID, Owner: public.Owner, Conversation: public.Conversation, Kind: public.Kind, Status: "done", Checkpoint: public.State.Checkpoint})
					}); err == nil {
						return ToolResult{}, errors.New("builtin raw authority forged")
					}
					if _, err := api.MemoCandidate(ctx, "toString", JSON{"memo": "owned"}); err != nil {
						return ToolResult{}, err
					}
					if err := api.Output("stored-prefix"); err != nil {
						return ToolResult{}, err
					}
					if _, ok, err := api.Memo(ctx, "toString"); err != nil || !ok {
						return ToolResult{}, errors.New("Output lost memo")
					}
					if err := api.Commit(ctx, func(tx *Tx) error {
						var err error
						taskDocID, err = tx.MintID()
						if err != nil {
							return err
						}
						_, err = tx.CreateDocument(Document{ID: taskDocID, Scope: "task", Owner: toolID, Kind: "app.task-hold", Version: 1, Value: JSON{"retained": true}})
						return err
					}); err != nil {
						return ToolResult{}, err
					}
					if _, err := api.CreateTask(ctx, child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: toolID}}); err != nil {
						return ToolResult{}, err
					}
					return ToolResult{Content: "held-result", Details: JSON{"held": true}, Usage: &goai.Usage{Input: 1, Output: 2, TotalTokens: 3}, Commit: func(tx *Tx) error {
						callbacks.Add(1)
						for _, doc := range tx.state.Documents {
							if doc.Kind == "app.result" {
								handle, err := tx.Document(doc.ID)
								if err != nil {
									return err
								}
								return handle.Set(JSON{"sum": 7})
							}
						}
						return errors.New("app document missing")
					}}, nil
				}})
				if err != nil {
					t.Fatal(err)
				}
				reported := make(chan error, 8)
				options := Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }, OnReport: func(err error) {
					select {
					case reported <- err:
					default:
					}
				}}
				h = openHarness(t, store, options)
				cleanupTaskGates(t, releaseChild)
				conv := root(t, h, ModelRef{model.Provider, model.ID})
				appDocID := createAppDocument(t, conv)
				sub, err := conv.Submit(bg, Input{Content: "hold", RequestID: "held"})
				if err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, childEntered)
				observeTaskState(t, h, toolID, "completing")
				before, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				assertTaskToolSpend(t, before, conv.ID(), "hold-recovery", 3)
				if callbacks.Load() != 1 || effects.Load() != 1 || requests.Load() != 1 || before.Documents[taskDocID].Retired || !equalJSONValue(before.Documents[appDocID].Value["sum"], 7) {
					t.Fatal("held deciding effects", callbacks.Load(), effects.Load(), before.Documents[taskDocID])
				}
				if before.Submissions[sub.ID()].Status != "pending" {
					t.Fatal("submission cleaned at tool hold", before.Submissions[sub.ID()])
				}
				var cp toolCheckpoint
				if err := fromObject(before.Tasks[toolID].Checkpoint, &cp, h.session.limits); err != nil || cp.Output != "stored-prefix" || cp.Result == nil || before.Tasks[toolID].Execution.Builtin.Memos != nil {
					t.Fatal("memo/output/receipt placement", cp, err)
				}
				var original func(byte, uint64, uint64, []byte) error
				h.session.taskBookkeeping(func() { <-core.line; original = core.appendFrame; core.leave() })
				var injected atomic.Bool
				installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
					if kind == 2 {
						var record commitRecord
						if err := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &record); err != nil {
							return err
						}
						for _, write := range record.Writes {
							if write.Task != nil && write.Task.ID == toolID && terminalStatus(write.Task.Status) {
								injected.Store(true)
								if failure == "uncertain-after" && original != nil {
									if err := original(kind, ordinal, high, payload); err != nil {
										return err
									}
								}
								if failure == "rejected" {
									return reject("held final rejected")
								}
								return errors.New("uncertain held final")
							}
						}
					}
					if original != nil {
						return original(kind, ordinal, high, payload)
					}
					return nil
				})
				releaseTaskGate(releaseChild)
				select {
				case <-reported:
				case <-time.After(3 * time.Second):
					t.Fatal("final failure not reported")
				}
				if !injected.Load() {
					t.Fatal("did not witness held final append")
				}
				if failure == "rejected" {
					rejectedState, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					if rejectedState.Tasks[toolID].Status != "completing" || rejectedState.Documents[taskDocID].Retired || terminalStatus(rejectedState.Submissions[sub.ID()].Status) {
						t.Fatal("rejected Final partially adopted", rejectedState.Tasks[toolID])
					}
					assertTaskToolSpend(t, rejectedState, conv.ID(), "hold-recovery", 3)
				}
				if err := h.Close(bg); err != nil {
					t.Fatal(err)
				}
				second := openHarness(t, open(), options)
				opened, err := second.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				assertTaskToolSpend(t, opened, conv.ID(), "hold-recovery", 3)
				if callbacks.Load() != 1 || effects.Load() != 1 {
					t.Fatal("Open replayed adopted Hold")
				}
				wantFinal := backendName == "journal" && failure == "uncertain-after"
				if terminalStatus(opened.Tasks[toolID].Status) != wantFinal {
					t.Fatal("confirmed reopened prefix", opened.Tasks[toolID].Status, wantFinal)
				}
				resumed := &SubmissionHandle{h: second, id: sub.ID()}
				if result := waitSubmission(t, resumed); result.Submission.Status != "done" {
					t.Fatal(result)
				}
				final, err := second.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				assertTaskToolSpend(t, final, conv.ID(), "hold-recovery", 3)
				if !final.Documents[taskDocID].Retired || callbacks.Load() != 1 || effects.Load() != 1 || requests.Load() != 2 {
					t.Fatal("final replay/retirement", callbacks.Load(), effects.Load(), requests.Load())
				}
				var receipts int
				for _, entry := range final.Entries {
					if entry.ByTask == toolID && entry.Kind == "message" {
						receipts++
					}
				}
				if receipts != 1 {
					t.Fatal("held receipt repeated", receipts)
				}
			})
		}
	}
}

func TestTaskRecoveryGenerationHeldReceiptFinalCleanup(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		requestEntered, answer, childEntered, releaseChild := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
		var requests atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			close(requestEntered)
			<-answer
			taskHTTPRound(w, nil)
		}))
		defer server.Close()
		model := fakeModel(goai.ApiOpenAICompletions)
		model.BaseURL = server.URL
		model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
		registry := NewRegistry()
		child := taskDefinition(t, "task.generation.hold.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(childEntered)
			select {
			case <-releaseChild:
			case <-ctx.Done():
				return ctx.Err()
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(child); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }})
		cleanupTaskGates(t, answer, releaseChild)
		conv := root(t, h, ModelRef{model.Provider, model.ID})
		sub, err := conv.Submit(bg, Input{Content: "held answer"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, requestEntered)
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		var generationID, taskDocID ID
		for _, task := range state.Tasks {
			if task.Kind == "pi.generation" {
				generationID = task.ID
			}
		}
		_, err = h.CommitTasks(bg, conv.ID(), func(tx *Tx) error {
			if _, err := tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: generationID}}); err != nil {
				return err
			}
			var err error
			taskDocID, err = tx.MintID()
			if err != nil {
				return err
			}
			_, err = tx.CreateDocument(Document{ID: taskDocID, Scope: "task", Owner: generationID, Kind: "app.generation-hold", Version: 1, Value: JSON{}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, childEntered)
		releaseTaskGate(answer)
		held := observeTaskState(t, h, generationID, "completing")
		if held.State.Outcome.Status != "completed" {
			t.Fatal(held)
		}
		state, err = h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if state.Submissions[sub.ID()].Status != "pending" || state.Documents[taskDocID].Retired {
			t.Fatal("generation cleanup leaked into Hold")
		}
		assertTaskModelSpend(t, state, conv.ID(), model.Provider, model.ID, 5)
		var receipts int
		for _, entry := range state.Entries {
			if entry.ByTask == generationID {
				receipts++
			}
		}
		if receipts != 1 {
			t.Fatal("generation receipt placement at Hold", receipts)
		}
		// A later mark cannot rewrite the adopted answer. Child cancellation
		// may drain via its Abort adapter, while the submission retains success.
		if _, err := h.AbortTask(bg, generationID); err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(releaseChild)
		if settled := waitSubmission(t, sub); settled.Submission.Status != "done" || settled.Message == nil {
			t.Fatal("decided generation changed", settled)
		}
		state, err = h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		assertTaskModelSpend(t, state, conv.ID(), model.Provider, model.ID, 5)
		if !state.Documents[taskDocID].Retired || requests.Load() != 1 {
			t.Fatal("generation final effects repeated")
		}
	})
}

func assertTaskModelSpend(t *testing.T, state Snapshot, conversation ID, provider goai.Provider, model string, total int) {
	t.Helper()
	for _, doc := range state.Documents {
		if doc.Kind == "pi.usage" && doc.Owner == conversation && !doc.Retired {
			models, ok := doc.Value["models"].(map[string]any)
			if !ok {
				t.Fatal("model usage shape")
			}
			value, ok := models[string(provider)+"/"+model].(map[string]any)
			if !ok || !equalJSONValue(value["totalTokens"], total) {
				t.Fatal("aggregate model usage", models, total)
			}
			return
		}
	}
	t.Fatal("model usage missing")
}

func TestTaskRecoverySchedulerGenerationOrphanNoFabricatedMessage(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var effects atomic.Int64
		h := taskTestHarnessOptions(t, b.store, Options{Models: func(goai.Provider, string) *goai.Model { effects.Add(1); return nil }})
		var taskID, subID ID
		_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
			if err := initializeBuiltins(tx, 1); err != nil {
				return err
			}
			var err error
			subID, err = tx.MintID()
			if err != nil {
				return err
			}
			taskID, err = tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.PutSubmission(Submission{ID: subID, Conversation: 1, Type: "follow-up", Status: "pending", Value: JSON{"content": "intent"}}); err != nil {
				return err
			}
			cp, err := dtoObject(generationCheckpoint{Phase: "missing-host-phase", Submission: subID}, tx.limits)
			if err != nil {
				return err
			}
			return tx.PutTask(Task{ID: taskID, Conversation: 1, Kind: "pi.generation", Status: "pending", Checkpoint: cp})
		})
		if err != nil {
			t.Fatal(err)
		}
		sub := &SubmissionHandle{h: h, id: subID}
		settled := waitSubmission(t, sub)
		if settled.Submission.Status != "failed" || settled.Message != nil || effects.Load() != 0 {
			t.Fatal("orphan invented response/effect", settled, effects.Load())
		}
		record, ok, err := h.Task(bg, taskID)
		if err != nil || !ok || record.State.Outcome.Status != "orphaned" {
			t.Fatal(record, err)
		}
		state, err := h.Snapshot(bg)
		if err != nil || len(state.Entries) != 0 {
			t.Fatal("orphan fabricated receipt", state.Entries, err)
		}
	})
}

func TestTaskRecoveryToolOutcomePanicRollbackFallbackAndReopen(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var requests, effects, callbacks atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if requests.Add(1) == 1 {
				taskHTTPRound(w, []string{"panic-result"})
			} else {
				taskHTTPRound(w, nil)
			}
		}))
		defer server.Close()
		model := fakeModel(goai.ApiOpenAICompletions)
		model.BaseURL = server.URL
		model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
		registry := NewRegistry()
		var docID, toolID ID
		if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "panic-result", Description: "outcome fence", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "panic.result", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: "success must rollback", Usage: &goai.Usage{Output: 2, TotalTokens: 2}, Commit: func(tx *Tx) error {
				callbacks.Add(1)
				doc, err := tx.Document(docID)
				if err != nil {
					return err
				}
				if err := doc.Set(JSON{"sum": 99}); err != nil {
					return err
				}
				panic("private callback payload must not persist")
			}}, nil
		}}); err != nil {
			t.Fatal(err)
		}
		options := Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }}
		h := openHarness(t, b.store, options)
		conv := root(t, h, ModelRef{model.Provider, model.ID})
		docID = createAppDocument(t, conv)
		sub, err := conv.Submit(bg, Input{Content: "throwing outcome"})
		if err != nil {
			t.Fatal(err)
		}
		if settled := waitSubmission(t, sub); settled.Submission.Status != "done" {
			t.Fatal(settled)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if !equalJSONValue(state.Documents[docID].Value["sum"], 0) || callbacks.Load() != 1 || effects.Load() != 1 {
			t.Fatal("panic callback leaked/repeated", state.Documents[docID], callbacks.Load(), effects.Load())
		}
		assertTaskToolSpend(t, state, conv.ID(), "panic-result", 2)
		for _, task := range state.Tasks {
			if task.Kind == "pi.tool" {
				toolID = task.ID
				var cp toolCheckpoint
				if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil || cp.ErrorCode != "tool_outcome_rejected" || cp.Result == nil || !cp.Result.IsError {
					t.Fatal("panic fallback receipt", cp, err)
				}
			}
		}
		if toolID == 0 {
			t.Fatal("tool missing")
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		opened, err := second.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		assertTaskToolSpend(t, opened, conv.ID(), "panic-result", 2)
		if callbacks.Load() != 1 || effects.Load() != 1 || opened.Tasks[toolID].Status != "done" {
			t.Fatal("panic fallback replayed")
		}
	})
}

// Child-process crash stages use only a temporary journal/service directory.
// The parent kills and joins the REAL process before claiming journal ownership.
func TestTaskRecoveryCrashHelper(t *testing.T) {
	dir := os.Getenv("GO_AI_TASK_CRASH_DIR")
	if dir == "" {
		return
	}
	stage := os.Getenv("GO_AI_TASK_CRASH_STAGE")
	store, err := OpenJournal(dir+"/private", JournalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	writeWitness := func(state Snapshot, id ID) {
		payload, err := json.Marshal(struct {
			Task     ID
			Snapshot Snapshot
		}{id, state})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dir+"/adopted.json", payload, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ack := func(h *Harness, id ID) {
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		writeWitness(state, id)
	}
	registry := NewRegistry()
	var h *Harness
	never := make(chan struct{})
	hostReady := make(chan struct{})
	child := taskDefinition(t, "task.crash.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error { <-never; return ctx.Err() })
	parent := taskDefinition(t, "task.crash.parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		if stage == "held" {
			if err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				_, err := tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
				return nil, err
			}); err != nil {
				return err
			}
		}
		if err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
			id, err := tx.MintID()
			if err != nil {
				return nil, err
			}
			if _, err := tx.CreateDocument(Document{ID: id, Scope: "task", Owner: r.TaskID(), Kind: "app.crash-task", Version: 1, Value: JSON{"held": true}}); err != nil {
				return nil, err
			}
			if stage == "held" {
				return taskDone("decided"), nil
			}
			if stage == "final" {
				return taskDone("decided"), nil
			}
			return nil, nil
		}); err != nil {
			return err
		}
		close(hostReady) // actual phase entry AND task document adoption
		if stage != "marked" && stage != "mark-append" {
			ack(h, r.TaskID())
		}
		<-never
		return nil
	})
	if strings.HasPrefix(stage, "abort-") {
		options := parent.options
		options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				id, err := tx.MintID()
				if err != nil {
					return nil, err
				}
				_, err = tx.CreateDocument(Document{ID: id, Scope: "task", Owner: r.TaskID(), Kind: "app.crash-task", Version: 1, Value: JSON{"abort": true}})
				return nil, err
			}); err != nil {
				return err
			}
			if stage == "abort-host" {
				ack(h, r.TaskID())
				<-never
				return nil
			}
			if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "decided-abort"}}, nil
			}); err != nil {
				return err
			}
			ack(h, r.TaskID())
			<-never
			return nil
		}
		parent, err = DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := registry.RegisterTask(child); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RegisterTask(parent); err != nil {
		t.Fatal(err)
	}
	h, err = Open(bg, store, Options{Registry: registry})
	if err != nil {
		t.Fatal(err)
	}
	id := createPublicTask(t, h, parent, []any{"input", nil}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	if strings.HasPrefix(stage, "abort-") {
		_, err = h.session.Commit(bg, func(tx *Tx) error {
			task := markTask(tx.state.Tasks[id])
			return tx.stage(Write{Op: "put-task", Task: &task})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if stage == "reservation-append" || stage == "abort-outcome-append" {
		original := store.storeCore.appendFrame
		installTaskAppend(t, h, store.storeCore, func(kind byte, ordinal, high uint64, payload []byte) error {
			if kind == 2 {
				var record commitRecord
				if err := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &record); err != nil {
					return err
				}
				for _, write := range record.Writes {
					if write.Task != nil && write.Task.ID == id && ((stage == "reservation-append" && write.Task.Status == "running") || (stage == "abort-outcome-append" && terminalStatus(write.Task.Status))) {
						// Both native lines held: record the confirmed prefix before the
						// attempted frame is appended. Snapshot() would re-enter/deadlock.
						writeWitness(store.storeCore.state, id)
						<-never
						return nil
					}
				}
			}
			return original(kind, ordinal, high, payload)
		})
	}
	if err := h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	if stage == "marked" || stage == "mark-append" {
		// Wait for the real invocation, then persist mark without joining the
		// intentionally stubborn host. This is the pre-join crash boundary.
		awaitTaskSignal(t, hostReady)
		if stage == "mark-append" {
			original := store.storeCore.appendFrame
			installTaskAppend(t, h, store.storeCore, func(kind byte, ordinal, high uint64, payload []byte) error {
				if kind == 2 {
					var record commitRecord
					if err := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &record); err != nil {
						return err
					}
					for _, write := range record.Writes {
						if write.Task != nil && write.Task.ID == id && taskAborted(*write.Task) {
							writeWitness(store.storeCore.state, id)
							<-never
							return nil
						}
					}
				}
				return original(kind, ordinal, high, payload)
			})
		}
		_, err = h.session.Commit(bg, func(tx *Tx) error {
			task := markTask(tx.state.Tasks[id])
			return tx.stage(Write{Op: "put-task", Task: &task})
		})
		if err != nil {
			t.Fatal(err)
		}
		ack(h, id)
	}
	<-never
}

func TestTaskRecoveryRealSIGKILLAdoptedStages(t *testing.T) {
	for _, stage := range []string{"running", "mark-append", "marked", "abort-host", "abort-outcome-append", "abort-final", "reservation-append", "held", "final"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(bg, 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestTaskRecoveryCrashHelper$", "-test.count=1")
			cmd.Env = append(os.Environ(), "GO_AI_TASK_CRASH_DIR="+dir, "GO_AI_TASK_CRASH_STAGE="+stage)
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			joined := false
			defer func() {
				if !joined {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
				if t.Failed() {
					t.Logf("raw crash-helper stdout/stderr (%s):\n%s", stage, output.String())
				}
			}()
			var witness struct {
				Task     ID
				Snapshot Snapshot
			}
			for {
				payload, e := os.ReadFile(dir + "/adopted.json")
				if e == nil && json.Unmarshal(payload, &witness) == nil {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("child did not acknowledge adopted stage", stage)
				default:
					runtime.Gosched()
				}
			}
			assertTaskCrashStage(t, witness.Snapshot, witness.Task, stage, false)
			if err := ctx.Err(); err != nil {
				t.Fatal("crash stage already timed out", err)
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			err = cmd.Wait()
			joined = true
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatal("real crash not observed", err)
			}
			status, ok := exit.Sys().(syscall.WaitStatus)
			if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL || ctx.Err() != nil {
				t.Fatal("unexpected child exit/timeout", exit, status, ctx.Err())
			}
			store, err := OpenJournal(dir+"/private", JournalOptions{})
			if err != nil {
				t.Fatal(err)
			}
			var effects, parentRuns, abortRuns atomic.Int64
			child := taskDefinition(t, "task.crash.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				effects.Add(1)
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("child"), nil })
			})
			parent := taskDefinition(t, "task.crash.parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				parentRuns.Add(1)
				effects.Add(1)
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("recovered"), nil })
			})
			options := parent.options
			options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				abortRuns.Add(1)
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "recovered-abort"}}, nil
				})
			}
			parent, err = DefineTask(options)
			if err != nil {
				t.Fatal(err)
			}
			h := taskTestHarness(t, store, parent, child)
			opened, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			assertTaskCrashStage(t, opened, witness.Task, stage, true)
			if effects.Load() != 0 || !equalTaskValue(opened.Tasks[witness.Task].Execution.Native.Input, witness.Snapshot.Tasks[witness.Task].Execution.Native.Input, h.session.limits) {
				t.Fatal("Open effects/input changed")
			}
			record := waitPublicTask(t, h, witness.Task)
			want := "completed"
			if stage == "marked" || strings.HasPrefix(stage, "abort-") {
				want = "aborted"
			}
			if record.State.Outcome.Status != want {
				t.Fatal("crash outcome", stage, record)
			}
			if stage == "held" || stage == "final" {
				if record.State.Outcome.Result.Value != "decided" {
					t.Fatal("decided outcome rerun", record)
				}
			}
			if (stage == "marked" || stage == "abort-host" || stage == "abort-outcome-append") && (parentRuns.Load() != 0 || abortRuns.Load() != 1 || effects.Load() != 0) {
				t.Fatal("confirmed mark replayed run/missed fresh abort", parentRuns.Load(), abortRuns.Load(), effects.Load())
			}
			if (stage == "mark-append" || stage == "reservation-append" || stage == "running") && (parentRuns.Load() != 1 || abortRuns.Load() != 0) {
				t.Fatal("unconfirmed mark/reservation recovery dispatch", parentRuns.Load(), abortRuns.Load())
			}
			if (stage == "final" || stage == "abort-final") && (effects.Load() != 0 || abortRuns.Load() != 0) {
				t.Fatal("terminal effect replayed")
			}
			if stage == "held" && effects.Load() != 1 {
				t.Fatal("held parent replayed or child missing", effects.Load())
			}
			final, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			for _, doc := range final.Documents {
				if doc.Scope == "task" && doc.Owner == witness.Task && !doc.Retired {
					t.Fatal("confirmed terminal document not retired", doc)
				}
			}
		})
	}
}

func assertTaskCrashStage(t *testing.T, state Snapshot, id ID, stage string, reopened bool) {
	t.Helper()
	task, ok := state.Tasks[id]
	if !ok || task.Execution == nil || task.Execution.Native == nil {
		t.Fatal("crash witness missing native task", stage)
	}
	want := "running"
	if reopened {
		want = "pending"
	}
	if stage == "held" {
		want = "completing"
	}
	if stage == "final" {
		want = "done"
	}
	if stage == "abort-final" {
		want = "aborted"
	}
	if stage == "reservation-append" {
		want = "pending"
	}
	marked := stage == "marked" || strings.HasPrefix(stage, "abort-")
	if task.Status != want || taskAborted(task) != marked {
		t.Fatal("crash status/mark witness", stage, reopened, task.Status, taskAborted(task))
	}
	decided := stage == "held" || stage == "final" || stage == "abort-final"
	if taskHasDecidedOutcome(task) != decided {
		t.Fatal("crash decided witness", stage)
	}
	if decided && stage != "abort-final" && task.Execution.Native.State.Outcome.Result.Value != "decided" {
		t.Fatal("crash outcome witness", stage)
	}
	if stage == "abort-final" && (task.Execution.Native.State.Outcome.Status != "aborted" || task.Execution.Native.State.Outcome.Reason != "decided-abort") {
		t.Fatal("crash abort outcome witness")
	}
	var docs, children int
	for _, doc := range state.Documents {
		if doc.Scope == "task" && doc.Owner == id {
			docs++
			if doc.Retired != (stage == "final" || stage == "abort-final") {
				t.Fatal("crash document witness", stage, doc)
			}
		}
	}
	for _, child := range state.Tasks {
		if child.Owner == id {
			children++
		}
	}
	wantDocs := 1
	if stage == "reservation-append" {
		wantDocs = 0
	}
	if docs != wantDocs || children != map[bool]int{true: 1, false: 0}[stage == "held"] {
		t.Fatal("crash document/child witness", stage, docs, children)
	}
}

// Both attempts traverse the production queued-generation adapter and native
// storage. Append gates witness actual on-line preparation/fallback admission;
// goroutine launch order and elapsed absence do not establish wake ordering.
func TestTaskRecoveryGenerationPreparationFallbackWakeSequence(t *testing.T) {
	for _, timing := range []string{"no-self", "preexisting", "during-first", "during-fallback", "poison-fallback"} {
		t.Run(timing, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				var core *storeCore
				switch store := b.store.(type) {
				case *MemoryStorage:
					core = store.storeCore
				case *JournalStorage:
					core = store.storeCore
				}
				var requests atomic.Int64
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					requests.Add(1)
					taskHTTPRound(w, nil)
				}))
				defer server.Close()
				model := fakeModel(goai.ApiOpenAICompletions)
				model.BaseURL = server.URL
				model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
				registry := NewRegistry()
				reports := make(chan error, 4)
				h := openHarness(t, b.store, Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }, OnReport: func(err error) {
					select {
					case reports <- err:
					default:
					}
				}})
				firstRelease, fallbackRelease := make(chan struct{}), make(chan struct{})
				cleanupTaskGates(t, firstRelease, fallbackRelease)
				conv := root(t, h, ModelRef{model.Provider, model.ID})
				publishWake := func() {
					def := taskDefinition(t, "task.preparation.registry-wake", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
						return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
					})
					if _, err := registry.RegisterTask(def); err != nil {
						t.Fatal(err)
					}
				}
				if timing == "preexisting" {
					publishWake()
				}
				preparationFailure := reject("first preparation append rejected")
				fallbackFailure := reject("bounded fallback append rejected")
				type admissionWitness struct {
					runtime *TaskRuntime
					epoch   uint64
				}
				first := make(chan admissionWitness, 1)
				fallback := make(chan admissionWitness, 1)
				var original func(byte, uint64, uint64, []byte) error
				h.session.taskBookkeeping(func() { <-core.line; original = core.appendFrame; core.leave() })
				var firstAttempts, fallbackAttempts atomic.Int64
				installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
					if kind == 2 {
						var record commitRecord
						if err := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &record); err != nil {
							return err
						}
						for _, write := range record.Writes {
							if write.Task == nil || write.Task.Kind != "pi.generation" {
								continue
							}
							var cp generationCheckpoint
							if err := fromObject(write.Task.Checkpoint, &cp, h.session.limits); err != nil {
								return err
							}
							if cp.Phase == "intent" && cp.Attempt == 0 && firstAttempts.Add(1) == 1 {
								r := h.scheduler.invocations[write.Task.ID]
								first <- admissionWitness{runtime: r, epoch: r.admissionEpoch}
								<-firstRelease
								return preparationFailure
							}
							if cp.Phase == "terminal" && fallbackAttempts.Add(1) == 1 {
								r := h.scheduler.invocations[write.Task.ID]
								// Pause selection before this held admission leaves. The
								// next pass is explicitly released without changing epoch.
								h.scheduler.enabled.Store(false)
								fallback <- admissionWitness{runtime: r, epoch: r.admissionEpoch}
								<-fallbackRelease
								if timing == "poison-fallback" {
									return errors.New("uncertain fallback append")
								}
								return fallbackFailure
							}
						}
					}
					if original != nil {
						return original(kind, ordinal, high, payload)
					}
					return nil
				})
				sub, err := conv.Submit(bg, Input{Content: "queued preparation", RequestID: "fallback-sequence"})
				if err != nil {
					t.Fatal(err)
				}
				var firstWitness, fallbackWitness admissionWitness
				select {
				case firstWitness = <-first:
				case <-time.After(3 * time.Second):
					t.Fatal("first preparation admission missing")
				}
				if firstWitness.runtime == nil {
					t.Fatal("first admission has no actual invocation")
				}
				if timing == "during-first" {
					publishWake()
				}
				releaseTaskGate(firstRelease)
				select {
				case fallbackWitness = <-fallback:
				case <-time.After(3 * time.Second):
					t.Fatal("fallback admission missing")
				}
				if fallbackWitness.runtime != firstWitness.runtime || fallbackWitness.epoch != firstWitness.epoch {
					t.Fatal("fallback lost first failed attempt epoch", firstWitness.epoch, fallbackWitness.epoch)
				}
				if timing == "during-fallback" {
					publishWake()
				}
				releaseTaskGate(fallbackRelease)
				var reported error
				select {
				case reported = <-reports:
				case <-time.After(3 * time.Second):
					t.Fatal("fallback error not reported")
				}
				awaitTaskSignal(t, firstWitness.runtime.done)
				if timing == "poison-fallback" {
					if !errors.Is(reported, ErrPoisoned) || errors.Is(reported, preparationFailure) {
						t.Fatal("fallback poison replaced by preparation cause", reported)
					}
					if _, err := h.Snapshot(bg); !errors.Is(err, ErrPoisoned) {
						t.Fatal("fallback did not poison", err)
					}
					if requests.Load() != 0 || firstAttempts.Load() != 1 || fallbackAttempts.Load() != 1 {
						t.Fatal("poison dispatched/retried")
					}
					return
				}
				if !errors.Is(reported, fallbackFailure) || errors.Is(reported, preparationFailure) {
					t.Fatal("actual fallback error lost", reported)
				}
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				task := state.Tasks[firstWitness.runtime.taskID]
				if task.Status != "running" || taskHasDecidedOutcome(task) || len(state.Entries) != 0 || state.Submissions[sub.ID()].Status != "pending" || requests.Load() != 0 {
					t.Fatal("rejected sequence leaked effects/outcome", task, state.Submissions[sub.ID()])
				}
				for _, doc := range state.Documents {
					if doc.Kind == "pi.usage" {
						models, ok := doc.Value["models"].(map[string]any)
						if !ok || len(models) != 0 {
							t.Fatal("rejected preparation charged usage", doc.Value)
						}
					}
				}
				wantRetry := timing == "during-first" || timing == "during-fallback"
				h.session.taskBookkeeping(func() {
					if h.scheduler.retryAfter[task.ID] != firstWitness.epoch {
						t.Error("host return overwrote failed sequence epoch")
					}
					if h.scheduler.runnable(state, task) != wantRetry {
						t.Error("failed sequence wake permission", timing, h.scheduler.epoch.Load(), firstWitness.epoch)
					}
					// No Resume: this releases selection without granting a new wake.
					h.scheduler.enabled.Store(true)
				})
				if !wantRetry {
					for k := 0; k < 3; k++ {
						reservations, err := h.scheduler.reserve()

						discardUnstartedTaskReservations(t, h, reservations)
						if err != nil || len(reservations) != 0 {
							t.Fatal("self/preexisting wake retried rejected sequence", reservations, err)
						}
					}
					// A genuine public passive write (not an extra Resume) permits
					// retry after the witnessed rejected passes.
					if _, err := conv.Submit(bg, Input{Type: "write", Content: "later external progress"}); err != nil {
						t.Fatal(err)
					}
				} else {
					h.scheduler.kick()
				}
				// Ordinary Submission.Wait would add an epoch through Resume. Read
				// the adopted submission until terminal without granting permission.
				ctx, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				for {
					current, err := h.Snapshot(ctx)
					if err != nil {
						t.Fatal(err)
					}
					if terminalStatus(current.Submissions[sub.ID()].Status) {
						if current.Submissions[sub.ID()].Status != "done" {
							t.Fatal("retry failed", current.Submissions[sub.ID()])
						}
						assertTaskModelSpend(t, current, conv.ID(), model.Provider, model.ID, 5)
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal("retained wake did not permit retry", timing)
					default:
						runtime.Gosched()
					}
				}
				if requests.Load() != 1 || firstAttempts.Load() != 2 || fallbackAttempts.Load() != 2 {
					t.Fatal("bounded failure/retry effects", requests.Load(), firstAttempts.Load(), fallbackAttempts.Load())
				}
			})
		})
	}
}

func TestTaskRecoveryBoundReservationRejectionWaitCleanupNoRetry(t *testing.T) {
	for _, duringFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "later-wake", true: "wake-during-rejection"}[duringFailure], func(t *testing.T) {
			taskLimitedBackends(t, 2, func(t *testing.T, store Storage) {
				var core *storeCore
				if native, ok := store.(*MemoryStorage); ok {
					core = native.storeCore
				} else {
					core = store.(*JournalStorage).storeCore
				}
				callerEntered, letWait, detached, releaseCaller := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
				targetEntered, appendEntered, releaseAppend := make(chan struct{}), make(chan struct{}), make(chan struct{})
				var h *Harness
				var targetID ID
				var effects, attempts atomic.Int64
				reservationFailure := reject("bound reservation admission rejected")
				target := taskDefinition(t, "task.reservation.bound.target", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					effects.Add(1)
					close(targetEntered)
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("target"), nil })
				})
				caller := taskDefinition(t, "task.reservation.bound.caller", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					close(callerEntered)
					<-letWait
					if _, err := r.WaitForTask(ctx, targetID); !errors.Is(err, reservationFailure) {
						return fmt.Errorf("bound waiter rejection lost: %w", err)
					}
					close(detached)
					// No progress/return may rescue target after wait cleanup.
					<-releaseCaller
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("caller"), nil })
				})
				h = taskTestHarness(t, store, caller, target)
				cleanupTaskGates(t, letWait, releaseCaller, releaseAppend)
				callerID := createPublicTask(t, h, caller, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, callerEntered)
				h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(false) })
				targetID = createPublicTask(t, h, target, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				var original func(byte, uint64, uint64, []byte) error
				h.session.taskBookkeeping(func() { <-core.line; original = core.appendFrame; core.leave() })
				installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
					if kind == 2 {
						var record commitRecord
						if err := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &record); err != nil {
							return err
						}
						for _, write := range record.Writes {
							if write.Task != nil && write.Task.ID == targetID && write.Task.Status == "running" && attempts.Add(1) == 1 {
								h.scheduler.enabled.Store(false)
								close(appendEntered)
								<-releaseAppend
								return reservationFailure
							}
						}
					}
					if original != nil {
						return original(kind, ordinal, high, payload)
					}
					return nil
				})
				releaseTaskGate(letWait)
				ack, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				for {
					registered := false
					h.session.taskBookkeeping(func() {
						for wait := range h.scheduler.waiters {
							if wait.binding != nil && wait.binding.taskID == callerID && wait.target == targetID && h.scheduler.tickets[targetID][wait] {
								registered = true
							}
						}
					})
					if registered {
						break
					}
					select {
					case <-ack.Done():
						t.Fatal("actual bound ticket admission missing")
					default:
						runtime.Gosched()
					}
				}
				h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(true) })
				h.scheduler.kick()
				awaitTaskSignal(t, appendEntered)
				if duringFailure {
					if _, err := h.options.Registry.RegisterTask(target); err != nil {
						t.Fatal(err)
					}
				}
				releaseTaskGate(releaseAppend)
				awaitTaskSignal(t, detached)
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				if state.Tasks[targetID].Status != "pending" || effects.Load() != 0 || attempts.Load() != 1 {
					t.Fatal("rejected reservation started/retried target")
				}
				h.session.taskBookkeeping(func() {
					if len(h.scheduler.invocations) != 1 || len(h.scheduler.tickets) != 0 || len(h.scheduler.waiters) != 0 {
						t.Error("reservation rollback/wait cleanup incomplete")
					}
					if h.scheduler.runnable(state, state.Tasks[targetID]) != duringFailure {
						t.Error("failed reservation wake provenance", duringFailure)
					}
					h.scheduler.enabled.Store(true)
				})
				if !duringFailure {
					for k := 0; k < 3; k++ {
						reservations, err := h.scheduler.reserve()

						discardUnstartedTaskReservations(t, h, reservations)
						if err != nil || len(reservations) != 0 {
							t.Fatal("bound cleanup token retried failed target", reservations, err)
						}
					}
					if effects.Load() != 0 || attempts.Load() != 1 {
						t.Fatal("private selection bypassed barrier")
					}
					if _, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
						id, err := tx.MintID()
						if err != nil {
							return err
						}
						return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "app.genuine-wake", Value: JSON{}})
					}); err != nil {
						t.Fatal(err)
					}
				} else {
					h.scheduler.kick()
				}
				awaitTaskSignal(t, targetEntered)
				releaseTaskGate(releaseCaller)
				waitPublicTask(t, h, callerID)
				waitPublicTask(t, h, targetID)
				if effects.Load() != 1 || attempts.Load() != 2 {
					t.Fatal("reservation retry count", effects.Load(), attempts.Load())
				}
			})
		})
	}
}

func TestTaskRecoveryRejectedReconcileIgnoresStaleWakeAndRetainsNewWake(t *testing.T) {
	for _, duringFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "later-wake", true: "wake-during-final"}[duringFailure], func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				childEntered, releaseChild, appendEntered, releaseAppend := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
				var parentCalls, childCalls, finalAttempts atomic.Int64
				reports := make(chan error, 4)
				child := taskDefinition(t, "task.reconcile.retry.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					childCalls.Add(1)
					close(childEntered)
					<-releaseChild
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				parent := taskDefinition(t, "task.reconcile.retry.parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					parentCalls.Add(1)
					if err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
						_, err := tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
						return nil, err
					}); err != nil {
						return err
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("held"), nil })
				})
				h := taskTestHarnessOptions(t, b.store, Options{OnReport: func(err error) {
					select {
					case reports <- err:
					default:
					}
				}}, parent, child)
				cleanupTaskGates(t, releaseChild, releaseAppend)
				parentID := createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, childEntered)
				observeTaskState(t, h, parentID, "completing")
				// Exclude a delayed successful parent return as an unobserved
				// genuine wake AFTER the rejected Final attempt's baseline.
				var parentDone <-chan struct{}
				h.session.taskBookkeeping(func() {
					if r := h.scheduler.invocations[parentID]; r != nil {
						parentDone = r.done
					}
				})
				if parentDone != nil {
					awaitTaskSignal(t, parentDone)
				}
				var core *storeCore
				if native, ok := b.store.(*MemoryStorage); ok {
					core = native.storeCore
				} else {
					core = b.store.(*JournalStorage).storeCore
				}
				var original func(byte, uint64, uint64, []byte) error
				h.session.taskBookkeeping(func() { <-core.line; original = core.appendFrame; core.leave() })
				failure := reject("reconcile final rejected")
				installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
					if kind == 2 {
						var record commitRecord
						if err := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &record); err != nil {
							return err
						}
						for _, write := range record.Writes {
							if write.Task != nil && write.Task.ID == parentID && write.Task.Status == "done" && finalAttempts.Add(1) == 1 {
								h.scheduler.enabled.Store(false)
								close(appendEntered)
								<-releaseAppend
								return failure
							}
						}
					}
					if original != nil {
						return original(kind, ordinal, high, payload)
					}
					return nil
				})
				releaseTaskGate(releaseChild)
				awaitTaskSignal(t, appendEntered)
				if duringFailure {
					if _, err := h.options.Registry.RegisterTask(parent); err != nil {
						t.Fatal(err)
					}
				}
				releaseTaskGate(releaseAppend)
				select {
				case err := <-reports:
					if !errors.Is(err, failure) {
						t.Fatal("reconcile cause", err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("reconcile failure not reported")
				}
				state, err := h.Snapshot(bg)
				if err != nil || state.Tasks[parentID].Status != "completing" {
					t.Fatal("rejected reconcile adopted subset", err, state.Tasks[parentID])
				}
				if !duringFailure {
					for k := 0; k < 3; k++ {
						if err := h.scheduler.reconcile(); !errors.Is(err, errTaskPassAwaitingWake) {
							t.Fatal("self/stale token retried failed reconcile", err)
						}
					}
					if finalAttempts.Load() != 1 {
						t.Fatal("reconcile appended before genuine wake")
					}
					if _, err := h.options.Registry.RegisterTask(parent); err != nil {
						t.Fatal(err)
					}
				}
				// Same private pass used by scheduler; no Resume/host invocation can
				// donate an unobserved epoch to this retained deterministic Final.
				if err := h.scheduler.reconcile(); err != nil {
					t.Fatal("genuine wake lost", err)
				}
				final, err := h.Snapshot(bg)
				if err != nil || final.Tasks[parentID].Status != "done" {
					t.Fatal("retained final not adopted", err)
				}
				if parentCalls.Load() != 1 || childCalls.Load() != 1 || finalAttempts.Load() != 2 {
					t.Fatal("reconcile effects/count", parentCalls.Load(), childCalls.Load(), finalAttempts.Load())
				}
			})
		})
	}
}

func TestTaskRecoveryOfflineGenerationPreparationFallbackWakeSequence(t *testing.T) {
	for _, timing := range []string{"no-self", "preexisting", "during-offline", "poison-fallback"} {
		t.Run(timing, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				var core *storeCore
				if native, ok := b.store.(*MemoryStorage); ok {
					core = native.storeCore
				} else {
					core = b.store.(*JournalStorage).storeCore
				}
				type witness struct {
					runtime *TaskRuntime
					epoch   uint64
				}
				offline, fallback := make(chan witness, 1), make(chan witness, 1)
				releaseOffline, releaseFallback := make(chan struct{}), make(chan struct{})
				var h *Harness
				var preparations, requests, fallbackAttempts atomic.Int64
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); taskHTTPRound(w, nil) }))
				defer server.Close()
				model := fakeModel(goai.ApiOpenAICompletions)
				model.BaseURL = server.URL
				model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
				registry := NewRegistry()
				reports := make(chan error, 4)
				h = openHarness(t, b.store, Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }, RequestOptions: func(context.Context, ModelRef) (*goai.StreamOptions, error) {
					if preparations.Add(1) == 1 {
						var admitted witness
						h.session.taskBookkeeping(func() {
							for _, r := range h.scheduler.invocations {
								admitted = witness{r, r.admissionEpoch}
								break
							}
						})
						offline <- admitted
						<-releaseOffline
						return nil, errors.New("offline request options rejected")
					}
					return nil, nil
				}, OnReport: func(err error) {
					select {
					case reports <- err:
					default:
					}
				}})
				cleanupTaskGates(t, releaseOffline, releaseFallback)
				conv := root(t, h, ModelRef{model.Provider, model.ID})
				publishWake := func() {
					def := taskDefinition(t, "task.offline.registry-wake", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
						return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
					})
					if _, err := registry.RegisterTask(def); err != nil {
						t.Fatal(err)
					}
				}
				if timing == "preexisting" {
					publishWake()
				}
				failure := reject("offline preparation fallback rejected")
				var original func(byte, uint64, uint64, []byte) error
				h.session.taskBookkeeping(func() { <-core.line; original = core.appendFrame; core.leave() })
				installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
					if kind == 2 {
						var record commitRecord
						if err := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &record); err != nil {
							return err
						}
						for _, write := range record.Writes {
							if write.Task == nil || write.Task.Kind != "pi.generation" {
								continue
							}
							var cp generationCheckpoint
							if err := fromObject(write.Task.Checkpoint, &cp, h.session.limits); err != nil {
								return err
							}
							if cp.Phase == "terminal" && fallbackAttempts.Add(1) == 1 {
								h.scheduler.enabled.Store(false)
								r := h.scheduler.invocations[write.Task.ID]
								fallback <- witness{r, r.admissionEpoch}
								<-releaseFallback
								if timing == "poison-fallback" {
									return errors.New("uncertain offline fallback")
								}
								return failure
							}
						}
					}
					if original != nil {
						return original(kind, ordinal, high, payload)
					}
					return nil
				})
				sub, err := conv.Submit(bg, Input{Content: "offline preparation", RequestID: "offline-sequence"})
				if err != nil {
					t.Fatal(err)
				}
				var first, second witness
				select {
				case first = <-offline:
				case <-time.After(3 * time.Second):
					t.Fatal("offline preparation barrier missing")
				}
				if first.runtime == nil || first.epoch == 0 {
					t.Fatal("offline attempt lacks on-line baseline", first.epoch)
				}
				if timing == "during-offline" {
					publishWake()
				}
				releaseTaskGate(releaseOffline)
				select {
				case second = <-fallback:
				case <-time.After(3 * time.Second):
					t.Fatal("offline fallback admission missing")
				}
				if second.runtime != first.runtime || second.epoch != first.epoch {
					t.Fatal("offline fallback baseline overwritten", first.epoch, second.epoch)
				}
				releaseTaskGate(releaseFallback)
				var reported error
				select {
				case reported = <-reports:
				case <-time.After(3 * time.Second):
					t.Fatal("offline fallback cause missing")
				}
				awaitTaskSignal(t, first.runtime.done)
				if timing == "poison-fallback" {
					if !errors.Is(reported, ErrPoisoned) {
						t.Fatal("offline poison replaced", reported)
					}
					if requests.Load() != 0 || preparations.Load() != 1 {
						t.Fatal("offline poison self retry")
					}
					return
				}
				if !errors.Is(reported, failure) {
					t.Fatal("offline preparation error hid fallback", reported)
				}
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				if len(state.Entries) != 0 || taskHasDecidedOutcome(state.Tasks[first.runtime.taskID]) || state.Submissions[sub.ID()].Status != "pending" || requests.Load() != 0 {
					t.Fatal("offline rejected fallback leaked effects")
				}
				wantRetry := timing == "during-offline"
				h.session.taskBookkeeping(func() {
					if h.scheduler.retryAfter[first.runtime.taskID] != first.epoch || h.scheduler.runnable(state, state.Tasks[first.runtime.taskID]) != wantRetry {
						t.Error("offline wake provenance", timing)
					}
					h.scheduler.enabled.Store(true)
				})
				if !wantRetry {
					for k := 0; k < 3; k++ {
						reservations, err := h.scheduler.reserve()

						discardUnstartedTaskReservations(t, h, reservations)
						if err != nil || len(reservations) != 0 {
							t.Fatal("offline failed attempt retried on old token", reservations, err)
						}
					}
					publishWake()
				} else {
					h.scheduler.kick()
				}
				ctx, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				for {
					current, err := h.Snapshot(ctx)
					if err != nil {
						t.Fatal(err)
					}
					if terminalStatus(current.Submissions[sub.ID()].Status) {
						if current.Submissions[sub.ID()].Status != "done" {
							t.Fatal(current.Submissions[sub.ID()])
						}
						assertTaskModelSpend(t, current, conv.ID(), model.Provider, model.ID, 5)
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal("offline wake lost")
					default:
						runtime.Gosched()
					}
				}
				if requests.Load() != 1 || fallbackAttempts.Load() != 2 || preparations.Load() != 3 {
					t.Fatal("offline retry counts", requests.Load(), fallbackAttempts.Load(), preparations.Load())
				}
			})
		})
	}
}

func TestTaskRecoveryProvisionalReservationRollbackBeforeFrontierAdmission(t *testing.T) {
	taskLimitedBackends(t, 2, func(t *testing.T, store Storage) {
		callerEntered, releaseCaller, appendEntered, releaseAppend := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
		reservationResult := make(chan error, 1)
		var h *Harness
		var callerRuntime *TaskRuntime
		var effects atomic.Int64
		caller := taskDefinition(t, "task.rollback.frontier.caller", func(_ context.Context, _ TaskRecord, r *TaskRuntime) error {
			callerRuntime = r
			close(callerEntered)
			<-releaseCaller
			return nil
		})
		pending := taskDefinition(t, "task.rollback.frontier.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			effects.Add(1)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		h = taskTestHarnessOptions(t, store, Options{OnReport: func(err error) {
			select {
			case reservationResult <- err:
			default:
			}
		}}, caller, pending)
		cleanupTaskGates(t, releaseCaller, releaseAppend)
		createPublicTask(t, h, caller, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, callerEntered)
		h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(false) })
		var parentID, childID ID
		_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			parentID, err = tx.MintID()
			if err != nil {
				return err
			}
			childID, err = tx.MintID()
			if err != nil {
				return err
			}
			parent := foundationTask(parentID, nil)
			child := foundationTask(childID, nil)
			child.Kind = pending.Kind()
			child.Owner = parentID
			if err := tx.stage(Write{Op: "put-task", Task: &parent}); err != nil {
				return err
			}
			return tx.stage(Write{Op: "put-task", Task: &child})
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
			parent, err := copyTask(tx.state.Tasks[parentID], tx.limits)
			if err != nil {
				return err
			}
			parent.Status = "completing"
			parent.Execution.Native.State = TaskState{Status: "completing", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: "held"}}}
			return tx.stage(Write{Op: "put-task", Task: &parent})
		})
		if err != nil {
			t.Fatal(err)
		}
		var core *storeCore
		if native, ok := store.(*MemoryStorage); ok {
			core = native.storeCore
		} else {
			core = store.(*JournalStorage).storeCore
		}
		var original func(byte, uint64, uint64, []byte) error
		h.session.taskBookkeeping(func() { <-core.line; original = core.appendFrame; core.leave() })
		failure := reject("provisional child reservation rejected")
		var attempts atomic.Int64
		installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
			if kind == 2 {
				var record commitRecord
				if err := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &record); err != nil {
					return err
				}
				for _, write := range record.Writes {
					if write.Task != nil && write.Task.ID == childID && write.Task.Status == "running" && attempts.Add(1) == 1 {
						h.scheduler.enabled.Store(false)
						if h.scheduler.invocations[childID] == nil {
							return errors.New("reservation not installed before storage")
						}
						close(appendEntered)
						<-releaseAppend
						return failure
					}
				}
			}
			if original != nil {
				return original(kind, ordinal, high, payload)
			}
			return nil
		})
		h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(true) })
		h.scheduler.kick()
		awaitTaskSignal(t, appendEntered)
		// B's provisional registration exists under the held storage admission.
		// Queue A's frontier read, then release rejection. Every possible line
		// ordering after release must see rollback already completed.
		started := make(chan struct{})
		type observed struct {
			err             error
			provisional     bool
			claims, waiters int
		}
		frontierResult := make(chan observed, 1)
		go func() {
			close(started)
			result := observed{}
			result.err = h.session.readTasks(bg, func(state Snapshot) error {
				result.provisional = h.scheduler.invocations[childID] != nil
				wait := &taskWait{signal: make(chan struct{}, 1), binding: callerRuntime, target: parentID}
				err := h.scheduler.admitBoundWait(state, wait, parentID)
				if err == nil {
					h.scheduler.waiters[wait] = true
				}
				result.claims = len(wait.claims)
				result.waiters = len(h.scheduler.waiters)
				h.scheduler.releaseWaitClaims(wait)
				delete(h.scheduler.waiters, wait)
				return err
			})
			frontierResult <- result
		}()
		awaitTaskSignal(t, started)
		releaseTaskGate(releaseAppend)
		select {
		case err := <-reservationResult:
			if !errors.Is(err, failure) {
				t.Fatal("reservation cause", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("reservation rollback missing")
		}
		select {
		case result := <-frontierResult:
			var yield *InvocationWaitRequiresYield
			if result.provisional || result.claims != 0 || result.waiters != 0 || !errors.As(result.err, &yield) {
				t.Fatal("frontier saw rejected provisional join", result)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("frontier admission missing")
		}
		if attempts.Load() != 1 || effects.Load() != 0 {
			t.Fatal("rejected frontier work dispatched")
		}
		releaseTaskGate(releaseCaller)
	})
}

func TestTaskRecoveryHandoverMissingIncompatibleReportsBounded(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		const phases = 6
		entered, releases := make([]chan struct{}, phases), make([]chan struct{}, phases)
		for k := range entered {
			entered[k] = make(chan struct{})
			releases[k] = make(chan struct{})
		}
		reports := make(chan error, 8)
		var replacementCalls atomic.Int64
		phaseMap := map[string]TaskPhase{}
		for k := 0; k < phases; k++ {
			k := k
			phaseMap[fmt.Sprintf("phase%d", k)] = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				close(entered[k])
				<-releases[k]
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					if k == phases-1 {
						return taskDone("old retained"), nil
					}
					return &TaskState{Status: "running", Checkpoint: JSON{"phase": fmt.Sprintf("phase%d", k+1)}}, nil
				})
			}
		}
		options := TaskDefinitionOptions{Kind: "task.handover.reports", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "phase0"}, nil }, Phases: phaseMap, Abort: taskAbort}
		old, err := DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarnessOptions(t, b.store, Options{OnReport: func(err error) { reports <- err }}, old)
		cleanupTaskGates(t, releases...)
		dispose, err := h.options.Registry.RegisterTask(old)
		if err != nil {
			t.Fatal(err)
		}
		id := createPublicTask(t, h, old, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered[0])
		dispose()
		releaseTaskGate(releases[0])
		awaitTaskSignal(t, entered[1])
		// Same missing resolution across another phase emits no second report.
		releaseTaskGate(releases[1])
		awaitTaskSignal(t, entered[2])
		incompatibleOptions := options
		incompatibleOptions.Version = 2
		incompatibleOptions.Phases = map[string]TaskPhase{}
		for name := range phaseMap {
			incompatibleOptions.Phases[name] = func(context.Context, TaskRecord, *TaskRuntime) error {
				replacementCalls.Add(1)
				return errors.New("incompatible replacement dispatched")
			}
		}
		bad, err := DefineTask(incompatibleOptions)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.options.Registry.RegisterTask(bad); err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(releases[2])
		awaitTaskSignal(t, entered[3])
		// Same definition token republished, still one incompatible report.
		if _, err := h.options.Registry.RegisterTask(bad); err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(releases[3])
		awaitTaskSignal(t, entered[4])
		newBad, err := DefineTask(incompatibleOptions)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.options.Registry.RegisterTask(newBad); err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(releases[4])
		awaitTaskSignal(t, entered[5])
		releaseTaskGate(releases[5])
		record := waitPublicTask(t, h, id)
		if record.Version != 1 || record.State.Outcome.Result.Value != "old retained" || replacementCalls.Load() != 0 {
			t.Fatal("unfitting replacement changed invocation", record)
		}
		// Report delivery precedes the next real phase callback, hence this
		// terminal read witnesses all reported boundary decisions without sleep.
		for k, code := range []string{"missing_task", "incompatible_task", "incompatible_task"} {
			select {
			case err := <-reports:
				if err.Error() != reject(code).Error() {
					t.Fatal("handover report", k, err)
				}
			default:
				t.Fatal("handover report missing", k)
			}
		}
		select {
		case extra := <-reports:
			t.Fatal("same token/resolution reported again", extra)
		default:
		}
	})
}

func TestTaskRecoveryToolDecidingHoldRejectedUncertainAndReopen(t *testing.T) {
	for _, backendName := range []string{"memory", "journal"} {
		for _, failure := range []string{"rejected-fallback-adopts", "rejected-fallback-rejected", "uncertain-before", "uncertain-after"} {
			t.Run(backendName+"/"+failure, func(t *testing.T) {
				image := &MemoryImage{}
				directory := t.TempDir() + "/private"
				open := func() Storage {
					var store Storage
					var err error
					if backendName == "memory" {
						store, err = OpenMemory(MemoryOptions{Image: image})
					} else {
						store, err = OpenJournal(directory, JournalOptions{})
					}
					if err != nil {
						t.Fatal(err)
					}
					return store
				}
				store := open()
				var core *storeCore
				if native, ok := store.(*MemoryStorage); ok {
					core = native.storeCore
				} else {
					core = store.(*JournalStorage).storeCore
				}
				toolEntered, releaseTool, childEntered := make(chan struct{}), make(chan struct{}), make(chan struct{})
				var childCalls, requests, effects, callbacks atomic.Int64
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					if requests.Add(1) == 1 {
						taskHTTPRound(w, []string{"deciding-hold"})
					} else {
						taskHTTPRound(w, nil)
					}
				}))
				defer server.Close()
				model := fakeModel(goai.ApiOpenAICompletions)
				model.BaseURL = server.URL
				model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
				registry := NewRegistry()
				var toolID, docID ID
				child := taskDefinition(t, "task.deciding.hold.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					if childCalls.Add(1) == 1 {
						close(childEntered)
						<-ctx.Done()
						return ctx.Err()
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				if _, err := registry.RegisterTask(child); err != nil {
					t.Fatal(err)
				}
				if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "deciding-hold", Description: "atomic hold", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "deciding.hold", Version: 1, ReplaySafe: false, Execute: func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
					effects.Add(1)
					toolID = api.TaskID()
					if err := api.Output("known-prefix"); err != nil {
						return ToolResult{}, err
					}
					if _, err := api.CreateTask(ctx, child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: toolID}}); err != nil {
						return ToolResult{}, err
					}
					close(toolEntered)
					<-releaseTool
					return ToolResult{Content: "returned success", Details: JSON{"candidate": true}, Usage: &goai.Usage{Input: 1, Output: 2, TotalTokens: 3}, Commit: func(tx *Tx) error {
						callbacks.Add(1)
						doc, err := tx.Document(docID)
						if err != nil {
							return err
						}
						return doc.Set(JSON{"sum": 9})
					}}, nil
				}}); err != nil {
					t.Fatal(err)
				}
				reports := make(chan error, 4)
				options := Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }, OnReport: func(err error) {
					select {
					case reports <- err:
					default:
					}
				}}
				h := openHarness(t, store, options)
				cleanupTaskGates(t, releaseTool)
				conv := root(t, h, ModelRef{model.Provider, model.ID})
				docID = createAppDocument(t, conv)
				sub, err := conv.Submit(bg, Input{Content: "atomic deciding Hold", RequestID: "deciding-hold"})
				if err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, toolEntered)
				awaitTaskSignal(t, childEntered)
				var original func(byte, uint64, uint64, []byte) error
				h.session.taskBookkeeping(func() { <-core.line; original = core.appendFrame; core.leave() })
				var decidingAttempts atomic.Int64
				installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
					if kind == 2 {
						var record commitRecord
						if err := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &record); err != nil {
							return err
						}
						for _, write := range record.Writes {
							if write.Task == nil || write.Task.ID != toolID || !taskHasDecidedOutcome(*write.Task) {
								continue
							}
							attempt := decidingAttempts.Add(1)
							if attempt == 1 {
								h.scheduler.enabled.Store(false)
								if failure == "uncertain-after" && original != nil {
									if err := original(kind, ordinal, high, payload); err != nil {
										return err
									}
								}
								if strings.HasPrefix(failure, "rejected") {
									return reject("tool deciding Hold rejected")
								}
								return errors.New("uncertain tool deciding Hold")
							}
							if failure == "rejected-fallback-rejected" && attempt == 2 {
								return reject("tool bounded fallback rejected")
							}
						}
					}
					if original != nil {
						return original(kind, ordinal, high, payload)
					}
					return nil
				})
				var toolDone <-chan struct{}
				h.session.taskBookkeeping(func() { toolDone = h.scheduler.invocations[toolID].done })
				releaseTaskGate(releaseTool)
				awaitTaskSignal(t, toolDone)
				if callbacks.Load() != 1 || effects.Load() != 1 || requests.Load() != 1 {
					t.Fatal("deciding callback/effect/retry count", callbacks.Load(), effects.Load(), requests.Load())
				}
				fallbackAdopted := failure == "rejected-fallback-adopts"
				if strings.HasPrefix(failure, "rejected") {
					state, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					if !equalJSONValue(state.Documents[docID].Value["sum"], 0) || taskHasDecidedOutcome(state.Tasks[toolID]) != fallbackAdopted {
						t.Fatal("rejected deciding leaked successful callback/Hold")
					}
					assertTaskToolSpendOrAbsent(t, state, conv.ID(), "deciding-hold", map[bool]int{true: 3, false: 0}[fallbackAdopted])
					var receipts int
					for _, entry := range state.Entries {
						if entry.ByTask == toolID {
							receipts++
						}
					}
					if receipts != map[bool]int{true: 1, false: 0}[fallbackAdopted] {
						t.Fatal("rejected deciding receipt placement", receipts)
					}
					if !fallbackAdopted {
						select {
						case err := <-reports:
							if err.Error() != reject("tool bounded fallback rejected").Error() {
								t.Fatal(err)
							}
						default:
							t.Fatal("fallback rejection cause missing")
						}
					}
				} else {
					select {
					case err := <-reports:
						if !errors.Is(err, ErrPoisoned) {
							t.Fatal("uncertain deciding cause", err)
						}
					default:
						t.Fatal("uncertain deciding poison missing")
					}
				}
				if err := h.Close(bg); err != nil {
					t.Fatal(err)
				}
				second := openHarness(t, open(), options)
				opened, err := second.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				confirmedSuccess := backendName == "journal" && failure == "uncertain-after"
				confirmedHold := confirmedSuccess || fallbackAdopted
				if taskHasDecidedOutcome(opened.Tasks[toolID]) != confirmedHold || effects.Load() != 1 || callbacks.Load() != 1 {
					t.Fatal("deciding confirmed prefix/Open replay")
				}
				wantSum, wantSpend := 0, 0
				if confirmedSuccess {
					wantSum = 9
				}
				if confirmedHold {
					wantSpend = 3
				}
				if !equalJSONValue(opened.Documents[docID].Value["sum"], wantSum) {
					t.Fatal("deciding app adoption prefix", opened.Documents[docID])
				}
				assertTaskToolSpendOrAbsent(t, opened, conv.ID(), "deciding-hold", wantSpend)
				if settled := waitSubmission(t, &SubmissionHandle{h: second, id: sub.ID()}); settled.Submission.Status != "done" {
					t.Fatal(settled)
				}
				final, err := second.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				assertTaskToolSpendOrAbsent(t, final, conv.ID(), "deciding-hold", wantSpend)
				var cp toolCheckpoint
				if err := fromObject(final.Tasks[toolID].Checkpoint, &cp, second.session.limits); err != nil {
					t.Fatal(err)
				}
				wantCode := "interrupted"
				if fallbackAdopted {
					wantCode = "tool_outcome_rejected"
				}
				if confirmedSuccess {
					wantCode = ""
				}
				wantText := "known-prefix\n" + wantCode
				if confirmedSuccess {
					wantText = "known-prefixreturned success"
				}
				if cp.Result == nil || len(cp.Result.Content) != 1 || cp.Result.Content[0].Text != wantText || cp.Result.ToolCallID != cp.CallID || cp.Result.ToolName != cp.Offer.Name || cp.Result.IsError != (wantCode != "") {
					t.Fatal("deciding confirmed receipt content/identity", cp.Result, wantText)
				}
				if cp.ErrorCode != wantCode || cp.Result == nil || effects.Load() != 1 || callbacks.Load() != 1 || requests.Load() != 2 || !equalJSONValue(final.Documents[docID].Value["sum"], wantSum) {
					t.Fatal("deciding recovery duplicated host/app/receipt", cp.ErrorCode, wantCode)
				}
			})
		}
	}
}

func assertTaskToolSpendOrAbsent(t *testing.T, state Snapshot, conversation ID, name string, total int) {
	t.Helper()
	if total != 0 {
		assertTaskToolSpend(t, state, conversation, name, total)
		return
	}
	for _, doc := range state.Documents {
		if doc.Kind == "pi.usage" && doc.Owner == conversation && !doc.Retired {
			tools, ok := doc.Value["tools"].(map[string]any)
			if !ok {
				t.Fatal("tools usage shape")
			}
			if _, present := tools[name]; present {
				t.Fatal("unknown/rejected usage fabricated", tools[name])
			}
			return
		}
	}
	t.Fatal("usage document missing")
}

func TestTaskRecoveryHandoverIdentityMemoMigrationAndNoOverlap(t *testing.T) {
	for _, variant := range []string{"same-version", "newer-migrates", "newer-migration-fails"} {
		t.Run(variant, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				entered, releasePhase, progressAdopted, releaseOldHost := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
				var active, peak, oldCalls, newCalls, migrations atomic.Int64
				var oldRuntime *TaskRuntime
				reports := make(chan error, 4)
				enter := func() {
					n := active.Add(1)
					for {
						p := peak.Load()
						if n <= p || peak.CompareAndSwap(p, n) {
							break
						}
					}
				}
				old, err := DefineTask(TaskDefinitionOptions{Kind: "task.handover.identity", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "a"}, nil }, Phases: map[string]TaskPhase{
					"a": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
						enter()
						defer active.Add(-1)
						oldCalls.Add(1)
						oldRuntime = r
						if _, err := r.MemoCandidate(ctx, "picked", "old"); err != nil {
							return err
						}
						close(entered)
						<-releasePhase
						if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
							return &TaskState{Status: "running", Checkpoint: JSON{"phase": "b"}}, nil
						}); err != nil {
							return err
						}
						close(progressAdopted)
						<-releaseOldHost
						return nil
					},
					"b": func(context.Context, TaskRecord, *TaskRuntime) error {
						oldCalls.Add(1)
						return errors.New("old next phase dispatched after fitting replacement")
					},
				}, Abort: taskAbort})
				if err != nil {
					t.Fatal(err)
				}
				h := taskTestHarnessOptions(t, b.store, Options{OnReport: func(err error) {
					select {
					case reports <- err:
					default:
					}
				}}, old)
				cleanupTaskGates(t, releasePhase, releaseOldHost)
				id := createPublicTask(t, h, old, JSON{"source": "input"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, entered)
				options := old.options
				options.Phases = map[string]TaskPhase{"a": func(context.Context, TaskRecord, *TaskRuntime) error {
					return errors.New("replacement restarted old phase")
				}, "b": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
					enter()
					defer active.Add(-1)
					newCalls.Add(1)
					winner, err := r.MemoCandidate(ctx, "picked", "new")
					if err != nil {
						return err
					}
					if winner != "old" {
						return errors.New("handover erased memo")
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return taskDone(JSON{"picked": winner, "version": task.Version}), nil
					})
				}}
				if variant != "same-version" {
					options.Version = 2
					options.Migrate = func(input any, checkpoint JSON, _ uint64) (any, JSON, error) {
						migrations.Add(1)
						if variant == "newer-migration-fails" {
							return nil, nil, errors.New("handover migration rejected")
						}
						return input, checkpoint, nil
					}
				}
				replacement, err := DefineTask(options)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := h.options.Registry.RegisterTask(replacement); err != nil {
					t.Fatal(err)
				}
				releaseTaskGate(releasePhase)
				awaitTaskSignal(t, progressAdopted)
				// Real old host remains active AFTER progress adoption. A fitting
				// registry publication cannot run replacement before its return.
				state, ok, err := h.Task(bg, id)
				if err != nil || !ok || state.State.Checkpoint["phase"] != "b" || newCalls.Load() != 0 || active.Load() != 1 {
					t.Fatal("phase overlap/handover boundary", state, err)
				}
				releaseTaskGate(releaseOldHost)
				if variant == "newer-migration-fails" {
					select {
					case <-reports:
					case <-time.After(3 * time.Second):
						t.Fatal("handover migration failure missing")
					}
					blocked, ok, err := h.Task(bg, id)
					if err != nil || !ok || blocked.Version != 1 || blocked.State.Status != "pending" || blocked.State.Checkpoint["phase"] != "b" || newCalls.Load() != 0 || migrations.Load() != 1 {
						t.Fatal("failed handover migration changed task", blocked, err)
					}
					inspection, err := h.InspectTasks(bg)
					if err != nil || len(inspection.Tasks) != 1 || inspection.Tasks[0].Reason != "migration_failed" {
						t.Fatal(inspection, err)
					}
				} else {
					final := waitPublicTask(t, h, id)
					wantVersion := uint64(1)
					if variant == "newer-migrates" {
						wantVersion = 2
					}
					if final.Version != wantVersion || newCalls.Load() != 1 || oldCalls.Load() != 1 || peak.Load() != 1 {
						t.Fatal("handover identity/overlap", final, newCalls.Load(), oldCalls.Load(), peak.Load())
					}
				}
				if err := oldRuntime.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("late"), nil }); !errors.Is(err, ErrSealed) {
					t.Fatal("old invocation escaped handover", err)
				}
			})
		})
	}
}

// Native bounded-retry adaptation of pinned harness-tasks:746: a rejected
// scheduler fault retries on a genuine later wake, never its own cleanup token.
func TestTaskRecoveryRejectedSchedulerFaultRunAbortLaterWake(t *testing.T) {
	for _, mode := range []string{"run-throw", "run-no-progress", "abort-throw", "abort-no-progress"} {
		for _, duringFailure := range []bool{false, true} {
			t.Run(mode+"/"+map[bool]string{false: "later-wake", true: "during-fault"}[duringFailure], func(t *testing.T) {
				backends(t, func(t *testing.T, b backend) {
					var runs, aborts, attempts atomic.Int64
					failure := reject("scheduler fault append rejected")
					entered, release := make(chan struct{}), make(chan struct{})
					reports := make(chan error, 4)
					phase := func(context.Context, TaskRecord, *TaskRuntime) error {
						runs.Add(1)
						if mode == "run-throw" {
							return errors.New("private run failure")
						}
						return nil
					}
					def := taskDefinition(t, "task.fault.retry", phase)
					options := def.options
					options.Abort = func(context.Context, TaskRecord, *TaskRuntime) error {
						aborts.Add(1)
						if mode == "abort-throw" {
							return errors.New("private abort failure")
						}
						return nil
					}
					var err error
					def, err = DefineTask(options)
					if err != nil {
						t.Fatal(err)
					}
					h := taskTestHarnessOptions(t, b.store, Options{OnReport: func(err error) {
						select {
						case reports <- err:
						default:
						}
					}}, def)
					cleanupTaskGates(t, release)
					id := createPublicTask(t, h, def, JSON{"input": true}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
					var docID ID
					_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
						var err error
						docID, err = tx.MintID()
						if err != nil {
							return err
						}
						_, err = tx.CreateDocument(Document{ID: docID, Scope: "task", Owner: id, Kind: "app.fault", Version: 1, Value: JSON{"alive": true}})
						if err != nil {
							return err
						}
						if strings.HasPrefix(mode, "abort") {
							task := markTask(tx.state.Tasks[id])
							return tx.stage(Write{Op: "put-task", Task: &task})
						}
						return nil
					})
					if err != nil {
						t.Fatal(err)
					}
					var core *storeCore
					if native, ok := b.store.(*MemoryStorage); ok {
						core = native.storeCore
					} else {
						core = b.store.(*JournalStorage).storeCore
					}
					var original func(byte, uint64, uint64, []byte) error
					h.session.taskBookkeeping(func() { <-core.line; original = core.appendFrame; core.leave() })
					var failedRuntime *TaskRuntime
					var faultEpoch uint64
					installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
						if kind == 2 {
							var record commitRecord
							if err := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &record); err != nil {
								return err
							}
							for _, write := range record.Writes {
								if write.Task != nil && write.Task.ID == id && taskHasDecidedOutcome(*write.Task) && write.Task.Execution.Native.State.Outcome.Status == "faulted" && attempts.Add(1) == 1 {
									h.scheduler.enabled.Store(false)
									failedRuntime = h.scheduler.invocations[id]
									faultEpoch = failedRuntime.admissionEpoch
									close(entered)
									<-release
									return failure
								}
							}
						}
						if original != nil {
							return original(kind, ordinal, high, payload)
						}
						return nil
					})
					if err := h.Resume(bg); err != nil {
						t.Fatal(err)
					}
					awaitTaskSignal(t, entered)
					if duringFailure {
						if _, err := h.options.Registry.RegisterTask(def); err != nil {
							t.Fatal(err)
						}
					}
					releaseTaskGate(release)
					select {
					case err := <-reports:
						if !errors.Is(err, failure) {
							t.Fatal("fault rejection cause", err)
						}
					case <-time.After(3 * time.Second):
						t.Fatal("fault rejection not reported")
					}
					awaitTaskSignal(t, failedRuntime.done)
					state, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					if state.Tasks[id].Status != "running" || taskHasDecidedOutcome(state.Tasks[id]) || state.Documents[docID].Retired || len(state.Entries) != 0 {
						t.Fatal("rejected scheduler fault leaked terminal/retirement", state.Tasks[id])
					}
					h.session.taskBookkeeping(func() {
						if h.scheduler.retryAfter[id] != faultEpoch || h.scheduler.runnable(state, state.Tasks[id]) != duringFailure {
							t.Error("fault retry epoch", mode, duringFailure)
						}
						h.scheduler.enabled.Store(true)
					})
					if !duringFailure {
						for k := 0; k < 3; k++ {
							reservations, err := h.scheduler.reserve()
							discardUnstartedTaskReservations(t, h, reservations)
							if err != nil || len(reservations) != 0 {
								t.Fatal("fault cleanup self-retry", reservations, err)
							}
						}
						if attempts.Load() != 1 || runs.Load()+aborts.Load() != 1 {
							t.Fatal("rejected fault repeated handler before new wake")
						}
						if _, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
							id, err := tx.MintID()
							if err != nil {
								return err
							}
							return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "app.fault-wake", Value: JSON{}})
						}); err != nil {
							t.Fatal(err)
						}
					} else {
						h.scheduler.kick()
					}
					// Snapshot-only completion avoids a Wait/Resume-created epoch.
					ctx, cancel := context.WithTimeout(bg, 3*time.Second)
					defer cancel()
					for {
						current, err := h.Snapshot(ctx)
						if err != nil {
							t.Fatal(err)
						}
						if terminalStatus(current.Tasks[id].Status) {
							task, err := CanonicalTask(current.Tasks[id], h.session.limits)
							if err != nil || task.State.Outcome.Status != "faulted" || !current.Documents[docID].Retired {
								t.Fatal("retried scheduler fault outcome", task, err)
							}
							break
						}
						select {
						case <-ctx.Done():
							t.Fatal("fault new wake lost")
						default:
							runtime.Gosched()
						}
					}
					wantRuns, wantAborts := int64(2), int64(0)
					if strings.HasPrefix(mode, "abort") {
						wantRuns, wantAborts = 0, 2
					}
					if runs.Load() != wantRuns || aborts.Load() != wantAborts || attempts.Load() != 2 {
						t.Fatal("fault retry handler counts", runs.Load(), aborts.Load(), attempts.Load())
					}
				})
			})
		}
	}
}

func TestTaskRecoveryCloseSealRejectsFreshMemoAndToolPrefix(t *testing.T) {
	for _, kind := range []string{"native-memo", "tool-output"} {
		t.Run(kind, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				entered, releaseHost, blockerEntered, releaseBlocker := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
				var captured *TaskRuntime
				var api *ToolAPI
				var h *Harness
				if kind == "native-memo" {
					def := taskDefinition(t, "task.close.memo", func(_ context.Context, _ TaskRecord, r *TaskRuntime) error {
						captured = r
						close(entered)
						<-releaseHost
						return nil
					})
					h = taskTestHarness(t, b.store, def)
					cleanupTaskGates(t, releaseHost, releaseBlocker)
					createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
					if err := h.Resume(bg); err != nil {
						t.Fatal(err)
					}
				} else {
					var requests atomic.Int64
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						if requests.Add(1) == 1 {
							taskHTTPRound(w, []string{"close-prefix"})
						} else {
							taskHTTPRound(w, nil)
						}
					}))
					defer server.Close()
					model := fakeModel(goai.ApiOpenAICompletions)
					model.BaseURL = server.URL
					model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
					registry := NewRegistry()
					if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "close-prefix", Description: "close seal", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "close.prefix", Version: 1, Execute: func(_ context.Context, _ JSON, current *ToolAPI) (ToolResult, error) {
						api = current
						captured = current.runtime
						close(entered)
						<-releaseHost
						return ToolResult{}, errors.New("unfinished on close")
					}}); err != nil {
						t.Fatal(err)
					}
					h = openHarness(t, b.store, Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }})
					cleanupTaskGates(t, releaseHost, releaseBlocker)
					conv := root(t, h, ModelRef{model.Provider, model.ID})
					if _, err := conv.Submit(bg, Input{Content: "close prefix"}); err != nil {
						t.Fatal(err)
					}
				}
				awaitTaskSignal(t, entered)
				before, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				blocked := make(chan error, 1)
				go func() {
					_, err := h.session.taskCommit(bg, func(*Tx) error { close(blockerEntered); <-releaseBlocker; return nil })
					blocked <- err
				}()
				awaitTaskSignal(t, blockerEntered)
				memoDone, outputDone := make(chan error, 1), make(chan error, 1)
				go func() { _, err := captured.MemoCandidate(bg, "after-close", JSON{"forbidden": true}); memoDone <- err }()
				if api != nil {
					// A test-only already-passed early context gate: use Background so
					// the authoritative Output callback (not ctx fastcheck) must reject.
					api.mu.Lock()
					api.ctx = bg
					api.mu.Unlock()
					go func() { outputDone <- api.Output("late-prefix") }()
				}
				closing := make(chan error, 1)
				go func() { closing <- h.Close(bg) }()
				ctx, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				for !h.closing.Load() {
					select {
					case <-ctx.Done():
						t.Fatal("atomic Close seal missing")
					default:
						runtime.Gosched()
					}
				}
				// No write was admitted before the seal: the Session line is still
				// held. New reads/operations after release must preserve this prefix.
				releaseTaskGate(releaseBlocker)
				select {
				case err := <-blocked:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("held line did not leave")
				}
				for _, done := range []<-chan error{memoDone} {
					select {
					case err := <-done:
						if !errors.Is(err, ErrClosed) && !errors.Is(err, ErrSealed) {
							t.Fatal("postseal memo write", err)
						}
					case <-ctx.Done():
						t.Fatal("postseal memo admission missing")
					}
				}
				if api != nil {
					select {
					case err := <-outputDone:
						if !errors.Is(err, ErrClosed) && !errors.Is(err, ErrSealed) {
							t.Fatal("postseal prefix write", err)
						}
					case <-ctx.Done():
						t.Fatal("postseal Output admission missing")
					}
				}
				after, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				if after.Seq != before.Seq || !equalJSONValue(after.Tasks[captured.taskID].Checkpoint, before.Tasks[captured.taskID].Checkpoint) || len(after.Entries) != len(before.Entries) {
					t.Fatal("Close allowed fresh memo/prefix publication")
				}
				if after.Tasks[captured.taskID].Execution != nil {
					memos := after.Tasks[captured.taskID].Execution.Native
					if memos != nil && memos.Memos["after-close"] != nil {
						t.Fatal("native memo admitted after seal")
					}
					builtin := after.Tasks[captured.taskID].Execution.Builtin
					if builtin != nil && builtin.Memos["after-close"] != nil {
						t.Fatal("tool memo admitted after seal")
					}
				}
				releaseTaskGate(releaseHost)
				select {
				case err := <-closing:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("Close actual join missing")
				}
			})
		})
	}
}

func TestTaskRecoveryDirectAbortCloseReopenFourStages(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		runEntered, releaseRun, abortEntered := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var runs, aborts atomic.Int64
		var blockAbort atomic.Bool
		blockAbort.Store(true)
		def := taskDefinition(t, "task.abort.close-stages", func(_ context.Context, _ TaskRecord, _ *TaskRuntime) error {
			runs.Add(1)
			close(runEntered)
			<-releaseRun
			return nil
		})
		options := def.options
		options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			aborts.Add(1)
			if blockAbort.Load() {
				close(abortEntered)
				<-ctx.Done()
				return ctx.Err()
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "stopped"}}, nil
			})
		}
		var err error
		def, err = DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
		first := taskTestHarness(t, b.store, def)
		cleanupTaskGates(t, releaseRun)
		id := createPublicTask(t, first, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := first.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, runEntered)
		aborting := make(chan error, 1)
		go func() { _, err := first.AbortTask(bg, id); aborting <- err }()
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for {
			record, _, err := first.Task(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if record.AbortRequested {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("stage1 mark missing")
			default:
				runtime.Gosched()
			}
		}
		closing := make(chan error, 1)
		go func() { closing <- first.Close(bg) }()
		for !first.closing.Load() {
			select {
			case <-ctx.Done():
				t.Fatal("stage1 Close seal missing")
			default:
				runtime.Gosched()
			}
		}
		releaseTaskGate(releaseRun)
		select {
		case err := <-closing:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("stage1 actual host join missing")
		}
		select {
		case err := <-aborting:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("marked abort caller did not join")
		}
		if runs.Load() != 1 || aborts.Load() != 0 {
			t.Fatal("stage1 invented abort effect", runs.Load(), aborts.Load())
		}
		second := taskTestHarness(t, reopenStoreAfterHarnessClose(t, b.store), def)
		opened, ok, err := second.Task(bg, id)
		if err != nil || !ok || !opened.AbortRequested || opened.State.Status != "pending" {
			t.Fatal("stage2 marked prefix", opened, err)
		}
		if err := second.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, abortEntered)
		if err := second.Close(bg); err != nil {
			t.Fatal(err)
		}
		if runs.Load() != 1 || aborts.Load() != 1 {
			t.Fatal("stage2 replayed run", runs.Load(), aborts.Load())
		}
		blockAbort.Store(false)
		third := taskTestHarness(t, reopenStoreAfterHarnessClose(t, b.store), def)
		record := waitPublicTask(t, third, id)
		if record.State.Outcome.Status != "aborted" || record.State.Outcome.Reason != "stopped" || runs.Load() != 1 || aborts.Load() != 2 {
			t.Fatal("stage3 fresh abort", record, runs.Load(), aborts.Load())
		}
		if err := third.Close(bg); err != nil {
			t.Fatal(err)
		}
		fourth := taskTestHarness(t, reopenStoreAfterHarnessClose(t, b.store), def)
		if status, err := fourth.AbortTask(bg, id); err != nil || status != "terminal" {
			t.Fatal("stage4 terminal abort", status, err)
		}
		reopened, ok, err := fourth.Task(bg, id)
		if err != nil || !ok || reopened.State.Outcome.Status != "aborted" || runs.Load() != 1 || aborts.Load() != 2 {
			t.Fatal("stage4 terminal replay", reopened, err)
		}
	})
}

// These real public operations queue on an explicitly held Session line while
// owning h.mu. Close seals atomically before that line is released. This covers
// each origin's read/admission rejection, without asserting launch-time FIFO.
func TestTaskRecoveryCloseSealRejectsPublicNonterminalOrigins(t *testing.T) {
	for _, operation := range []string{"create-conversation", "configure", "passive-commit", "withdraw", "submit-write", "submit-followup"} {
		t.Run(operation, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				entered, releaseHost, blockerEntered, releaseBlocker := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
				def := taskDefinition(t, "task.public.close.origin", func(context.Context, TaskRecord, *TaskRuntime) error { close(entered); <-releaseHost; return nil })
				h := taskTestHarness(t, b.store, def)
				cleanupTaskGates(t, releaseHost, releaseBlocker)
				model := fakeModel(goai.ApiOpenAICompletions)
				rootHandle := root(t, h, ModelRef{model.Provider, model.ID})
				foreign, err := h.CreateConversation(bg, AgentChange{Model: ModelRef{model.Provider, model.ID}})
				if err != nil {
					t.Fatal(err)
				}
				var subID ID
				_, err = h.CommitTasks(bg, foreign.ID(), func(tx *Tx) error {
					var err error
					subID, err = tx.MintID()
					if err != nil {
						return err
					}
					return tx.PutSubmission(Submission{ID: subID, Conversation: foreign.ID(), Type: "follow-up", Status: "pending", Value: JSON{}})
				})
				if err != nil {
					t.Fatal(err)
				}
				createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, entered)
				before, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				blocked := make(chan error, 1)
				go func() {
					_, err := h.session.taskCommit(bg, func(*Tx) error { close(blockerEntered); <-releaseBlocker; return nil })
					blocked <- err
				}()
				awaitTaskSignal(t, blockerEntered)
				result := make(chan error, 1)
				go func() {
					var err error
					switch operation {
					case "create-conversation":
						_, err = h.CreateConversation(bg, AgentChange{Model: ModelRef{model.Provider, model.ID}})
					case "configure":
						err = foreign.Configure(bg, AgentChange{Model: ModelRef{model.Provider, model.ID}, SystemPrompt: "late"})
					case "passive-commit":
						_, err = foreign.Commit(bg, func(tx *Tx) error {
							id, err := tx.MintID()
							if err != nil {
								return err
							}
							return tx.AppendEntry(Entry{ID: id, Conversation: foreign.ID(), Kind: "app.late", Value: JSON{}})
						})
					case "withdraw":
						err = (&SubmissionHandle{h: h, id: subID}).Withdraw(bg)
					case "submit-write":
						_, err = foreign.Submit(bg, Input{Type: "write", Content: "late"})
					case "submit-followup":
						_, err = foreign.Submit(bg, Input{Content: "late"})
					}
					result <- err
				}()
				ctx, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				// h.mu ownership is the actual public admission precondition, not
				// an 'attempted' channel emitted before calling the operation.
				for {
					if !h.mu.TryLock() {
						break
					}
					h.mu.Unlock()
					select {
					case <-ctx.Done():
						t.Fatal("public operation did not acquire origin lock")
					default:
						runtime.Gosched()
					}
				}
				closing := make(chan error, 1)
				go func() { closing <- h.Close(bg) }()
				for !h.closing.Load() {
					select {
					case <-ctx.Done():
						t.Fatal("public Close seal missing")
					default:
						runtime.Gosched()
					}
				}
				releaseTaskGate(releaseBlocker)
				select {
				case err := <-blocked:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("origin held line missing")
				}
				select {
				case err := <-result:
					if !errors.Is(err, ErrClosed) {
						t.Fatal("public origin admitted after seal", operation, err)
					}
				case <-ctx.Done():
					t.Fatal("public origin rejection missing", operation)
				}
				after, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				// CreateConversation may consume its pretransaction MintID after
				// the Harness seal, before its rejected commit. Native reservations
				// are monotonic allocator writes, not table/entry publications.
				if operation != "create-conversation" && after.HighWater != before.HighWater {
					t.Fatal("unexpected preparation ID reservation", operation)
				}
				if operation == "create-conversation" && (after.HighWater < before.HighWater || after.HighWater > before.HighWater+1) {
					t.Fatal("unexpected conversation reservation count")
				}
				if after.Seq != before.Seq || len(after.Entries) != len(before.Entries) || len(after.Conversations) != len(before.Conversations) || after.Submissions[subID].Status != "pending" {
					t.Fatal("closed public origin published records", operation)
				}
				_ = rootHandle
				releaseTaskGate(releaseHost)
				select {
				case err := <-closing:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("Close join missing")
				}
			})
		})
	}
}

func TestTaskRecoveryQueuedWithdrawalOwnedChildDefersSettlement(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, releaseChild := make(chan struct{}), make(chan struct{})
		var childCalls, requests atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); taskHTTPRound(w, nil) }))
		defer server.Close()
		model := fakeModel(goai.ApiOpenAICompletions)
		model.BaseURL = server.URL
		model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
		child := taskDefinition(t, "task.withdraw.owned.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			childCalls.Add(1)
			close(entered)
			<-releaseChild
			return ctx.Err()
		})
		owner := taskDefinition(t, "task.withdraw.owner", func(context.Context, TaskRecord, *TaskRuntime) error { return errors.New("owner should abort") })
		registry := NewRegistry()
		for _, def := range []*TaskDefinition{child, owner} {
			if _, err := registry.RegisterTask(def); err != nil {
				t.Fatal(err)
			}
		}
		h := openHarness(t, b.store, Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }})
		cleanupTaskGates(t, releaseChild)
		rootHandle := root(t, h, ModelRef{model.Provider, model.ID})
		ownerID := createPublicTask(t, h, owner, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		var foreign ID
		_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			foreign, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.CreateConversation(Conversation{ID: foreign, Owner: ownerID})
		})
		if err != nil {
			t.Fatal(err)
		}
		foreignHandle, _ := h.Conversation(bg, foreign)
		if err := foreignHandle.Configure(bg, AgentChange{Model: ModelRef{model.Provider, model.ID}}); err != nil {
			t.Fatal(err)
		}
		// Real queued admission under a private paused selection seam. A later
		// supplemental task descendant is admitted before any provider effect.
		h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(false) })
		var subID, queuedGeneration ID
		_, err = h.CommitTasks(bg, foreign, func(tx *Tx) error {
			var err error
			subID, err = tx.MintID()
			if err != nil {
				return err
			}
			queuedGeneration, err = tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.PutSubmission(Submission{ID: subID, Conversation: foreign, Type: "follow-up", Status: "pending", Value: JSON{"content": "queued child scope"}}); err != nil {
				return err
			}
			cp, err := dtoObject(generationCheckpoint{Phase: "queued", Submission: subID, Input: "queued child scope"}, tx.limits)
			if err != nil {
				return err
			}
			if err := tx.PutTask(Task{ID: queuedGeneration, Conversation: foreign, Kind: "pi.generation", Status: "pending", Checkpoint: cp}); err != nil {
				return err
			}
			inbox, err := builtin(tx, foreign, "pi.inbox")
			if err != nil {
				return err
			}
			return inbox.Set(JSON{"items": []any{JSON{"id": subID, "mode": "followUp", "input": JSON{"content": "queued child scope"}}}})
		})
		if err != nil {
			t.Fatal(err)
		}
		sub := &SubmissionHandle{h: h, id: subID}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		var generationID ID
		for _, task := range state.Tasks {
			if task.Kind == "pi.generation" && task.Conversation == foreign {
				generationID = task.ID
			}
		}
		var childID ID
		_, err = h.CommitTasks(bg, foreign, func(tx *Tx) error {
			var err error
			childID, err = tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: generationID}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		h.session.taskBookkeeping(func() {
			h.scheduler.retryAfter[ownerID] = ^uint64(0)
			h.scheduler.retryAfter[generationID] = ^uint64(0)
		})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(false) })
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if err := sub.Withdraw(bg); err == nil {
			t.Fatal("owned queued withdrawal terminalised parent")
		}
		rejected, err := h.Snapshot(bg)
		if err != nil || rejected.Seq != before.Seq || rejected.Submissions[sub.ID()].Status != "pending" {
			t.Fatal("owned explicit withdrawal partially adopted", err)
		}
		// Passive write in the descendant conversation and own queue records
		// remain outside reached follow-up withdrawal.
		write, err := foreignHandle.Submit(bg, Input{Type: "write", Content: "preserve passive"})
		if err != nil {
			t.Fatal(err)
		}
		var ownID ID
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			ownID, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.PutSubmission(Submission{ID: ownID, Conversation: 1, Type: "follow-up", Status: "pending", Value: JSON{"content": "preserve own queue"}})
		})
		if err != nil {
			t.Fatal(err)
		}
		own := &SubmissionHandle{h: h, id: ownID}
		_ = rootHandle
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
			task := markTask(tx.state.Tasks[ownerID])
			return tx.stage(Write{Op: "put-task", Task: &task})
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.scheduler.reconcile(); err != nil {
			t.Fatal(err)
		}
		marked, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if !taskAborted(marked.Tasks[generationID]) || terminalStatus(marked.Tasks[generationID].Status) || marked.Submissions[sub.ID()].Status != "pending" || requests.Load() != 0 {
			t.Fatal("owned queued cascade settled before actual child join")
		}
		if err := h.scheduler.reconcile(); err != nil {
			t.Fatal(err)
		}
		childRecord, _, err := h.Task(bg, childID)
		if err != nil || !childRecord.AbortRequested {
			t.Fatal("queued child cascade mark missing", childRecord, err)
		}
		joined, err := h.Snapshot(bg)
		if err != nil || joined.Submissions[sub.ID()].Status != "pending" || joined.Submissions[write.ID()].Status != "pending" || joined.Submissions[own.ID()].Status != "pending" {
			t.Fatal("queued child unsettled/queue preservation", err)
		}
		releaseTaskGate(releaseChild)
		h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(true) })
		h.scheduler.kick()
		if record := waitPublicTask(t, h, childID); record.State.Outcome.Status != "aborted" {
			t.Fatal(record)
		}
		if settled := waitSubmission(t, sub); settled.Submission.Status != "aborted" {
			t.Fatal(settled)
		}
		if settled := waitSubmission(t, write); settled.Submission.Status != "done" {
			t.Fatal("passive write was withdrawn", settled)
		}
		if childCalls.Load() != 1 || requests.Load() != 0 {
			t.Fatal("queued scope provider/effect dispatched", childCalls.Load(), requests.Load())
		}
	})
}

func TestTaskRecoveryFreshSubmitAfterReadCloseAdmissionRejects(t *testing.T) {
	for _, mode := range []string{"write", "follow-up"} {
		for _, bindingMode := range []string{"host", "bound"} {
			t.Run(mode+"/"+bindingMode, func(t *testing.T) {
				backends(t, func(t *testing.T, b backend) {
					entered, releaseHost := make(chan struct{}), make(chan struct{})
					var captured *TaskRuntime
					def := taskDefinition(t, "task.submit.postread.close", func(_ context.Context, _ TaskRecord, r *TaskRuntime) error {
						captured = r
						close(entered)
						<-releaseHost
						return nil
					})
					h := taskTestHarness(t, b.store, def)
					cleanupTaskGates(t, releaseHost)
					model := fakeModel(goai.ApiOpenAICompletions)
					conv, err := h.CreateConversation(bg, AgentChange{Model: ModelRef{model.Provider, model.ID}})
					if err != nil {
						t.Fatal(err)
					}
					createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
					if err := h.Resume(bg); err != nil {
						t.Fatal(err)
					}
					awaitTaskSignal(t, entered)
					before, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					var binding *TaskRuntime
					if bindingMode == "bound" {
						binding = captured
					}
					result := make(chan error, 1)
					queued := make(chan struct{})
					err = func() error {
						h.mu.Lock()
						defer h.mu.Unlock()
						// Witness the exact successful initial read prerequisites first.
						if err := h.session.readTasks(bg, func(state Snapshot) error {
							if binding != nil {
								if err := binding.check(); err != nil {
									return err
								}
							}
							if _, ok := agentDocument(state, conv.ID()); !ok {
								return errors.New("agent missing before fresh submission")
							}
							if len(state.Submissions) != 0 {
								return errors.New("expected fresh request")
							}
							return nil
						}); err != nil {
							return err
						}
						// The trusted fresh helper is queued while this deciding no-op
						// owns the Session line. Close seals before it can enter Commit.
						_, err := h.session.taskCommit(bg, func(*Tx) error {
							go func() {
								close(queued)
								_, err := conv.submitNewAdmission(bg, Input{Type: mode, Content: "postread queued"}, binding)
								result <- err
							}()
							<-queued
							go func() { _ = h.Close(bg) }()
							ctx, cancel := context.WithTimeout(bg, 3*time.Second)
							defer cancel()
							for !h.closing.Load() {
								select {
								case <-ctx.Done():
									return ctx.Err()
								default:
									runtime.Gosched()
								}
							}
							return nil
						})
						return err
					}()
					if err != nil {
						t.Fatal(err)
					}
					select {
					case err := <-result:
						if !errors.Is(err, ErrClosed) {
							t.Fatal("fresh submission adopted after close", err)
						}
					case <-time.After(3 * time.Second):
						t.Fatal("fresh queued commit rejection missing")
					}
					after, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					if after.Seq != before.Seq || after.HighWater != before.HighWater || len(after.Submissions) != 0 || len(after.Entries) != 0 {
						t.Fatal("fresh submission leaked writes/IDs after close")
					}
					releaseTaskGate(releaseHost)
					ctx, cancel := context.WithTimeout(bg, 3*time.Second)
					defer cancel()
					if err := h.Close(ctx); err != nil {
						t.Fatal(err)
					}
				})
			})
		}
	}
}

func TestTaskRecoveryBuiltinPredispatchAncestorCancellation(t *testing.T) {
	for _, kind := range []string{"generation", "tool"} {
		t.Run(kind, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				var requests, executes atomic.Int64
				entered, release := make(chan struct{}), make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); taskHTTPRound(w, nil) }))
				defer server.Close()
				model := fakeModel(goai.ApiOpenAICompletions)
				model.BaseURL = server.URL
				model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
				registry := NewRegistry()
				if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "predispatch", Description: "cancel gate", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "predispatch", Version: 1, ReplaySafe: true, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
					executes.Add(1)
					return ToolResult{Content: "forbidden effect"}, nil
				}}); err != nil {
					t.Fatal(err)
				}
				options := Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }}
				if kind == "generation" {
					options.RequestOptions = func(context.Context, ModelRef) (*goai.StreamOptions, error) {
						close(entered)
						<-release
						return nil, nil
					}
				}
				h := openHarness(t, b.store, options)
				cleanupTaskGates(t, release)
				conv := root(t, h, ModelRef{model.Provider, model.ID})
				var ownerID, generationID, toolID, subID ID
				_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
					var err error
					ownerID, err = tx.MintID()
					if err != nil {
						return err
					}
					owner := foundationTask(ownerID, nil)
					if err := tx.stage(Write{Op: "put-task", Task: &owner}); err != nil {
						return err
					}
					subID, err = tx.MintID()
					if err != nil {
						return err
					}
					generationID, err = tx.MintID()
					if err != nil {
						return err
					}
					if err := tx.PutSubmission(Submission{ID: subID, Conversation: conv.ID(), Type: "follow-up", Status: "pending", Value: JSON{"content": "predispatch"}}); err != nil {
						return err
					}
					cp := generationCheckpoint{Phase: "intent", Submission: subID, Input: "predispatch", Model: model, Agent: agentState{Model: ModelRef{model.Provider, model.ID}}, Messages: []MessageReceipt{userReceipt("predispatch")}}
					if kind == "tool" {
						toolID, err = tx.MintID()
						if err != nil {
							return err
						}
						reg, _ := registry.current("predispatch")
						toolCP, err := dtoObject(toolCheckpoint{Offer: reg.offer, CallID: "predispatch-call", Arguments: JSON{}}, tx.limits)
						if err != nil {
							return err
						}
						if err := tx.PutTask(Task{ID: toolID, Owner: generationID, Conversation: conv.ID(), Kind: "pi.tool", Status: "pending", Checkpoint: toolCP}); err != nil {
							return err
						}
						cp.Phase = "tools"
						cp.Children = []ID{toolID}
						// Persist the actual assistant call, so ordered abort receipts
						// compose valid context without a fake missing tool pair.
						entryID, err := tx.MintID()
						if err != nil {
							return err
						}
						receipt := MessageReceipt{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, EmptyArguments: []int{0}, Content: []goai.ContentBlock{{Type: "toolCall", ID: "predispatch-call", Name: "predispatch", Arguments: JSON{}}}}
						entryValue, err := dtoObject(receipt, tx.limits)
						if err != nil {
							return err
						}
						if err := tx.AppendEntry(Entry{ID: entryID, Conversation: conv.ID(), Kind: "message", Value: entryValue, ByTask: generationID}); err != nil {
							return err
						}
					}
					value, err := dtoObject(cp, tx.limits)
					if err != nil {
						return err
					}
					status := "pending"
					if kind == "tool" {
						status = "completing"
					}
					return tx.PutTask(Task{ID: generationID, Owner: ownerID, Conversation: conv.ID(), Kind: "pi.generation", Status: status, Checkpoint: value})
				})
				if err != nil {
					t.Fatal(err)
				}
				h.session.taskBookkeeping(func() { h.scheduler.retryAfter[ownerID] = ^uint64(0) })
				var reserved *TaskRuntime
				if kind == "generation" {
					if err := h.Resume(bg); err != nil {
						t.Fatal(err)
					}
					awaitTaskSignal(t, entered)
					h.session.taskBookkeeping(func() { reserved = h.scheduler.invocations[generationID]; h.scheduler.enabled.Store(false) })
				} else {
					// Native reserved candidate without host launch; hold the real
					// authoritative initial read until ancestor mark is adopted.
					reservations, err := h.scheduler.reservePass(false)
					if err != nil || len(reservations) != 1 || reservations[0].taskID != toolID {
						discardUnstartedTaskReservations(t, h, reservations)
						t.Fatal("tool reservation fixture", err)
					}
					reserved = reservations[0]
					t.Cleanup(func() {
						select {
						case <-reserved.done:
						default:
							discardUnstartedTaskReservations(t, h, []*TaskRuntime{reserved})
						}
					})
					h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(false) })
				}
				_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
					task := markTask(tx.state.Tasks[ownerID])
					return tx.stage(Write{Op: "put-task", Task: &task})
				})
				if err != nil {
					t.Fatal(err)
				}
				if kind == "tool" {
					go h.scheduler.run(reserved)
				} else {
					releaseTaskGate(release)
				}
				awaitTaskSignal(t, reserved.done)
				if requests.Load() != 0 || executes.Load() != 0 {
					t.Fatal("reserved builtin escaped confirmed ancestor cancellation", requests.Load(), executes.Load())
				}
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				if terminalStatus(state.Tasks[generationID].Status) || state.Submissions[subID].Status != "pending" {
					t.Fatal("predispatch cancelled result fabricated")
				}
				h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(true) })
				h.scheduler.kick()
				if settled := waitSubmission(t, &SubmissionHandle{h: h, id: subID}); settled.Submission.Status != "aborted" {
					t.Fatal(settled)
				}
				if requests.Load() != 0 || executes.Load() != 0 {
					t.Fatal("abort bridge dispatched provider/tool effect")
				}
			})
		})
	}
}

func TestTaskRecoveryQueuedHandoverLateCommitAndAbortOrdering(t *testing.T) {
	for _, operation := range []string{"late-commit", "abort-mark"} {
		t.Run(operation, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				oldEntered, releaseOld, progressEntered, releaseProgress, handoverEntered, releaseHandover := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
				var oldRuntime *TaskRuntime
				var oldNext, newNext, newAbort atomic.Int64
				old, err := DefineTask(TaskDefinitionOptions{Kind: "task.handover.queue", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "a"}, nil }, Phases: map[string]TaskPhase{
					"a": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
						oldRuntime = r
						close(oldEntered)
						<-releaseOld
						return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
							return &TaskState{Status: "running", Checkpoint: JSON{"phase": "b"}}, nil
						})
					},
					"b": func(context.Context, TaskRecord, *TaskRuntime) error {
						oldNext.Add(1)
						return errors.New("old next phase ran")
					},
				}, Abort: taskAbort})
				if err != nil {
					t.Fatal(err)
				}
				h := taskTestHarness(t, b.store, old)
				cleanupTaskGates(t, releaseOld, releaseProgress, releaseHandover)
				id := createPublicTask(t, h, old, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				var core *storeCore
				if native, ok := b.store.(*MemoryStorage); ok {
					core = native.storeCore
				} else {
					core = b.store.(*JournalStorage).storeCore
				}
				var original func(byte, uint64, uint64, []byte) error
				h.session.taskBookkeeping(func() { <-core.line; original = core.appendFrame; core.leave() })
				var reservationSeen bool
				var progressOnce, handoverOnce sync.Once
				var statesMu sync.Mutex
				states := []string{"pending"}
				installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
					if kind == 2 {
						var record commitRecord
						if err := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &record); err != nil {
							return err
						}
						for _, write := range record.Writes {
							if write.Task == nil || write.Task.ID != id {
								continue
							}
							state := write.Task.Status
							if taskAborted(*write.Task) {
								state += "+mark"
							}
							if write.Task.Status == "running" && !reservationSeen {
								reservationSeen = true
							} else if write.Task.Status == "running" && !taskAborted(*write.Task) {
								progressOnce.Do(func() { close(progressEntered); <-releaseProgress })
							} else if write.Task.Status == "pending" && !taskAborted(*write.Task) {
								// Only the unmarked handover owns this one-shot gate.
								// Later pending+mark is recorded/appended independently.
								handoverOnce.Do(func() {
									h.scheduler.enabled.Store(false)
									close(handoverEntered)
									<-releaseHandover
								})
							}
							if original != nil {
								if err := original(kind, ordinal, high, payload); err != nil {
									return err
								}
							}
							statesMu.Lock()
							states = append(states, state)
							statesMu.Unlock()
							return nil
						}
					}
					if original != nil {
						return original(kind, ordinal, high, payload)
					}
					return nil
				})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, oldEntered)
				options := old.options
				options.Phases = map[string]TaskPhase{"a": func(context.Context, TaskRecord, *TaskRuntime) error { return errors.New("replacement restarted a") }, "b": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					newNext.Add(1)
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("new"), nil })
				}}
				options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					newAbort.Add(1)
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "new"}}, nil
					})
				}
				replacement, err := DefineTask(options)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := h.options.Registry.RegisterTask(replacement); err != nil {
					t.Fatal(err)
				}
				releaseTaskGate(releaseOld)
				awaitTaskSignal(t, progressEntered)
				// This actual progress storage admission is before the handover;
				// no registry/launch-time ordering inference selects the step.
				releaseTaskGate(releaseProgress)
				awaitTaskSignal(t, handoverEntered)
				if oldRuntime.ended.Load() {
					t.Fatal("invocation sealed before handover storage adoption")
				}
				result := make(chan error, 1)
				if operation == "late-commit" {
					// Already-held handover admission wins over this runtime commit.
					go func() {
						result <- oldRuntime.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("late"), nil })
					}()
				} else {
					epoch := h.scheduler.epoch.Load()
					go func() { _, err := h.AbortTask(bg, id); result <- err }()
					ctx, cancel := context.WithTimeout(bg, 3*time.Second)
					for h.scheduler.epoch.Load() == epoch {
						select {
						case <-ctx.Done():
							cancel()
							t.Fatal("actual AbortTask Resume barrier missing")
						default:
							runtime.Gosched()
						}
					}
					cancel()
					// Pause selection AFTER witnessed host Resume, while handover
					// still owns the line. Mark admission cannot race a new phase.
					h.scheduler.enabled.Store(false)
				}
				releaseTaskGate(releaseHandover)
				select {
				case err := <-result:
					if operation == "late-commit" && !errors.Is(err, ErrSealed) {
						t.Fatal("queued old invocation admitted after handover", err)
					}
					if operation == "abort-mark" && err != nil {
						t.Fatal("queued mark", err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("queued handover operation missing")
				}
				awaitTaskSignal(t, oldRuntime.done)
				if operation == "late-commit" {
					h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(true) })
					h.scheduler.kick()
				}
				record := waitPublicTask(t, h, id)
				if oldNext.Load() != 0 {
					t.Fatal("old phase overlapped replacement")
				}
				if operation == "abort-mark" {
					if record.State.Outcome.Status != "aborted" || record.State.Outcome.Reason != "new" || newAbort.Load() != 1 || newNext.Load() != 0 {
						t.Fatal("handover mark lost/replacement ordinaryphase", record, newAbort.Load(), newNext.Load())
					}
					statesMu.Lock()
					actual := strings.Join(states, ",")
					statesMu.Unlock()
					if actual != "pending,running,running,pending,pending+mark,running+mark,aborted+mark" {
						t.Fatal("handover+mark adopted sequence", actual)
					}
				} else if record.State.Outcome.Status != "completed" || record.State.Outcome.Result.Value != "new" || newNext.Load() != 1 || newAbort.Load() != 0 {
					t.Fatal("latecommit overwrote replacement outcome", record)
				}
			})
		})
	}
}

func TestTaskRecoveryGenerationDecidingFinalFailureMatrix(t *testing.T) {
	for _, backendName := range []string{"memory", "journal"} {
		for _, stage := range []string{"deciding", "final"} {
			for _, failure := range []string{"rejected", "uncertain-before", "uncertain-after"} {
				t.Run(backendName+"/"+stage+"/"+failure, func(t *testing.T) {
					image := &MemoryImage{}
					directory := t.TempDir() + "/private"
					open := func() Storage {
						var store Storage
						var err error
						if backendName == "memory" {
							store, err = OpenMemory(MemoryOptions{Image: image})
						} else {
							store, err = OpenJournal(directory, JournalOptions{})
						}
						if err != nil {
							t.Fatal(err)
						}
						return store
					}
					store := open()
					var core *storeCore
					if native, ok := store.(*MemoryStorage); ok {
						core = native.storeCore
					} else {
						core = store.(*JournalStorage).storeCore
					}
					requestEntered, releaseResponse, childEntered, releaseChild := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
					var requests, childCalls atomic.Int64
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						if requests.Add(1) == 1 {
							close(requestEntered)
							<-releaseResponse
						}
						taskHTTPRound(w, nil)
					}))
					defer server.Close()
					model := fakeModel(goai.ApiOpenAICompletions)
					model.BaseURL = server.URL
					model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
					child := taskDefinition(t, "task.generation.failure.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
						if childCalls.Add(1) == 1 {
							close(childEntered)
							select {
							case <-releaseChild:
							case <-ctx.Done():
								return ctx.Err()
							}
						}
						return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("child"), nil })
					})
					registry := NewRegistry()
					if _, err := registry.RegisterTask(child); err != nil {
						t.Fatal(err)
					}
					reports := make(chan error, 4)
					options := Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }, OnReport: func(err error) {
						select {
						case reports <- err:
						default:
						}
					}}
					h := openHarness(t, store, options)
					cleanupTaskGates(t, releaseResponse, releaseChild)
					conv := root(t, h, ModelRef{model.Provider, model.ID})
					sub, err := conv.Submit(bg, Input{Content: "generation outcome failure", RequestID: "generation-matrix"})
					if err != nil {
						t.Fatal(err)
					}
					awaitTaskSignal(t, requestEntered)
					state, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					var generation, taskDocID ID
					for _, task := range state.Tasks {
						if task.Kind == "pi.generation" {
							generation = task.ID
						}
					}
					_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
						if _, err := tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: generation}}); err != nil {
							return err
						}
						id, err := tx.MintID()
						if err != nil {
							return err
						}
						taskDocID = id
						_, err = tx.CreateDocument(Document{ID: id, Scope: "task", Owner: generation, Kind: "app.generation-failure", Version: 1, Value: JSON{"live": true}})
						return err
					})
					if err != nil {
						t.Fatal(err)
					}
					awaitTaskSignal(t, childEntered)
					var original func(byte, uint64, uint64, []byte) error
					h.session.taskBookkeeping(func() { <-core.line; original = core.appendFrame; core.leave() })
					var injected atomic.Int64
					installFailure := func() {
						installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
							if kind == 2 {
								var record commitRecord
								if err := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &record); err != nil {
									return err
								}
								for _, write := range record.Writes {
									if write.Task == nil || write.Task.ID != generation || !taskHasDecidedOutcome(*write.Task) {
										continue
									}
									hold := write.Task.Execution.Builtin.Hold
									if (stage == "deciding" && hold.Stage == "held") || (stage == "final" && hold.Stage == "final") {
										injected.Add(1)
										h.scheduler.enabled.Store(false)
										if failure == "uncertain-after" && original != nil {
											if err := original(kind, ordinal, high, payload); err != nil {
												return err
											}
										}
										if failure == "rejected" {
											return reject("generation " + stage + " rejected")
										}
										return errors.New("generation " + stage + " uncertain")
									}
								}
							}
							if original != nil {
								return original(kind, ordinal, high, payload)
							}
							return nil
						})
					}
					var generationDone <-chan struct{}
					h.session.taskBookkeeping(func() { generationDone = h.scheduler.invocations[generation].done })
					if stage == "deciding" {
						installFailure()
					}
					releaseTaskGate(releaseResponse)
					if stage == "final" {
						observeTaskState(t, h, generation, "completing")
						awaitTaskSignal(t, generationDone)
						held, err := h.Snapshot(bg)
						if err != nil {
							t.Fatal(err)
						}
						assertTaskModelSpend(t, held, 1, model.Provider, model.ID, 5)
						if held.Submissions[sub.ID()].Status != "pending" || held.Documents[taskDocID].Retired {
							t.Fatal("generation cleanup leaked at Hold")
						}
						installFailure()
						releaseTaskGate(releaseChild)
					}
					select {
					case err := <-reports:
						if failure == "rejected" {
							if err.Error() != reject("generation "+stage+" rejected").Error() {
								t.Fatal("generation rejection cause", err)
							}
						} else if !errors.Is(err, ErrPoisoned) {
							t.Fatal("generation poison cause", err)
						}
					case <-time.After(3 * time.Second):
						t.Fatal("generation failure not reported")
					}
					awaitTaskSignal(t, generationDone)
					if injected.Load() != 1 || requests.Load() != 1 {
						t.Fatal("generation failure self retry/effects", injected.Load(), requests.Load())
					}
					if failure == "rejected" {
						rejected, err := h.Snapshot(bg)
						if err != nil {
							t.Fatal(err)
						}
						if rejected.Submissions[sub.ID()].Status != "pending" || rejected.Documents[taskDocID].Retired || taskHasDecidedOutcome(rejected.Tasks[generation]) != (stage == "final") {
							t.Fatal("rejected generation outcome subset adopted")
						}
						assertTaskModelSpendOrAbsent(t, rejected, 1, model.Provider, model.ID, map[bool]int{true: 5, false: 0}[stage == "final"])
					}
					if err := h.Close(bg); err != nil {
						t.Fatal(err)
					}
					second := openHarness(t, open(), options)
					opened, err := second.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					confirmedAfter := backendName == "journal" && failure == "uncertain-after"
					confirmedReceipt := stage == "final" || confirmedAfter
					confirmedFinal := stage == "final" && confirmedAfter
					if taskHasDecidedOutcome(opened.Tasks[generation]) != confirmedReceipt || terminalStatus(opened.Submissions[sub.ID()].Status) != confirmedFinal || opened.Documents[taskDocID].Retired != confirmedFinal {
						t.Fatal("generation confirmed reopen prefix")
					}
					assertTaskModelSpendOrAbsent(t, opened, 1, model.Provider, model.ID, map[bool]int{true: 5, false: 0}[confirmedReceipt])
					if requests.Load() != 1 {
						t.Fatal("Open replayed provider")
					}
					if settled := waitSubmission(t, &SubmissionHandle{h: second, id: sub.ID()}); settled.Submission.Status != "done" || settled.Message == nil {
						t.Fatal("generation recovery settlement", settled)
					}
					final, err := second.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					assertTaskModelSpend(t, final, 1, model.Provider, model.ID, 5)
					wantRequests := int64(1)
					if !confirmedReceipt {
						wantRequests = 2
					}
					if requests.Load() != wantRequests || !final.Documents[taskDocID].Retired {
						t.Fatal("generation recovery duplicated confirmed effect or omitted cleanup", requests.Load(), wantRequests)
					}
					var receipts int
					for _, entry := range final.Entries {
						if entry.ByTask == generation && entry.Kind == "message" {
							receipts++
						}
					}
					// Unconfirmed deciding recovery may turn a confirmed Partial
					// checkpoint into ONE interrupted prefix before the next final.
					// Adopted Hold/Final replays neither receipt nor known spend.
					wantReceipts := 1
					if !confirmedReceipt {
						var failedCP generationCheckpoint
						if err := fromObject(opened.Tasks[generation].Checkpoint, &failedCP, second.session.limits); err != nil {
							t.Fatal(err)
						}
						if failedCP.Partial != nil {
							wantReceipts++
						}
					}
					if receipts != wantReceipts {
						t.Fatal("generation confirmed receipt count", receipts, wantReceipts)
					}
				})
			}
		}
	}
}

func assertTaskModelSpendOrAbsent(t *testing.T, state Snapshot, conversation ID, provider goai.Provider, model string, total int) {
	t.Helper()
	if total != 0 {
		assertTaskModelSpend(t, state, conversation, provider, model, total)
		return
	}
	for _, doc := range state.Documents {
		if doc.Kind == "pi.usage" && doc.Owner == conversation && !doc.Retired {
			models, ok := doc.Value["models"].(map[string]any)
			if !ok {
				t.Fatal("models usage shape")
			}
			if _, present := models[string(provider)+"/"+model]; present {
				t.Fatal("rejected/unconfirmed provider spend fabricated")
			}
			return
		}
	}
	t.Fatal("usage document missing")
}

func TestTaskRecoveryBlockedDefinitionPauseReadAndFittingRegistration(t *testing.T) {
	for _, variant := range []string{"missing", "too-old", "no-migration"} {
		t.Run(variant, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				var runs, migrations atomic.Int64
				base := taskDefinition(t, "task.definition.blocked", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					runs.Add(1)
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("fitting"), nil })
				})
				storedOptions := base.options
				if variant == "too-old" {
					storedOptions.Version = 2
				}
				stored, err := DefineTask(storedOptions)
				if err != nil {
					t.Fatal(err)
				}
				reports := make(chan error, 4)
				h := taskTestHarnessOptions(t, b.store, Options{OnReport: func(err error) {
					select {
					case reports <- err:
					default:
					}
				}})
				id := createPublicTask(t, h, stored, JSON{"input": "owned"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				before, err := h.InspectTasks(bg)
				if err != nil || before.Scheduling != "paused" || runs.Load() != 0 || migrations.Load() != 0 {
					t.Fatal("paused definition reads enabled effects", before, err)
				}
				var dispose func()
				if variant != "missing" {
					options := base.options
					if variant == "no-migration" {
						options.Version = 2
					}
					unfitting, err := DefineTask(options)
					if err != nil {
						t.Fatal(err)
					}
					dispose, err = h.options.Registry.RegisterTask(unfitting)
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				// Explicit production pass supplies a deterministic blocked-decision
				// witness; no timing-based absence is the acceptance gate.
				reservations, err := h.scheduler.reservePass(false)
				discardUnstartedTaskReservations(t, h, reservations)
				if err != nil || len(reservations) != 0 {
					t.Fatal("unfitting task reserved", err)
				}
				inspection, err := h.InspectTasks(bg)
				reason := "missing_task"
				if variant == "too-old" {
					reason = "task_too_old"
				}
				if variant == "no-migration" {
					reason = "migration_failed"
				}
				if err != nil || len(inspection.Tasks) != 1 || inspection.Tasks[0].Kind != "blocked" || inspection.Tasks[0].Reason != reason || runs.Load() != 0 || migrations.Load() != 0 {
					t.Fatal("blocked task projection", inspection, err)
				}
				if variant == "missing" {
					ctx, cancel := context.WithCancel(bg)
					idle := make(chan error, 1)
					go func() { idle <- h.WaitForIdle(ctx) }()
					ack, stop := context.WithTimeout(bg, 3*time.Second)
					defer stop()
					for {
						registered := false
						h.session.taskBookkeeping(func() {
							for wait := range h.scheduler.waiters {
								if wait.idleScope != nil && wait.binding == nil {
									registered = true
								}
							}
						})
						if registered {
							break
						}
						select {
						case <-ack.Done():
							cancel()
							t.Fatal("blocked work not counted as live idle")
						default:
							runtime.Gosched()
						}
					}
					cancel()
					select {
					case err := <-idle:
						if !errors.Is(err, context.Canceled) {
							t.Fatal(err)
						}
					case <-ack.Done():
						t.Fatal("blocked idle cancel missing")
					}
				}
				if dispose != nil {
					dispose()
				}
				fittingOptions := storedOptions
				if variant == "no-migration" {
					fittingOptions.Version = 2
					fittingOptions.Migrate = func(input any, checkpoint JSON, _ uint64) (any, JSON, error) {
						migrations.Add(1)
						return input, checkpoint, nil
					}
				}
				fitting, err := DefineTask(fittingOptions)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := h.options.Registry.RegisterTask(fitting); err != nil {
					t.Fatal(err)
				}
				// Reads never Resume; registry publication alone must unblock.
				ack, stop := context.WithTimeout(bg, 3*time.Second)
				defer stop()
				var record TaskRecord
				for {
					view, ok, err := h.Task(ack, id)
					if err != nil || !ok {
						t.Fatal("fitting read", err)
					}
					if view.State.Status == "terminal" {
						record = view
						break
					}
					select {
					case <-ack.Done():
						t.Fatal("registry wake did not unblock fitting definition")
					default:
						runtime.Gosched()
					}
				}
				if record.State.Outcome.Result.Value != "fitting" || record.Version != fitting.Version() || runs.Load() != 1 || migrations.Load() != map[bool]int64{true: 1, false: 0}[variant == "no-migration"] {
					t.Fatal("fitting definition did not recover blocked record", record, runs.Load(), migrations.Load())
				}
			})
		})
	}
}

func TestTaskRecoveryOpenFailureClosesStoreAndRetainsNoSubscription(t *testing.T) {
	for _, stage := range []string{"running-reconciliation", "registry-subscription"} {
		t.Run(stage, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				registry := NewRegistry()
				var effects atomic.Int64
				def := taskDefinition(t, "task.open.failure", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					effects.Add(1)
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("recovered"), nil })
				})
				if _, err := registry.RegisterTask(def); err != nil {
					t.Fatal(err)
				}
				id := mint(t, b.store)
				task := foundationTask(id, JSON{"input": "retained"})
				task.Kind = def.Kind()
				task.Status = "running"
				task.Execution.Native.State.Status = "running"
				apply(t, b.store, Write{Op: "put-task", Task: &task})
				if stage == "running-reconciliation" {
					var core *storeCore
					if native, ok := b.store.(*MemoryStorage); ok {
						core = native.storeCore
					} else {
						core = b.store.(*JournalStorage).storeCore
					}
					<-core.line
					original := core.appendFrame
					core.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
						if kind == 2 {
							return errors.New("uncertain open reconciliation")
						}
						if original != nil {
							return original(kind, ordinal, high, payload)
						}
						return nil
					}
					core.leave()
				} else {
					registry.mu.Lock()
					registry.nextListener = MaxID
					registry.mu.Unlock()
				}
				h, err := Open(bg, b.store, Options{Registry: registry})
				if err == nil || h != nil {
					if h != nil {
						_ = h.Close(bg)
					}
					t.Fatal("failedOpen retained Harness")
				}
				if stage == "running-reconciliation" && !errors.Is(err, ErrPoisoned) {
					t.Fatal("Open storage poison cause", err)
				}
				registry.mu.RLock()
				listeners := len(registry.listeners)
				registry.mu.RUnlock()
				if listeners != 0 || effects.Load() != 0 {
					t.Fatal("failedOpen retained registry/effect", listeners, effects.Load())
				}
				if _, err := b.store.Snapshot(bg); !errors.Is(err, ErrClosed) {
					t.Fatal("failedOpen did not release/close native store", err)
				}
				registry.mu.Lock()
				registry.nextListener = 0
				registry.mu.Unlock()
				reopened := reopenStoreAfterHarnessClose(t, b.store)
				second := taskTestHarness(t, reopened, def)
				record, ok, err := second.Task(bg, id)
				if err != nil || !ok || record.State.Status != "pending" || effects.Load() != 0 {
					t.Fatal("failedOpen reopen prefix/effects", record, err)
				}
				if record := waitPublicTask(t, second, id); record.State.Outcome.Status != "completed" || effects.Load() != 1 {
					t.Fatal(record, effects.Load())
				}
			})
		})
	}
}

func TestTaskRecoveryLateOwnedConversationEdgesHeldFailureOrMark(t *testing.T) {
	for _, intent := range []string{"held-failure", "abort-mark"} {
		t.Run(intent, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				abortEntered, releaseAbort := make(chan struct{}), make(chan struct{})
				started := make(chan ID, 4)
				var runs, childAborts atomic.Int64
				owner := taskDefinition(t, "task.late.owner", func(context.Context, TaskRecord, *TaskRuntime) error { return errors.New("owner old phase replayed") })
				ownerOptions := owner.options
				ownerOptions.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					close(abortEntered)
					<-releaseAbort
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
					})
				}
				var err error
				owner, err = DefineTask(ownerOptions)
				if err != nil {
					t.Fatal(err)
				}
				child := taskDefinition(t, "task.late.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					runs.Add(1)
					started <- r.TaskID()
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("later ordinary"), nil })
				})
				childOptions := child.options
				childOptions.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					childAborts.Add(1)
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
					})
				}
				child, err = DefineTask(childOptions)
				if err != nil {
					t.Fatal(err)
				}
				h := taskTestHarness(t, b.store, owner, child)
				cleanupTaskGates(t, releaseAbort)
				ownerID := createPublicTask(t, h, owner, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				var conversation, oldChild, lateChild ID
				_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
					var err error
					conversation, err = tx.MintID()
					if err != nil {
						return err
					}
					return tx.CreateConversation(Conversation{ID: conversation, Owner: ownerID})
				})
				if err != nil {
					t.Fatal(err)
				}
				_, err = h.CommitTasks(bg, conversation, func(tx *Tx) error {
					var err error
					oldChild, err = tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
					task, err := copyTask(tx.state.Tasks[ownerID], tx.limits)
					if err != nil {
						return err
					}
					if intent == "held-failure" {
						task.Status = "completing"
						task.Execution.Native.State = TaskState{Status: "completing", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "held failure"}}}
					} else {
						task = markTask(task)
					}
					return tx.stage(Write{Op: "put-task", Task: &task})
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := h.Close(bg); err != nil {
					t.Fatal(err)
				}
				second := taskTestHarness(t, reopenStoreAfterHarnessClose(t, b.store), owner, child)
				if runs.Load() != 0 || childAborts.Load() != 0 {
					t.Fatal("Open executed cascade")
				}
				_, err = second.CommitTasks(bg, conversation, func(tx *Tx) error {
					var err error
					lateChild, err = tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := second.Resume(bg); err != nil {
					t.Fatal(err)
				}
				for _, id := range []ID{oldChild, lateChild} {
					if record := waitPublicTask(t, second, id); record.State.Outcome.Status != "aborted" {
						t.Fatal("lateedge ordinary phase escaped confirmed cancellation", record)
					}
				}
				if intent == "abort-mark" {
					awaitTaskSignal(t, abortEntered)
					releaseTaskGate(releaseAbort)
				}
				record := waitPublicTask(t, second, ownerID)
				want := "failed"
				if intent == "abort-mark" {
					want = "aborted"
				}
				if record.State.Outcome.Status != want || runs.Load() != 0 || childAborts.Load() != 2 {
					t.Fatal("reopen edge cascade counts", record, runs.Load(), childAborts.Load())
				}
				// Retained terminal owner is a lookup/idle edge, not a cancellation
				// mandate for later admission in its already-existing conversation.
				var afterTerminal ID
				_, err = second.CommitTasks(bg, conversation, func(tx *Tx) error {
					var err error
					afterTerminal, err = tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				if record := waitPublicTask(t, second, afterTerminal); record.State.Outcome.Status != "completed" || record.AbortRequested {
					t.Fatal("terminal owner recascaded future work", record)
				}
				select {
				case id := <-started:
					if id != afterTerminal {
						t.Fatal("unexpected ordinary lateedge run", id)
					}
				default:
					t.Fatal("afterterminal actual host run missing")
				}
				if runs.Load() != 1 || childAborts.Load() != 2 {
					t.Fatal("lateedge terminal boundary counts")
				}
			})
		})
	}
}

func TestTaskRecoveryRejectedLateEdgeCascadeNeedsGenuineWake(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var runs, aborts atomic.Int64
		owner := taskDefinition(t, "task.cascade.retry.owner", func(context.Context, TaskRecord, *TaskRuntime) error {
			return errors.New("owner should retain failed hold")
		})
		child := taskDefinition(t, "task.cascade.retry.child", func(context.Context, TaskRecord, *TaskRuntime) error {
			runs.Add(1)
			return errors.New("pending descendant ran through failed cascade")
		})
		options := child.options
		options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			aborts.Add(1)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
			})
		}
		var err error
		child, err = DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
		first := taskTestHarness(t, b.store, owner, child)
		ownerID := createPublicTask(t, first, owner, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		var conversation, childID ID
		_, err = first.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			conversation, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.CreateConversation(Conversation{ID: conversation, Owner: ownerID})
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = first.CommitTasks(bg, conversation, func(tx *Tx) error {
			var err error
			childID, err = tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = first.CommitTasks(bg, 1, func(tx *Tx) error {
			task, err := copyTask(tx.state.Tasks[ownerID], tx.limits)
			if err != nil {
				return err
			}
			task.Status = "completing"
			task.Execution.Native.State = TaskState{Status: "completing", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "held failed owner"}}}
			return tx.stage(Write{Op: "put-task", Task: &task})
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := first.Close(bg); err != nil {
			t.Fatal(err)
		}
		store := reopenStoreAfterHarnessClose(t, b.store)
		reports := make(chan error, 4)
		second := taskTestHarnessOptions(t, store, Options{OnReport: func(err error) {
			select {
			case reports <- err:
			default:
			}
		}}, owner, child)
		var core *storeCore
		if native, ok := store.(*MemoryStorage); ok {
			core = native.storeCore
		} else {
			core = store.(*JournalStorage).storeCore
		}
		var original func(byte, uint64, uint64, []byte) error
		second.session.taskBookkeeping(func() { <-core.line; original = core.appendFrame; core.leave() })
		var markAttempts atomic.Int64
		failure := reject("late edge cascade mark rejected")
		installTaskAppend(t, second, core, func(kind byte, ordinal, high uint64, payload []byte) error {
			if kind == 2 {
				var record commitRecord
				if err := decodeStrict(payload, second.session.limits, second.session.limits.MaxFramePayloadBytes, &record); err != nil {
					return err
				}
				for _, write := range record.Writes {
					if write.Task != nil && write.Task.ID == childID && taskAborted(*write.Task) && write.Task.Status == "pending" && markAttempts.Add(1) == 1 {
						second.scheduler.enabled.Store(false)
						return failure
					}
				}
			}
			if original != nil {
				return original(kind, ordinal, high, payload)
			}
			return nil
		})
		if err := second.Resume(bg); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-reports:
			if !errors.Is(err, failure) {
				t.Fatal("cascade rejection cause", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("lateedge rejection not reported")
		}
		state, err := second.Snapshot(bg)
		if err != nil || state.Tasks[ownerID].Status != "completing" || taskAborted(state.Tasks[childID]) || runs.Load() != 0 || aborts.Load() != 0 {
			t.Fatal("rejected cascade partially adopted/dispatched", err)
		}
		for k := 0; k < 3; k++ {
			if err := second.scheduler.reconcile(); !errors.Is(err, errTaskPassAwaitingWake) {
				t.Fatal("cascade stale token retried", err)
			}
		}
		reservations, err := second.scheduler.reservePass(false)
		discardUnstartedTaskReservations(t, second, reservations)
		if err != nil || len(reservations) != 0 {
			t.Fatal("unmarked descendant escaped confirmed failure", err)
		}
		if markAttempts.Load() != 1 {
			t.Fatal("cascade selfretry")
		}
		second.session.taskBookkeeping(func() { second.scheduler.enabled.Store(true) })
		_, err = second.CommitTasks(bg, 1, func(tx *Tx) error {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "app.cascade-retry", Value: JSON{}})
		})
		if err != nil {
			t.Fatal(err)
		}
		// Public read-only Task polling cannot supply a Resume rescue epoch.
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for {
			record, _, err := second.Task(ctx, ownerID)
			if err != nil {
				t.Fatal(err)
			}
			if record.State.Status == "terminal" {
				if record.State.Outcome.Status != "failed" {
					t.Fatal(record)
				}
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("later adopted progress did not retry cascade")
			default:
				runtime.Gosched()
			}
		}
		childRecord, _, err := second.Task(bg, childID)
		if err != nil || childRecord.State.Outcome == nil || childRecord.State.Outcome.Status != "aborted" || runs.Load() != 0 || aborts.Load() != 1 || markAttempts.Load() != 2 {
			t.Fatal("cascade retry effects", childRecord, err, runs.Load(), aborts.Load(), markAttempts.Load())
		}
	})
}

func TestTaskRecoveryOpenMoreRunningHostsThanPersistedWrites(t *testing.T) {
	for _, backendName := range []string{"memory", "journal"} {
		t.Run(backendName, func(t *testing.T) {
			l := DefaultLimits()
			l.MaxWrites = 2
			l.MaxPage = 8
			image := &MemoryImage{}
			directory := t.TempDir() + "/private"
			open := func() Storage {
				var store Storage
				var err error
				if backendName == "memory" {
					store, err = OpenMemory(MemoryOptions{Image: image, Limits: &l})
				} else {
					store, err = OpenJournal(directory, JournalOptions{Limits: &l})
				}
				if err != nil {
					t.Fatal(err)
				}
				return store
			}
			started := make(chan ID, 5)
			var calls atomic.Int64
			def := taskDefinition(t, "task.open.multihost", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				calls.Add(1)
				started <- r.TaskID()
				<-ctx.Done()
				return ctx.Err()
			})
			registry := NewRegistry()
			if _, err := registry.RegisterTask(def); err != nil {
				t.Fatal(err)
			}
			first, err := Open(bg, open(), Options{Registry: registry})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = first.Close(bg) })
			ids := []ID{}
			for k := 0; k < 5; k++ {
				ids = append(ids, createPublicTask(t, first, def, JSON{"item": k}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
			}
			if err := first.Resume(bg); err != nil {
				t.Fatal(err)
			}
			for k := 0; k < 5; k++ {
				select {
				case <-started:
				case <-time.After(3 * time.Second):
					t.Fatal("actual running host set missing")
				}
			}
			before, err := first.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			if err := first.Close(bg); err != nil {
				t.Fatal(err)
			}
			secondStore := open()
			second, err := Open(bg, secondStore, Options{Registry: registry})
			if err != nil {
				_ = secondStore.Close(bg)
				t.Fatal("valid multi-host recovery exceeded persisted write budget", err)
			}
			t.Cleanup(func() { _ = second.Close(bg) })
			after, err := second.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 5 || after.Seq != before.Seq+5 {
				t.Fatal("Open dispatched effects or aggregated recovery", calls.Load(), after.Seq, before.Seq)
			}
			for _, id := range ids {
				task := after.Tasks[id]
				if task.Status != "pending" || task.Execution.Native.State.Status != "pending" || !equalTaskValue(task.Execution.Native.Input, before.Tasks[id].Execution.Native.Input, l) {
					t.Fatal("Open cardinality input/state preservation", task)
				}
			}
			inspection, err := second.InspectTasks(bg)
			if err != nil || inspection.Scheduling != "paused" || calls.Load() != 5 {
				t.Fatal("recovered Open enabled scheduling", inspection, err)
			}
		})
	}
}

// Supplementary mixed native/builtin persisted fixtures validate deterministic
// startup prefix/cancel/failure. Genuine old producer journals remain gated.
func TestTaskRecoveryOpenFailurePrefixMixedMetadataPreserved(t *testing.T) {
	for _, backendName := range []string{"memory", "journal"} {
		for _, mode := range []string{"reject-first", "reject-second", "cancel-after-first", "success"} {
			t.Run(backendName+"/"+mode, func(t *testing.T) {
				l := DefaultLimits()
				l.MaxWrites = 2
				l.MaxPage = 8
				image := &MemoryImage{}
				directory := t.TempDir() + "/private"
				open := func() Storage {
					var store Storage
					var err error
					if backendName == "memory" {
						store, err = OpenMemory(MemoryOptions{Image: image, Limits: &l})
					} else {
						store, err = OpenJournal(directory, JournalOptions{Limits: &l})
					}
					if err != nil {
						t.Fatal(err)
					}
					return store
				}
				store := open()
				var core *storeCore
				if native, ok := store.(*MemoryStorage); ok {
					core = native.storeCore
				} else {
					core = store.(*JournalStorage).storeCore
				}
				var effects atomic.Int64
				registry := NewRegistry()
				def := taskDefinition(t, "task.open.metadata", func(context.Context, TaskRecord, *TaskRuntime) error {
					effects.Add(1)
					return errors.New("Open executed host callback")
				})
				if _, err := registry.RegisterTask(def); err != nil {
					t.Fatal(err)
				}
				retained := []ID{}
				for k := 0; k < 3; k++ {
					id := mint(t, store)
					task := foundationTask(id, JSON{"index": k, "null": nil})
					task.Kind = def.Kind()
					task.Status = "running"
					task.Execution.Native.State.Status = "running"
					task.Execution.Native.Memos = map[string]*TaskValue{"toString": {Present: true, Value: JSON{"memo": k}}}
					if k == 1 {
						task = markTask(task)
					}
					apply(t, store, Write{Op: "put-task", Task: &task})
					retained = append(retained, id)
				}
				subID := mint(t, store)
				apply(t, store, Write{Op: "put-submission", Submission: &Submission{ID: subID, Conversation: 1, Type: "follow-up", Status: "pending", Value: JSON{"content": "persisted"}}})
				generation := mint(t, store)
				generationCP, err := dtoObject(generationCheckpoint{Phase: "tools", Submission: subID, Input: "persisted", Abort: true, Attempt: 2}, l)
				if err != nil {
					t.Fatal(err)
				}
				g := Task{ID: generation, Conversation: 1, Kind: "pi.generation", Status: "running", Checkpoint: generationCP, Execution: &TaskExecution{Tag: builtinTaskTag, Builtin: &BuiltinTaskExecution{AbortRequested: true, Memos: map[string]*TaskValue{"builtin": {Present: true, Value: []any{"kept", nil}}}}}}
				apply(t, store, Write{Op: "put-task", Task: &g})
				retained = append(retained, generation)
				tool := mint(t, store)
				toolCP, err := dtoObject(toolCheckpoint{Offer: toolOffer{Name: "persisted-tool", Implementation: "persisted", Version: 3, ReplaySafe: false}, CallID: "persisted-call", Arguments: JSON{"arg": "owned"}, Started: true, Abort: true, Output: "confirmed-prefix"}, l)
				if err != nil {
					t.Fatal(err)
				}
				task := Task{ID: tool, Owner: generation, Conversation: 1, Kind: "pi.tool", Status: "running", Checkpoint: toolCP, Execution: &TaskExecution{Tag: builtinTaskTag, Builtin: &BuiltinTaskExecution{AbortRequested: true, Memos: map[string]*TaskValue{"builtin": {Present: true, Value: nil}}}}}
				apply(t, store, Write{Op: "put-task", Task: &task})
				retained = append(retained, tool)
				before := snap(t, store)
				ctx, cancel := context.WithCancel(bg)
				defer cancel()
				var recoveryIDs []ID
				var admitted int
				<-core.line
				original := core.appendFrame
				core.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
					if kind == 2 {
						var record commitRecord
						if err := decodeStrict(payload, l, l.MaxFramePayloadBytes, &record); err != nil {
							return err
						}
						if len(record.Writes) != 1 || record.Writes[0].Task == nil {
							return reject("startup recovery did not use one task admission")
						}
						id := record.Writes[0].Task.ID
						recoveryIDs = append(recoveryIDs, id)
						if mode == "reject-first" && admitted == 0 || mode == "reject-second" && admitted == 1 {
							return reject("recovery prefix rejected")
						}
						if original != nil {
							if err := original(kind, ordinal, high, payload); err != nil {
								return err
							}
						}
						admitted++
						if mode == "cancel-after-first" && admitted == 1 {
							cancel()
						}
						return nil
					}
					if original != nil {
						return original(kind, ordinal, high, payload)
					}
					return nil
				}
				core.leave()
				h, err := Open(ctx, store, Options{Registry: registry})
				if mode == "success" {
					if err != nil {
						t.Fatal(err)
					}
					if err := h.Close(bg); err != nil {
						t.Fatal(err)
					}
				} else {
					if err == nil || h != nil {
						if h != nil {
							_ = h.Close(bg)
						}
						t.Fatal("prefix rejection/cancellation kept Harness")
					}
					if mode == "cancel-after-first" && !errors.Is(err, context.Canceled) {
						t.Fatal("prefix cancellation cause", err)
					}
					if _, err := store.Snapshot(bg); !errors.Is(err, ErrClosed) {
						t.Fatal("failed prefix retained storage ownership", err)
					}
				}
				if effects.Load() != 0 {
					t.Fatal("Open recovery invoked host effects")
				}
				wantPrefix := 0
				if mode == "reject-second" || mode == "cancel-after-first" {
					wantPrefix = 1
				}
				if mode == "success" {
					wantPrefix = len(retained)
				}
				if admitted != wantPrefix {
					t.Fatal("recovery prefix adoption count", admitted, wantPrefix)
				}
				for index, id := range recoveryIDs {
					if id != retained[index] {
						t.Fatal("recovery nondeterministic ID order", recoveryIDs, retained)
					}
				}
				reopenedStore := open()
				prefix := snap(t, reopenedStore)
				for index, id := range retained {
					actual := prefix.Tasks[id]
					expected, err := copyTask(before.Tasks[id], l)
					if err != nil {
						t.Fatal(err)
					}
					if index < wantPrefix {
						expected.Status = "pending"
						if expected.Execution.Native != nil {
							expected.Execution.Native.State.Status = "pending"
						}
					}
					if !equalJSONValue(actual.Checkpoint, expected.Checkpoint) || actual.Status != expected.Status || actual.Owner != expected.Owner || actual.Conversation != expected.Conversation {
						t.Fatal("recovery checkpoint/identity prefix", id)
					}
					a, err := encodeBounded(actual, l, l.MaxRecordBytes)
					if err != nil {
						t.Fatal(err)
					}
					b, err := encodeBounded(expected, l, l.MaxRecordBytes)
					if err != nil {
						t.Fatal(err)
					}
					var av, bv JSON
					if err := decodeStrict(a, l, l.MaxRecordBytes, &av); err != nil {
						t.Fatal(err)
					}
					if err := decodeStrict(b, l, l.MaxRecordBytes, &bv); err != nil {
						t.Fatal(err)
					}
					if !equalJSONValue(av, bv) {
						t.Fatal("recovery full execution/memo/mark/input metadata changed", id)
					}
				}
				second, err := Open(bg, reopenedStore, Options{Registry: registry})
				if err != nil {
					t.Fatal("retryOpen failed remaining valid prefix", err)
				}
				t.Cleanup(func() { _ = second.Close(bg) })
				final, err := second.Snapshot(bg)
				if err != nil || final.Seq != before.Seq+uint64(len(retained)) || effects.Load() != 0 {
					t.Fatal("retryOpen prefix count/effects", err)
				}
				for _, id := range retained {
					if final.Tasks[id].Status != "pending" {
						t.Fatal("retryOpen left running record", id)
					}
				}
			})
		}
	}
}

func TestTaskRecoveryRejectedCascadeExactOwnerVariants(t *testing.T) {
	for _, scenario := range []string{"reopened-marked-empty-new-latechild", "live-heldfailed-preexisting-child"} {
		t.Run(scenario, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				ownerEntered, releaseOwner, childEntered, releaseChild, abortEntered, releaseAbort, decideFailure := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
				var ordinaryRuns, childAborts atomic.Int64
				ownerRuntime, drainedRuntime := make(chan *TaskRuntime, 1), make(chan *TaskRuntime, 1)
				owner := taskDefinition(t, "task.exact-cascade.owner", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					ownerRuntime <- r
					close(ownerEntered)
					if scenario == "reopened-marked-empty-new-latechild" {
						<-releaseOwner
						return ctx.Err()
					}
					<-decideFailure
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "held owner failure"}}}, nil
					})
				})
				ownerOptions := owner.options
				ownerOptions.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					close(abortEntered)
					<-releaseAbort
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
					})
				}
				var err error
				owner, err = DefineTask(ownerOptions)
				if err != nil {
					t.Fatal(err)
				}
				child := taskDefinition(t, "task.exact-cascade.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					ordinaryRuns.Add(1)
					close(childEntered)
					<-releaseChild
					return ctx.Err()
				})
				childOptions := child.options
				childOptions.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					childAborts.Add(1)
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
					})
				}
				child, err = DefineTask(childOptions)
				if err != nil {
					t.Fatal(err)
				}
				finished := taskDefinition(t, "task.exact-cascade.finished", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					drainedRuntime <- r
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("drained"), nil })
				})
				reports := make(chan error, 4)
				options := Options{OnReport: func(err error) {
					select {
					case reports <- err:
					default:
					}
				}}
				first := taskTestHarnessOptions(t, b.store, options, owner, child, finished)
				cleanupTaskGates(t, releaseOwner, releaseChild, releaseAbort, decideFailure)
				ownerID := createPublicTask(t, first, owner, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				var conversation, childID, drained ID
				_, err = first.CommitTasks(bg, 1, func(tx *Tx) error {
					var err error
					conversation, err = tx.MintID()
					if err != nil {
						return err
					}
					return tx.CreateConversation(Conversation{ID: conversation, Owner: ownerID})
				})
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "reopened-marked-empty-new-latechild" {
					_, err = first.CommitTasks(bg, conversation, func(tx *Tx) error {
						var err error
						drained, err = tx.CreateTask(finished, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
						return err
					})
					if err != nil {
						t.Fatal(err)
					}
				} else {
					_, err = first.CommitTasks(bg, conversation, func(tx *Tx) error {
						var err error
						childID, err = tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
						return err
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := first.Resume(bg); err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, ownerEntered)
				pickedOwner := <-ownerRuntime
				var h *Harness
				var store Storage
				if scenario == "reopened-marked-empty-new-latechild" {
					if record := waitPublicTask(t, first, drained); record.State.Outcome.Status != "completed" {
						t.Fatal("inner task did not drain", record)
					}
					pickedDrained := <-drainedRuntime
					awaitTaskSignal(t, pickedDrained.done)
					if err := first.session.readTasks(bg, func(state Snapshot) error {
						if len(first.scheduler.ownedLive(state, ownerID)) != 0 {
							return reject("ownedconversation not drained before mark")
						}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
					markResult := make(chan error, 1)
					go func() { _, err := first.AbortTask(bg, ownerID); markResult <- err }()
					ctx, cancel := context.WithTimeout(bg, 3*time.Second)
					defer cancel()
					for {
						record, _, err := first.Task(ctx, ownerID)
						if err != nil {
							t.Fatal(err)
						}
						if record.AbortRequested {
							break
						}
						select {
						case <-ctx.Done():
							t.Fatal("actual owner mark missing")
						default:
							runtime.Gosched()
						}
					}
					closing := make(chan error, 1)
					go func() { closing <- first.Close(bg) }()
					for !first.closing.Load() {
						select {
						case <-ctx.Done():
							t.Fatal("owner Close seal missing")
						default:
							runtime.Gosched()
						}
					}
					releaseTaskGate(releaseOwner)
					select {
					case err := <-closing:
						if err != nil {
							t.Fatal(err)
						}
					case <-ctx.Done():
						t.Fatal("marked owner actualjoin missing")
					}
					select {
					case err := <-markResult:
						if err != nil {
							t.Fatal(err)
						}
					case <-ctx.Done():
						t.Fatal("marked owner callerjoin missing")
					}
					store = reopenStoreAfterHarnessClose(t, b.store)
					h = taskTestHarnessOptions(t, store, options, owner, child, finished)
					// Registered AFTER the reopened harness cleanup so every stubborn
					// host can return even if an earlier assertion fails (cleanup LIFO).
					cleanupTaskGates(t, releaseOwner, releaseChild, releaseAbort, decideFailure)
					opened, err := h.Snapshot(bg)
					if err != nil || !taskAborted(opened.Tasks[ownerID]) || len(taskOwnedLive(opened, ownerID)) != 0 || ordinaryRuns.Load() != 0 {
						t.Fatal("reopened marked empty prefix", err)
					}
					_, err = h.CommitTasks(bg, conversation, func(tx *Tx) error {
						var err error
						childID, err = tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
						return err
					})
					if err != nil {
						t.Fatal(err)
					}
				} else {
					awaitTaskSignal(t, childEntered)
					store = b.store
					h = first
				}
				var core *storeCore
				if native, ok := store.(*MemoryStorage); ok {
					core = native.storeCore
				} else {
					core = store.(*JournalStorage).storeCore
				}
				var original func(byte, uint64, uint64, []byte) error
				h.session.taskBookkeeping(func() { <-core.line; original = core.appendFrame; core.leave() })
				var attempts atomic.Int64
				failure := reject("exact cascade mark rejected")
				installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
					if kind == 2 {
						var record commitRecord
						if err := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &record); err != nil {
							return err
						}
						for _, write := range record.Writes {
							if scenario == "live-heldfailed-preexisting-child" && write.Task != nil && write.Task.ID == ownerID && write.Task.Status == "completing" {
								// Pause selection BEFORE the actual public runtime decision
								// leaves its admission. Wait for host-return bookkeeping,
								// then exercise one real cascade admission explicitly.
								h.scheduler.enabled.Store(false)
							}
							if write.Task != nil && write.Task.ID == childID && taskAborted(*write.Task) && !taskAborted(core.state.Tasks[childID]) && attempts.Add(1) == 1 {
								h.scheduler.enabled.Store(false)
								return failure
							}
						}
					}
					if original != nil {
						return original(kind, ordinal, high, payload)
					}
					return nil
				})
				if scenario == "reopened-marked-empty-new-latechild" {
					if err := h.Resume(bg); err != nil {
						t.Fatal(err)
					}
					select {
					case err := <-reports:
						if !errors.Is(err, failure) {
							t.Fatal(err)
						}
					case <-time.After(3 * time.Second):
						t.Fatal("exact cascade rejection missing")
					}
				} else {
					releaseTaskGate(decideFailure)
					awaitTaskSignal(t, pickedOwner.done)
					if err := h.scheduler.reconcile(); !errors.Is(err, failure) {
						t.Fatal("live held-failure cascade rejection missing", err)
					}
				}
				state, err := h.Snapshot(bg)
				if err != nil || taskAborted(state.Tasks[childID]) || terminalStatus(state.Tasks[childID].Status) {
					t.Fatal("rejected cascade adopted subset", err)
				}
				if scenario == "live-heldfailed-preexisting-child" && state.Tasks[ownerID].Status != "completing" {
					t.Fatal("live failure not held")
				}
				for k := 0; k < 3; k++ {
					if err := h.scheduler.reconcile(); !errors.Is(err, errTaskPassAwaitingWake) {
						t.Fatal("stale cascade retry", err)
					}
				}
				if attempts.Load() != 1 {
					t.Fatal("cascade selfretry")
				}
				h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(true) })
				_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
					id, err := tx.MintID()
					if err != nil {
						return err
					}
					return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "app.exact-retry", Value: JSON{}})
				})
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				for {
					record, _, err := h.Task(ctx, childID)
					if err != nil {
						t.Fatal(err)
					}
					if record.AbortRequested {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal("genuine latercommit did not retry exactcascade")
					default:
						runtime.Gosched()
					}
				}
				if scenario == "live-heldfailed-preexisting-child" {
					releaseTaskGate(releaseChild)
				}
				if record := waitPublicTask(t, h, childID); record.State.Outcome.Status != "aborted" {
					t.Fatal(record)
				}
				if scenario == "reopened-marked-empty-new-latechild" {
					awaitTaskSignal(t, abortEntered)
					releaseTaskGate(releaseAbort)
				}
				record := waitPublicTask(t, h, ownerID)
				want := "failed"
				wantRuns := int64(1)
				if scenario == "reopened-marked-empty-new-latechild" {
					want = "aborted"
					wantRuns = 0
				}
				if record.State.Outcome.Status != want || ordinaryRuns.Load() != wantRuns || childAborts.Load() != 1 || attempts.Load() != 2 {
					t.Fatal("exact cascade result/effect counts", record, ordinaryRuns.Load(), childAborts.Load(), attempts.Load())
				}
			})
		})
	}
}

func TestTaskRecoveryGenerationHostCallbacksPreparationAndDispatchedReceipts(t *testing.T) {
	for _, stage := range []string{"preparation", "dispatched"} {
		for _, failure := range []string{"model-panic", "model-nil", "options-panic", "options-error"} {
			t.Run(stage+"/"+failure, func(t *testing.T) {
				backends(t, func(t *testing.T, b backend) {
					var requests, modelCalls, optionCalls atomic.Int64
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); taskHTTPRound(w, nil) }))
					defer server.Close()
					model := fakeModel(goai.ApiOpenAICompletions)
					model.BaseURL = server.URL
					model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
					failCall := int64(1)
					if stage == "dispatched" {
						failCall = 2
					}
					secret := "private host callback credential"
					options := Options{Models: func(goai.Provider, string) *goai.Model {
						n := modelCalls.Add(1)
						if n == failCall && failure == "model-panic" {
							panic(secret)
						}
						if n == failCall && failure == "model-nil" {
							return nil
						}
						return model
					}, RequestOptions: func(context.Context, ModelRef) (*goai.StreamOptions, error) {
						n := optionCalls.Add(1)
						if n == failCall && failure == "options-panic" {
							panic(secret)
						}
						if n == failCall && failure == "options-error" {
							return nil, errors.New(secret)
						}
						return nil, nil
					}}
					h := openHarness(t, b.store, options)
					conv := root(t, h, ModelRef{model.Provider, model.ID})
					doc := createAppDocument(t, conv)
					sub, err := conv.Submit(bg, Input{Content: "host callback boundary"})
					if err != nil {
						t.Fatal(err)
					}
					if settled := waitSubmission(t, sub); settled.Submission.Status != "failed" {
						t.Fatal(settled)
					}
					idleContext, stopIdle := context.WithTimeout(bg, 3*time.Second)
					defer stopIdle()
					if err := h.WaitForIdle(idleContext); err != nil {
						t.Fatal(err)
					}
					state, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					code := "invalid_preparation"
					if stage == "dispatched" {
						code = "model_unavailable"
						if strings.HasPrefix(failure, "options") {
							code = "request_options_unavailable"
						}
					}
					var generation ID
					for id, task := range state.Tasks {
						if task.Kind != "pi.generation" {
							continue
						}
						generation = id
						var cp generationCheckpoint
						if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
							t.Fatal(err)
						}
						view, err := CanonicalTask(task, h.session.limits)
						if err != nil || task.Status != "failed" || view.State.Outcome.Status != "failed" || view.State.Outcome.Error.Message != code || cp.Phase != "terminal" {
							t.Fatal("callback receipt disposition", view, cp, err)
						}
						if (cp.Model != nil) != (stage == "dispatched") || cp.Attempt != map[bool]uint64{true: 1, false: 0}[stage == "dispatched"] {
							t.Fatal("preintent vs attempted metadata", cp)
						}
					}
					var assistant, user int
					for _, entry := range state.Entries {
						if entry.Kind != "message" {
							continue
						}
						var receipt messageReceipt
						if err := fromObject(entry.Value, &receipt, h.session.limits); err != nil {
							t.Fatal(err)
						}
						if receipt.Role == goai.RoleUser {
							user++
						}
						if entry.ByTask == generation {
							assistant++
							if receipt.ErrorCode != code || receipt.Usage != nil {
								t.Fatal("callback error receipt/usage", receipt)
							}
						}
					}
					if generation == 0 || assistant != 1 || user != map[bool]int{true: 1, false: 0}[stage == "dispatched"] || requests.Load() != 0 || !equalJSONValue(state.Documents[doc].Value["sum"], 0) {
						t.Fatal("callback preHTTP effects/entry placement", generation, assistant, user, requests.Load())
					}
					for _, d := range state.Documents {
						if d.Kind == "pi.usage" && len(d.Value["models"].(map[string]any)) != 0 {
							t.Fatal("undispatched callback charged usage")
						}
					}
					wire, err := encodeBounded(state.Tasks[generation], h.session.limits, h.session.limits.MaxRecordBytes)
					if err != nil || strings.Contains(string(wire), secret) {
						t.Fatal("host panic credential persisted", err)
					}
					beforeModels, beforeOptions := modelCalls.Load(), optionCalls.Load()
					if err := h.Close(bg); err != nil {
						t.Fatal(err)
					}
					second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
					if reopened, err := second.WaitForTask(bg, generation); err != nil || reopened.State.Outcome.Error.Message != code {
						t.Fatal("confirmed callback outcome reopen", reopened, err)
					}
					if requests.Load() != 0 || modelCalls.Load() != beforeModels || optionCalls.Load() != beforeOptions {
						t.Fatal("terminal callback replay")
					}
					// An independent public request proves the same real HTTP path is
					// usable after the bounded callback failure and reopen.
					again, err := second.Conversation(bg, conv.ID())
					if err != nil {
						t.Fatal(err)
					}
					healthy, err := again.Submit(bg, Input{Content: "healthy after callback failure"})
					if err != nil {
						t.Fatal(err)
					}
					if settled := waitSubmission(t, healthy); settled.Submission.Status != "done" || requests.Load() != 1 {
						t.Fatal("healthy HTTP successor", settled, requests.Load())
					}
				})
			})
		}
	}
}

func TestTaskRecoveryHostPanicDecidingStoragePrefixAndReopen(t *testing.T) {
	for _, callback := range []string{"options", "models"} {
		for _, stage := range []string{"preparation", "dispatched"} {
			for _, failure := range []string{"rejected", "uncertain-before", "uncertain-after"} {
				t.Run(callback+"/"+stage+"/"+failure, func(t *testing.T) {
					backends(t, func(t *testing.T, b backend) {
						var requests, optionsCalls, modelCalls, deciding atomic.Int64
						var recovered atomic.Bool
						server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); taskHTTPRound(w, nil) }))
						defer server.Close()
						model := fakeModel(goai.ApiOpenAICompletions)
						model.BaseURL = server.URL
						model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
						offline, release := make(chan *TaskRuntime, 1), make(chan struct{})
						reports := make(chan error, 4)
						var h *Harness
						failCallback := func(n int64) {
							target := int64(1)
							if stage == "dispatched" {
								target = 2
							}
							if !recovered.Load() && n == target {
								var picked *TaskRuntime
								h.session.taskBookkeeping(func() {
									for _, r := range h.scheduler.invocations {
										picked = r
										break
									}
								})
								offline <- picked
								<-release
								panic("private deciding callback credential")
							}
						}
						options := Options{Models: func(goai.Provider, string) *goai.Model {
							n := modelCalls.Add(1)
							if callback == "models" {
								failCallback(n)
							}
							return model
						}, RequestOptions: func(context.Context, ModelRef) (*goai.StreamOptions, error) {
							n := optionsCalls.Add(1)
							if callback == "options" {
								failCallback(n)
							}
							return nil, nil
						}, OnReport: func(err error) {
							select {
							case reports <- err:
							default:
							}
						}}
						h = openHarness(t, b.store, options)
						cleanupTaskGates(t, release)
						conv := root(t, h, ModelRef{model.Provider, model.ID})
						doc := createAppDocument(t, conv)
						sub, err := conv.Submit(bg, Input{Content: "callback deciding storage prefix"})
						if err != nil {
							t.Fatal(err)
						}
						var picked *TaskRuntime
						select {
						case picked = <-offline:
						case <-time.After(3 * time.Second):
							t.Fatal("callback actualhost barrier missing")
						}
						if picked == nil {
							t.Fatal("callback not dispatched by shared scheduler")
						}
						var core *storeCore
						if native, ok := b.store.(*MemoryStorage); ok {
							core = native.storeCore
						} else {
							core = b.store.(*JournalStorage).storeCore
						}
						var original func(byte, uint64, uint64, []byte) error
						h.session.taskBookkeeping(func() { <-core.line; original = core.appendFrame; core.leave() })
						rejection := reject("host callback deciding rejected")
						installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
							if kind == 2 {
								var record commitRecord
								if err := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &record); err != nil {
									return err
								}
								for _, write := range record.Writes {
									if write.Task != nil && write.Task.ID == picked.taskID && taskHasDecidedOutcome(*write.Task) && deciding.Add(1) == 1 {
										h.scheduler.enabled.Store(false)
										if failure == "rejected" {
											return rejection
										}
										if failure == "uncertain-after" && original != nil {
											if err := original(kind, ordinal, high, payload); err != nil {
												return err
											}
										}
										return errors.New("uncertain host deciding append")
									}
								}
							}
							if original != nil {
								return original(kind, ordinal, high, payload)
							}
							return nil
						})
						releaseTaskGate(release)
						awaitTaskSignal(t, picked.done)
						select {
						case err := <-reports:
							if failure == "rejected" && !errors.Is(err, rejection) || failure != "rejected" && !errors.Is(err, ErrPoisoned) {
								t.Fatal("actual deciding storage cause lost", err)
							}
						case <-time.After(3 * time.Second):
							t.Fatal("deciding storage report missing")
						}
						if deciding.Load() != 1 || requests.Load() != 0 {
							t.Fatal("unbounded callback fallback/effect", deciding.Load(), requests.Load())
						}
						if failure == "rejected" {
							state, err := h.Snapshot(bg)
							if err != nil || taskHasDecidedOutcome(state.Tasks[picked.taskID]) || !equalJSONValue(state.Documents[doc].Value["sum"], 0) {
								t.Fatal("definite reject adopted callback receipt", err)
							}
							h.session.taskBookkeeping(func() {
								if h.scheduler.retryAfter[picked.taskID] != picked.admissionEpoch {
									t.Error("callback fallback changed failed epoch")
								}
							})
							for _, entry := range state.Entries {
								if entry.ByTask == picked.taskID {
									t.Fatal("rejected deciding receipt leaked")
								}
							}
						}
						if err := h.Close(bg); err != nil {
							t.Fatal(err)
						}
						recovered.Store(true)
						beforeModelCalls, beforeOptionCalls := modelCalls.Load(), optionsCalls.Load()
						second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
						opened, err := second.Snapshot(bg)
						if err != nil {
							t.Fatal(err)
						}
						if modelCalls.Load() != beforeModelCalls || optionsCalls.Load() != beforeOptionCalls {
							t.Fatal("paused Open called host resolver")
						}
						_, journal := b.store.(*JournalStorage)
						confirmed := journal && failure == "uncertain-after"
						if taskHasDecidedOutcome(opened.Tasks[picked.taskID]) != confirmed || requests.Load() != 0 || !equalJSONValue(opened.Documents[doc].Value["sum"], 0) {
							t.Fatal("callback confirmed prefix/noOpeneffects", confirmed, err)
						}
						settled := waitSubmission(t, &SubmissionHandle{h: second, id: sub.ID()})
						want := "done"
						wantRequests := int64(1)
						if confirmed {
							want = "failed"
							wantRequests = 0
						}
						if settled.Submission.Status != want || requests.Load() != wantRequests {
							t.Fatal("callback deciding recovery", settled, requests.Load(), confirmed)
						}
						if confirmed && (modelCalls.Load() != beforeModelCalls || optionsCalls.Load() != beforeOptionCalls) {
							t.Fatal("confirmed resolver-error receipt replayed callbacks")
						}
						if !confirmed {
							additional := int64(1)
							if stage == "preparation" {
								additional = 2
							}
							if modelCalls.Load() != beforeModelCalls+additional || optionsCalls.Load() != beforeOptionCalls+additional {
								t.Fatal("unconfirmed attempt resolver count", modelCalls.Load(), optionsCalls.Load(), additional)
							}
						}
						final, err := second.Snapshot(bg)
						if err != nil {
							t.Fatal(err)
						}
						count := 0
						for _, entry := range final.Entries {
							if entry.ByTask == picked.taskID {
								count++
							}
						}
						if count != 1 || !equalJSONValue(final.Documents[doc].Value["sum"], 0) {
							t.Fatal("callback recovery duplicate receipt/app effect", count)
						}
						var cp generationCheckpoint
						if err := fromObject(final.Tasks[picked.taskID].Checkpoint, &cp, second.session.limits); err != nil {
							t.Fatal(err)
						}
						wantAttempt := uint64(1)
						if stage == "preparation" && confirmed {
							wantAttempt = 0
						}
						if stage == "dispatched" && !confirmed {
							wantAttempt = 2
						}
						if cp.Attempt != wantAttempt || cp.Phase != "terminal" || (cp.Model != nil) != (!confirmed || stage == "dispatched") {
							t.Fatal("callback attempt/model recovery metadata", cp, confirmed)
						}
						for _, entry := range final.Entries {
							if entry.ByTask != picked.taskID {
								continue
							}
							var receipt messageReceipt
							if err := fromObject(entry.Value, &receipt, second.session.limits); err != nil {
								t.Fatal(err)
							}
							wantCode := ""
							if confirmed {
								wantCode = "invalid_preparation"
								if stage == "dispatched" {
									wantCode = "request_options_unavailable"
									if callback == "models" {
										wantCode = "model_unavailable"
									}
								}
							}
							if receipt.ErrorCode != wantCode || confirmed && receipt.Usage != nil || !confirmed && (receipt.Usage == nil || receipt.Usage.TotalTokens != 5) {
								t.Fatal("callback confirmed receipt/known usage", receipt, confirmed)
							}
						}
						for _, d := range final.Documents {
							if d.Kind != "pi.usage" {
								continue
							}
							models := d.Value["models"].(map[string]any)
							if confirmed {
								if len(models) != 0 {
									t.Fatal("confirmed preHTTP failure acquired usage")
								}
							} else {
								var usage goai.Usage
								if err := fromObject(JSON(models[string(model.Provider)+"/"+model.ID].(map[string]any)), &usage, second.session.limits); err != nil || usage.TotalTokens != 5 {
									t.Fatal("callback recovery model usage", usage, err)
								}
							}
						}
					})
				})
			}
		}
	}

}

func TestTaskRecoveryToolValidatorHostFailureBeforeIntentAndDecidingReopen(t *testing.T) {
	for _, hostFailure := range []string{"error", "panic"} {
		for _, storageFailure := range []string{"none", "rejected", "uncertain-before", "uncertain-after"} {
			t.Run(hostFailure+"/"+storageFailure, func(t *testing.T) {
				backends(t, func(t *testing.T, b backend) {
					var requests, repairs, effects, deciding atomic.Int64
					beforeIntent, releaseRepair := make(chan struct{}), make(chan struct{})
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						if requests.Add(1) == 1 {
							taskHTTPRound(w, []string{"repair_failure"})
						} else {
							taskHTTPRound(w, nil)
						}
					}))
					defer server.Close()
					model := fakeModel(goai.ApiOpenAICompletions)
					model.BaseURL = server.URL
					model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
					registry := NewRegistry()
					if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "repair_failure", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "repair.failure", Version: 1, ReplaySafe: false, Validator: func(_ context.Context, args JSON) (JSON, error) {
						repairs.Add(1)
						args["private_repair"] = "credential must not persist"
						close(beforeIntent)
						<-releaseRepair
						if hostFailure == "panic" {
							panic("private repair credential")
						}
						return nil, errors.New("private repair credential")
					}, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
						effects.Add(1)
						return ToolResult{}, errors.New("invalid repair dispatched executor")
					}}); err != nil {
						t.Fatal(err)
					}
					reports := make(chan error, 4)
					options := Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }, OnReport: func(err error) {
						select {
						case reports <- err:
						default:
						}
					}}
					h := openHarness(t, b.store, options)
					cleanupTaskGates(t, releaseRepair)
					conv := root(t, h, ModelRef{model.Provider, model.ID})
					doc := createAppDocument(t, conv)
					sub, err := conv.Submit(bg, Input{Content: "host repair failure"})
					if err != nil {
						t.Fatal(err)
					}
					awaitTaskSignal(t, beforeIntent)
					prefix, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					for _, task := range prefix.Tasks {
						if task.Kind == "pi.tool" {
							t.Fatal("validator ran after child intent")
						}
					}
					for _, entry := range prefix.Entries {
						if entry.Kind == "message" {
							var r messageReceipt
							if err := fromObject(entry.Value, &r, h.session.limits); err != nil {
								t.Fatal(err)
							}
							if r.Role != goai.RoleUser {
								t.Fatal("validator failure before assistant receipt leaked", r)
							}
						}
					}
					var core *storeCore
					if native, ok := b.store.(*MemoryStorage); ok {
						core = native.storeCore
					} else {
						core = b.store.(*JournalStorage).storeCore
					}
					var original func(byte, uint64, uint64, []byte) error
					h.session.taskBookkeeping(func() { <-core.line; original = core.appendFrame; core.leave() })
					var toolID ID
					var toolDone <-chan struct{}
					rejection := reject("invalid repair receipt deciding rejected")
					installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
						if kind == 2 {
							var record commitRecord
							if err := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &record); err != nil {
								return err
							}
							for _, write := range record.Writes {
								if write.Task != nil && write.Task.Kind == "pi.tool" && taskHasDecidedOutcome(*write.Task) && deciding.Add(1) == 1 {
									toolID = write.Task.ID
									toolDone = h.scheduler.invocations[toolID].done
									h.scheduler.enabled.Store(false) // Keep parent continuation paused for prefix/usage inspection.
									if storageFailure == "rejected" {
										return rejection
									}
									if storageFailure == "uncertain-before" {
										return errors.New("uncertain repair receipt")
									}
									if storageFailure == "uncertain-after" {
										if original != nil {
											if err := original(kind, ordinal, high, payload); err != nil {
												return err
											}
										}
										return errors.New("uncertain repair receipt")
									}
								}
							}
						}
						if original != nil {
							return original(kind, ordinal, high, payload)
						}
						return nil
					})
					releaseTaskGate(releaseRepair)
					ctx, cancel := context.WithTimeout(bg, 3*time.Second)
					defer cancel()
					var done <-chan struct{}
					for {
						h.session.taskBookkeeping(func() { done = toolDone })
						if done != nil {
							break
						}
						select {
						case <-ctx.Done():
							t.Fatal("invalid repair tool deciding barrier missing")
						default:
							runtime.Gosched()
						}
					}
					awaitTaskSignal(t, done)
					if repairs.Load() != 1 || effects.Load() != 0 || requests.Load() != 1 || deciding.Load() != 1 {
						t.Fatal("repair/intent/effect counts", repairs.Load(), effects.Load(), requests.Load(), deciding.Load())
					}
					if storageFailure != "none" {
						select {
						case err := <-reports:
							if storageFailure == "rejected" && !errors.Is(err, rejection) || storageFailure != "rejected" && !errors.Is(err, ErrPoisoned) {
								t.Fatal("repair storage cause lost", err)
							}
						case <-ctx.Done():
							t.Fatal("repair deciding report missing")
						}
					}
					if storageFailure == "none" || storageFailure == "rejected" {
						state, err := h.Snapshot(bg)
						if err != nil {
							t.Fatal(err)
						}
						if taskHasDecidedOutcome(state.Tasks[toolID]) != (storageFailure == "none") || !equalJSONValue(state.Documents[doc].Value["sum"], 0) {
							t.Fatal("invalid repair deciding prefix")
						}
						for _, d := range state.Documents {
							if d.Kind == "pi.usage" {
								models := d.Value["models"].(map[string]any)
								var usage goai.Usage
								if err := fromObject(JSON(models[string(model.Provider)+"/"+model.ID].(map[string]any)), &usage, h.session.limits); err != nil || usage.TotalTokens != 5 || len(d.Value["tools"].(map[string]any)) != 0 {
									t.Fatal("known model usage / no tool bill", usage, err)
								}
							}
						}
					}
					if err := h.Close(bg); err != nil {
						t.Fatal(err)
					}
					second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
					opened, err := second.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					_, journal := b.store.(*JournalStorage)
					confirmed := storageFailure == "none" || journal && storageFailure == "uncertain-after"
					if taskHasDecidedOutcome(opened.Tasks[toolID]) != confirmed || effects.Load() != 0 || repairs.Load() != 1 || requests.Load() != 1 {
						t.Fatal("repair confirmedprefix/Open effects", confirmed)
					}
					if settled := waitSubmission(t, &SubmissionHandle{h: second, id: sub.ID()}); settled.Submission.Status != "done" {
						t.Fatal(settled)
					}
					final, err := second.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					var cp toolCheckpoint
					if err := fromObject(final.Tasks[toolID].Checkpoint, &cp, second.session.limits); err != nil {
						t.Fatal(err)
					}
					view, err := CanonicalTask(final.Tasks[toolID], second.session.limits)
					if err != nil || final.Tasks[toolID].Status != "done" || view.State.Outcome.Status != "failed" || cp.ErrorCode != "invalid_tool_arguments" || cp.Started || cp.Result == nil || !cp.Result.IsError || cp.Result.Usage != nil || len(cp.Arguments) != 0 {
						t.Fatal("invalid repair receipt/raw mapping", cp, view, err)
					}
					count := 0
					for _, entry := range final.Entries {
						if entry.ByTask == toolID {
							count++
						}
					}
					if count != 1 || effects.Load() != 0 || repairs.Load() != 1 || requests.Load() != 2 || !equalJSONValue(final.Documents[doc].Value["sum"], 0) {
						t.Fatal("repair recovery revalidated/executed/recharged", count, effects.Load(), repairs.Load(), requests.Load())
					}
					for _, d := range final.Documents {
						if d.Kind == "pi.usage" {
							models := d.Value["models"].(map[string]any)
							var usage goai.Usage
							if err := fromObject(JSON(models[string(model.Provider)+"/"+model.ID].(map[string]any)), &usage, second.session.limits); err != nil || usage.TotalTokens != 10 || len(d.Value["tools"].(map[string]any)) != 0 {
								t.Fatal("repair model/tool usage recovery", usage, err)
							}
						}
					}
					wire, err := encodeBounded(final.Tasks[toolID], second.session.limits, second.session.limits.MaxRecordBytes)
					if err != nil || strings.Contains(string(wire), "private_repair") || strings.Contains(string(wire), "credential") {
						t.Fatal("host repair mutation/panic payload persisted", err)
					}
				})
			})
		}
	}
}

func TestTaskRecoveryHostCallbackFencesPreserveNormalErrorIdentity(t *testing.T) {
	cause := errors.New("private normal host cause")
	ref := ModelRef{Provider: goai.ProviderOpenAI, ID: "host-fence"}
	options, err := callRequestOptions(func(context.Context, ModelRef) (*goai.StreamOptions, error) { return nil, cause }, bg, ref)
	if options != nil || !errors.Is(err, cause) {
		t.Fatal("options normal error identity changed", err)
	}
	value, err := callToolValidator(func(context.Context, JSON) (JSON, error) { return nil, cause }, bg, JSON{})
	if value != nil || !errors.Is(err, cause) {
		t.Fatal("validator normal error identity changed", err)
	}
	model, err := callModelResolver(func(goai.Provider, string) *goai.Model { return nil }, ref)
	if model != nil || err != nil {
		t.Fatal("nil normal model result changed", err)
	}
	for _, name := range []string{"model", "options", "validator"} {
		t.Run(name, func(t *testing.T) {
			var err error
			switch name {
			case "model":
				_, err = callModelResolver(func(goai.Provider, string) *goai.Model { panic("credential") }, ref)
			case "options":
				_, err = callRequestOptions(func(context.Context, ModelRef) (*goai.StreamOptions, error) { panic("credential") }, bg, ref)
			case "validator":
				_, err = callToolValidator(func(context.Context, JSON) (JSON, error) { panic("credential") }, bg, JSON{})
			}
			var rejected *StorageRejected
			if !errors.As(err, &rejected) || strings.Contains(err.Error(), "credential") {
				t.Fatal("host-only panic not redacted", err)
			}
		})
	}
}

// Err is reached off-line immediately after the actual Started admission in
// executeTool. This observes that boundary without a Session-line re-entry or
// changing the public executor. It is a test control, not a cancellation source.
type taskStartedErrContext struct {
	context.Context
	entered chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (c *taskStartedErrContext) Err() error {
	c.once.Do(func() { close(c.entered); <-c.release })
	return c.Context.Err()
}

func TestTaskRecoverySelectedAncestorBuiltinDispatchGuards(t *testing.T) {
	for _, stage := range []string{"initial-tool", "started-tool", "offline-generation"} {
		t.Run(stage, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				var requests, executions atomic.Int64
				entered, release := make(chan struct{}), make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); taskHTTPRound(w, nil) }))
				defer server.Close()
				model := fakeModel(goai.ApiOpenAICompletions)
				model.BaseURL = server.URL
				model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
				registry := NewRegistry()
				if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "inherited_guard", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "inherited.guard", Version: 1, ReplaySafe: true, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
					executions.Add(1)
					return ToolResult{Content: "forbidden"}, nil
				}}); err != nil {
					t.Fatal(err)
				}
				options := Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }}
				if stage == "offline-generation" {
					options.RequestOptions = func(context.Context, ModelRef) (*goai.StreamOptions, error) {
						close(entered)
						<-release
						return nil, nil
					}
				}
				h := openHarness(t, b.store, options)
				cleanupTaskGates(t, release)
				conv := root(t, h, ModelRef{model.Provider, model.ID})
				control := taskDefinition(t, "task.inherited.builtin.control", func(context.Context, TaskRecord, *TaskRuntime) error { return errors.New("control dispatched") })
				p := createPublicTask(t, h, control, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				f := createPublicTask(t, h, control, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: p}})
				s := createPublicTask(t, h, control, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: p}})
				var generation, tool, sub ID
				_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
					var err error
					sub, err = tx.MintID()
					if err != nil {
						return err
					}
					if err := tx.PutSubmission(Submission{ID: sub, Conversation: conv.ID(), Type: "follow-up", Status: "pending", Value: JSON{"content": "inherited"}}); err != nil {
						return err
					}
					generation, err = tx.MintID()
					if err != nil {
						return err
					}
					cp := generationCheckpoint{Phase: "intent", Submission: sub, Input: "inherited", Model: model, Agent: agentState{Model: ModelRef{model.Provider, model.ID}}, Messages: []MessageReceipt{userReceipt("inherited")}}
					if stage != "offline-generation" {
						tool, err = tx.MintID()
						if err != nil {
							return err
						}
						registered, _ := registry.current("inherited_guard")
						checkpoint, err := dtoObject(toolCheckpoint{Offer: registered.offer, CallID: "guard-call", Arguments: JSON{}}, tx.limits)
						if err != nil {
							return err
						}
						if err := tx.PutTask(Task{ID: tool, Conversation: conv.ID(), Owner: generation, Kind: "pi.tool", Status: "pending", Checkpoint: checkpoint}); err != nil {
							return err
						}
						cp.Phase = "tools"
						cp.Children = []ID{tool}
						entry, err := tx.MintID()
						if err != nil {
							return err
						}
						value, err := dtoObject(MessageReceipt{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, EmptyArguments: []int{0}, Content: []goai.ContentBlock{{Type: "toolCall", ID: "guard-call", Name: "inherited_guard", Arguments: JSON{}}}}, tx.limits)
						if err != nil {
							return err
						}
						if err := tx.AppendEntry(Entry{ID: entry, Conversation: 1, Kind: "message", Value: value, ByTask: generation}); err != nil {
							return err
						}
					}
					checkpoint, err := dtoObject(cp, tx.limits)
					if err != nil {
						return err
					}
					status := "pending"
					if stage != "offline-generation" {
						status = "completing"
					}
					if err := tx.PutTask(Task{ID: generation, Conversation: 1, Owner: s, Kind: "pi.generation", Status: status, Checkpoint: checkpoint}); err != nil {
						return err
					}
					parent, err := copyTask(tx.state.Tasks[p], tx.limits)
					if err != nil {
						return err
					}
					parent.Status = "waiting"
					parent.Execution.Native.State = TaskState{Status: "waiting", Checkpoint: JSON{"phase": "work"}, On: []ID{f, s}, Policy: "failFast"}
					return tx.stage(Write{Op: "put-task", Task: &parent})
				})
				if err != nil {
					t.Fatal(err)
				}
				h.session.taskBookkeeping(func() {
					h.scheduler.retryAfter[p] = ^uint64(0)
					h.scheduler.retryAfter[f] = ^uint64(0)
					h.scheduler.retryAfter[s] = ^uint64(0)
				})
				reservations, err := h.scheduler.reservePass(false)
				target := generation
				if stage != "offline-generation" {
					target = tool
				}
				if err != nil || len(reservations) != 1 || reservations[0].taskID != target {
					discardUnstartedTaskReservations(t, h, reservations)
					t.Fatal("valid pre-failure reservation", err)
				}
				picked := reservations[0]
				started := false
				t.Cleanup(func() {
					if !started {
						discardUnstartedTaskReservations(t, h, []*TaskRuntime{picked})
					}
				})
				if stage == "started-tool" {
					picked.context = &taskStartedErrContext{Context: picked.context, entered: entered, release: release}
				}
				if stage != "initial-tool" {
					started = true
					go h.scheduler.run(picked)
					awaitTaskSignal(t, entered)
				}
				_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
					failed, err := copyTask(tx.state.Tasks[f], tx.limits)
					if err != nil {
						return err
					}
					failed.Status = "failed"
					failed.Execution.Native.State = TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "inherited selected failure"}}}
					return tx.stage(Write{Op: "put-task", Task: &failed})
				})
				if err != nil {
					t.Fatal(err)
				}
				if stage == "initial-tool" {
					started = true
					go h.scheduler.run(picked)
				} else {
					releaseTaskGate(release)
				}
				awaitTaskSignal(t, picked.done)
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				if taskAborted(state.Tasks[s]) || taskAborted(state.Tasks[target]) || terminalStatus(state.Tasks[target].Status) || taskHasDecidedOutcome(state.Tasks[target]) || state.Submissions[sub].Status != "pending" || requests.Load() != 0 || executions.Load() != 0 {
					t.Fatal("pre-mark inherited builtin effect/outcome", stage, requests.Load(), executions.Load())
				}
				if stage != "offline-generation" {
					var cp toolCheckpoint
					if err := fromObject(state.Tasks[tool].Checkpoint, &cp, h.session.limits); err != nil || cp.Started != (stage == "started-tool") || cp.Result != nil {
						t.Fatal("Started boundary receipt", cp, err)
					}
				}
				// Public scheduling now adopts real marks before fresh abort dispatch.
				if settled := waitSubmission(t, &SubmissionHandle{h: h, id: sub}); settled.Submission.Status != "aborted" {
					t.Fatal(settled)
				}
				if requests.Load() != 0 || executions.Load() != 0 {
					t.Fatal("abort bridge escaped inherited intent")
				}
			})
		})
	}
}

func TestTaskRecoveryOpenReportsSecondaryCleanupFailureAndPreservesPrimary(t *testing.T) {
	for _, backendName := range []string{"memory", "journal"} {
		for _, stage := range []string{"recovery", "recovery-caller-cancel", "subscription"} {
			for _, reportMode := range []string{"returns", "panics"} {
				t.Run(backendName+"/"+stage+"/"+reportMode, func(t *testing.T) {
					image := &MemoryImage{}
					directory := t.TempDir() + "/private"
					openStore := func() Storage {
						var store Storage
						var err error
						if backendName == "memory" {
							store, err = OpenMemory(MemoryOptions{Image: image})
						} else {
							store, err = OpenJournal(directory, JournalOptions{})
						}
						if err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() { _ = store.Close(bg) })
						return store
					}
					store := openStore()
					var core *storeCore
					if memory, ok := store.(*MemoryStorage); ok {
						core = memory.storeCore
					} else {
						core = store.(*JournalStorage).storeCore
					}
					registry := NewRegistry()
					var id ID
					if stage != "subscription" {
						id = mint(t, store)
						task := foundationTask(id, JSON{"kept": "input"})
						task.Status = "running"
						task.Execution.Native.State.Status = "running"
						apply(t, store, Write{Op: "put-task", Task: &task})
					} else {
						registry.mu.Lock()
						registry.nextListener = MaxID
						registry.mu.Unlock()
					}
					caller, cancel := context.WithCancel(bg)
					defer cancel()
					primary := reject("failed owned Open recovery")
					secondary := errors.New("native finish failed AFTER releasing ownership")
					var closes, reports, effects atomic.Int64
					<-core.line
					finish, appendFrame := core.finish, core.appendFrame
					core.finish = func() error {
						closes.Add(1)
						if finish != nil {
							if err := finish(); err != nil {
								return err
							}
						}
						return secondary
					}
					if stage != "subscription" {
						core.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
							if kind == 2 {
								if stage == "recovery-caller-cancel" {
									cancel()
								}
								return primary
							}
							if appendFrame != nil {
								return appendFrame(kind, ordinal, high, payload)
							}
							return nil
						}
					}
					core.leave()
					var reported error
					h, err := Open(caller, store, Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { effects.Add(1); return nil }, OnReport: func(err error) {
						reports.Add(1)
						reported = err
						// These actual lock/closed-store reads would deadlock if report
						// were called while the failing admission owned either line.
						<-core.line
						core.leave()
						registry.mu.RLock()
						listeners := len(registry.listeners)
						registry.mu.RUnlock()
						if listeners != 0 {
							t.Error("cleanup report before listener disposal")
						}
						if _, err := store.Snapshot(bg); !errors.Is(err, ErrClosed) {
							t.Error("cleanup reported before store closure", err)
						}
						if reportMode == "panics" {
							panic("report callback panic must not replace Open error")
						}
					}})
					if h != nil {
						_ = h.Close(bg)
						t.Fatal("failedOpen retained harness")
					}
					if err == nil || errors.Is(err, secondary) || stage != "subscription" && !errors.Is(err, primary) || stage == "subscription" && !strings.Contains(err.Error(), "task registry listener capacity") {
						t.Fatal("cleanup replaced primaryOpen error", err)
					}
					if stage == "recovery-caller-cancel" && (!errors.Is(caller.Err(), context.Canceled) || errors.Is(err, context.Canceled)) {
						t.Fatal("postadmission cancel hid actual rejection", err)
					}
					if !errors.Is(reported, secondary) || reports.Load() != 1 || closes.Load() != 1 || effects.Load() != 0 {
						t.Fatal("secondary report/cleanup/effect count", reported, reports.Load(), closes.Load(), effects.Load())
					}
					// Open the SAME real journal/image without asking the old store
					// to Close again (its retained secondary error is intentional).
					reopened := openStore()
					next, err := Open(bg, reopened, Options{})
					if err != nil {
						t.Fatal("cleanup did not release realownership", err)
					}
					t.Cleanup(func() {
						if err := next.Close(bg); err != nil {
							t.Error(err)
						}
					})
					if id != 0 {
						record, found, err := next.Task(bg, id)
						if err != nil || !found || record.State.Status != "pending" || record.Input == nil || record.Input.Value.(map[string]any)["kept"] != "input" {
							t.Fatal("failedOpen confirmedprefix/reopen", record, err)
						}
					}
					if effects.Load() != 0 || reports.Load() != 1 || closes.Load() != 1 {
						t.Fatal("reopen repeated failed cleanup/report/effect")
					}
				})
			}
		}
	}
}

func TestTaskRecoverySuccessfulValidatorIntentStoragePrefixAndReopen(t *testing.T) {
	for _, failure := range []string{"rejected", "uncertain-before", "uncertain-after"} {
		t.Run(failure, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				var requests, repairs, executions, appCommits, admissions atomic.Int64
				var repeatRound atomic.Bool
				validatorEntered := make(chan *TaskRuntime, 1)
				releaseValidator, releaseExecutor := make(chan struct{}), make(chan struct{})
				executorEntered := make(chan struct{}, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					n := requests.Add(1)
					if n == 1 || n == 2 && repeatRound.Load() {
						taskHTTPRound(w, []string{"successful_repair"})
					} else {
						taskHTTPRound(w, nil)
					}
				}))
				defer server.Close()
				model := fakeModel(goai.ApiOpenAICompletions)
				model.BaseURL = server.URL
				model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
				registry := NewRegistry()
				var first *Harness
				var document ID
				if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "successful_repair", Parameters: json.RawMessage(`{"type":"object","properties":{"repaired":{"type":"string"}}}`)}, Implementation: "successful.repair", Version: 1, ReplaySafe: false,
					Validator: func(_ context.Context, args JSON) (JSON, error) {
						n := repairs.Add(1)
						if len(args) != 0 {
							return nil, errors.New("original model arguments were overwritten")
						}
						args["repaired"] = "accepted host repair"
						if n == 1 {
							var r *TaskRuntime
							first.session.taskBookkeeping(func() {
								for _, active := range first.scheduler.invocations {
									r = active
									break
								}
							})
							validatorEntered <- r
							<-releaseValidator
						}
						return args, nil
					}, Execute: func(ctx context.Context, args JSON, api *ToolAPI) (ToolResult, error) {
						executions.Add(1)
						if len(args) != 1 || args["repaired"] != "accepted host repair" {
							return ToolResult{}, errors.New("executor did not use confirmed repaired intent")
						}
						if _, err := api.MemoCandidate(ctx, "kept-repair", args); err != nil {
							return ToolResult{}, err
						}
						if err := api.Output("repair-prefix"); err != nil {
							return ToolResult{}, err
						}
						executorEntered <- struct{}{}
						<-releaseExecutor
						return ToolResult{Content: " repaired-result", Details: JSON{"repaired": true}, Usage: &goai.Usage{Output: 2, TotalTokens: 2}, Commit: func(tx *Tx) error {
							appCommits.Add(1)
							doc, err := tx.Document(document)
							if err != nil {
								return err
							}
							return doc.Set(JSON{"sum": 7})
						}}, nil
					}}); err != nil {
					t.Fatal(err)
				}
				reports := make(chan error, 4)
				options := Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }, OnReport: func(err error) {
					select {
					case reports <- err:
					default:
					}
				}}
				first = openHarness(t, b.store, options)
				cleanupTaskGates(t, releaseValidator, releaseExecutor)
				conversation := root(t, first, ModelRef{model.Provider, model.ID})
				document = createAppDocument(t, conversation)
				submission, err := conversation.Submit(bg, Input{Content: "repair accepting intent storage failure"})
				if err != nil {
					t.Fatal(err)
				}
				var r *TaskRuntime
				select {
				case r = <-validatorEntered:
				case <-time.After(3 * time.Second):
					t.Fatal("successful validator pre-intent barrier missing")
				}
				if r == nil {
					t.Fatal("validator has no actual parent invocation")
				}
				before, err := first.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				for _, task := range before.Tasks {
					if task.Kind == "pi.tool" {
						t.Fatal("tool intent preceded successful validation")
					}
				}
				var core *storeCore
				if memory, ok := b.store.(*MemoryStorage); ok {
					core = memory.storeCore
				} else {
					core = b.store.(*JournalStorage).storeCore
				}
				var original func(byte, uint64, uint64, []byte) error
				first.session.taskBookkeeping(func() { <-core.line; original = core.appendFrame; core.leave() })
				primary := reject("validated accepting tool intent rejected")
				var proposedTool ID
				installTaskAppend(t, first, core, func(kind byte, ordinal, high uint64, payload []byte) error {
					if kind == 2 {
						var record commitRecord
						if err := decodeStrict(payload, first.session.limits, first.session.limits.MaxFramePayloadBytes, &record); err != nil {
							return err
						}
						for _, write := range record.Writes {
							if write.Task == nil || write.Task.Kind != "pi.tool" {
								continue
							}
							if _, exists := core.state.Tasks[write.Task.ID]; exists {
								continue
							}
							var cp toolCheckpoint
							if err := fromObject(write.Task.Checkpoint, &cp, first.session.limits); err != nil {
								return err
							}
							if cp.Started || cp.Result != nil || cp.ErrorCode != "" || cp.Arguments["repaired"] != "accepted host repair" {
								return reject("invalid accepting-intent fixture")
							}
							if admissions.Add(1) == 1 {
								proposedTool = write.Task.ID
								first.scheduler.enabled.Store(false)
								if failure == "rejected" {
									return primary
								}
								if failure == "uncertain-after" && original != nil {
									if err := original(kind, ordinal, high, payload); err != nil {
										return err
									}
								}
								return errors.New("uncertain validated intent acceptance")
							}
						}
					}
					if original != nil {
						return original(kind, ordinal, high, payload)
					}
					return nil
				})
				releaseTaskGate(releaseValidator)
				awaitTaskSignal(t, r.done)
				select {
				case cause := <-reports:
					if failure == "rejected" && !errors.Is(cause, primary) || failure != "rejected" && !errors.Is(cause, ErrPoisoned) {
						t.Fatal("actual accepting storage error lost", cause)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("accepting storage report missing")
				}
				if admissions.Load() != 1 || repairs.Load() != 1 || requests.Load() != 1 || executions.Load() != 0 || appCommits.Load() != 0 || proposedTool == 0 {
					t.Fatal("pre-confirmation host/attempt effects", admissions.Load(), repairs.Load(), requests.Load(), executions.Load())
				}
				if failure == "rejected" {
					state, err := first.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					if state.Seq != before.Seq || len(state.Entries) != len(before.Entries) || len(state.Tasks) != len(before.Tasks) || !equalJSONValue(state.Documents[document].Value["sum"], 0) {
						t.Fatal("definite rejection adopted acceptance subset")
					}
					first.session.taskBookkeeping(func() {
						if first.scheduler.retryAfter[r.taskID] != r.admissionEpoch {
							t.Error("accepting-intent failure epoch changed")
						}
					})
				}
				if err := first.Close(bg); err != nil {
					t.Fatal(err)
				}
				_, journal := b.store.(*JournalStorage)
				confirmed := journal && failure == "uncertain-after"
				repeatRound.Store(!confirmed)
				second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
				cleanupTaskGates(t, releaseExecutor) // LIFO: reopened harness never joins a gated executor first.
				opened, err := second.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				toolCount, callReceipts := 0, 0
				var openedTool ID
				for id, task := range opened.Tasks {
					if task.Kind == "pi.tool" {
						toolCount++
						openedTool = id
						var cp toolCheckpoint
						if err := fromObject(task.Checkpoint, &cp, second.session.limits); err != nil || cp.Started || cp.Result != nil || cp.Arguments["repaired"] != "accepted host repair" {
							t.Fatal("confirmed accepting intent metadata", cp, err)
						}
					}
				}
				for _, entry := range opened.Entries {
					if entry.ByTask == r.taskID {
						callReceipts++
					}
				}
				wantCount := 0
				if confirmed {
					wantCount = 1
				}
				if toolCount != wantCount || callReceipts != wantCount || confirmed && openedTool != proposedTool || repairs.Load() != 1 || executions.Load() != 0 || requests.Load() != 1 || !equalJSONValue(opened.Documents[document].Value["sum"], 0) {
					t.Fatal("accepting confirmed prefix/no Open effects", confirmed, toolCount, callReceipts)
				}
				for _, d := range opened.Documents {
					if d.Kind == "pi.usage" {
						models := d.Value["models"].(map[string]any)
						if confirmed {
							var usage goai.Usage
							if err := fromObject(JSON(models[string(model.Provider)+"/"+model.ID].(map[string]any)), &usage, second.session.limits); err != nil || usage.TotalTokens != 5 {
								t.Fatal("accepted round known usage", usage, err)
							}
						} else if len(models) != 0 {
							t.Fatal("unconfirmed round usage adopted")
						}
						if len(d.Value["tools"].(map[string]any)) != 0 {
							t.Fatal("unexecuted intent charged tool")
						}
					}
				}
				if err := second.Resume(bg); err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, executorEntered)
				wantRepairs, wantRequests := int64(1), int64(1)
				if !confirmed {
					wantRepairs = 2
					wantRequests = 2
				}
				if repairs.Load() != wantRepairs || requests.Load() != wantRequests || executions.Load() != 1 || appCommits.Load() != 0 {
					t.Fatal("reopen revalidated confirmed intent or dispatched unconfirmed twice", repairs.Load(), requests.Load(), executions.Load())
				}
				state, err := second.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				var finalTool ID
				for id, task := range state.Tasks {
					if task.Kind == "pi.tool" {
						finalTool = id
						var cp toolCheckpoint
						if err := fromObject(task.Checkpoint, &cp, second.session.limits); err != nil || !cp.Started || cp.Output != "repair-prefix" || cp.Result != nil || task.Execution == nil || task.Execution.Builtin.Memos["kept-repair"] == nil {
							t.Fatal("executing intent metadata/prefix/memo", cp, err)
						}
					}
				}
				if confirmed && finalTool != proposedTool || !confirmed && finalTool == proposedTool {
					t.Fatal("confirmed vs unconfirmed tool identity", finalTool, proposedTool)
				}
				releaseTaskGate(releaseExecutor)
				settled := waitSubmission(t, &SubmissionHandle{h: second, id: submission.ID()})
				if settled.Submission.Status != "done" {
					t.Fatal(settled)
				}
				idle, stop := context.WithTimeout(bg, 3*time.Second)
				defer stop()
				if err := second.WaitForIdle(idle); err != nil {
					t.Fatal(err)
				}
				final, err := second.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				var cp toolCheckpoint
				if err := fromObject(final.Tasks[finalTool].Checkpoint, &cp, second.session.limits); err != nil {
					t.Fatal(err)
				}
				view, err := CanonicalTask(final.Tasks[finalTool], second.session.limits)
				if err != nil || final.Tasks[finalTool].Status != "done" || view.State.Outcome.Status != "completed" || view.Memos != nil || cp.Result == nil || cp.Result.IsError || len(cp.Result.Content) != 1 || cp.Result.Content[0].Text != "repair-prefix repaired-result" || cp.Result.Usage == nil || cp.Result.Usage.TotalTokens != 2 {
					t.Fatal("successful repaired final receipt", cp, view, err)
				}
				toolReceipts, toolCallReceipts := 0, 0
				for _, entry := range final.Entries {
					if entry.Kind != "message" {
						continue
					}
					var receipt messageReceipt
					if err := fromObject(entry.Value, &receipt, second.session.limits); err != nil {
						t.Fatal(err)
					}
					if entry.ByTask == finalTool {
						toolReceipts++
					}
					if receipt.Role == goai.RoleAssistant && receipt.StopReason == goai.StopReasonToolUse {
						toolCallReceipts++
						if len(receipt.Content) != 1 || len(receipt.Content[0].Arguments) != 0 {
							t.Fatal("repaired args overwrote original transcript", receipt)
						}
					}
				}
				if toolReceipts != 1 || toolCallReceipts != 1 || requests.Load() != wantRequests+1 || repairs.Load() != wantRepairs || executions.Load() != 1 || appCommits.Load() != 1 || !equalJSONValue(final.Documents[document].Value["sum"], 7) {
					t.Fatal("successful intent replay/entry/app accounting", toolReceipts, toolCallReceipts, requests.Load(), repairs.Load(), executions.Load(), appCommits.Load())
				}
				assertTaskToolSpend(t, final, conversation.ID(), "successful_repair", 2)
				for _, d := range final.Documents {
					if d.Kind == "pi.usage" {
						var usage goai.Usage
						models := d.Value["models"].(map[string]any)
						if err := fromObject(JSON(models[string(model.Provider)+"/"+model.ID].(map[string]any)), &usage, second.session.limits); err != nil || usage.TotalTokens != 10 {
							t.Fatal("accepted round/successor double-charged", usage, err)
						}
					}
				}
				if err := second.Close(bg); err != nil {
					t.Fatal(err)
				}
				third := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
				if record, err := third.WaitForTask(bg, finalTool); err != nil || record.State.Outcome.Status != "completed" {
					t.Fatal("confirmed successful receipt reopen", record, err)
				}
				if requests.Load() != wantRequests+1 || repairs.Load() != wantRepairs || executions.Load() != 1 || appCommits.Load() != 1 {
					t.Fatal("terminal successful intent replayed")
				}
			})
		})
	}
}

// Test control at the last OFF-line tools-round snapshot. It publishes a
// detached real snapshot first, allowing Close or late public admissions before
// the adapter queues its authoritative continuation callback. No FIFO proof.
type taskContinuationSnapshotGate struct {
	Storage
	reads   atomic.Int64
	target  int64
	entered chan Snapshot
	release <-chan struct{}
}

func (s *taskContinuationSnapshotGate) Snapshot(ctx context.Context) (Snapshot, error) {
	value, err := s.Storage.Snapshot(ctx)
	if err == nil && s.reads.Add(1) == s.target {
		s.entered <- value
		<-s.release
	}
	return value, err
}

func TestTaskRecoveryOrderedToolContinuationAdmissionCloseAndLateOwnedWork(t *testing.T) {
	for _, placement := range []string{"close", "close-queued-admission", "late-owned-host"} {
		t.Run(placement, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				var requests, executions, childRuns atomic.Int64
				toolEntered, releaseTool, releaseSnapshot, childEntered, releaseChild, childTerminal, returnChild := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
				toolHost, parentHost, childHost := make(chan *TaskRuntime, 1), make(chan *TaskRuntime, 1), make(chan *TaskRuntime, 1)
				var first *Harness
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					if requests.Add(1) == 1 {
						taskHTTPRound(w, []string{"continuation_guard"})
					} else {
						taskHTTPRound(w, nil)
					}
				}))
				defer server.Close()
				model := fakeModel(goai.ApiOpenAICompletions)
				model.BaseURL = server.URL
				model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
				child := taskDefinition(t, "task.continuation.late", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					childRuns.Add(1)
					childHost <- r
					close(childEntered)
					<-releaseChild
					if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("late child drained"), nil }); err != nil {
						return err
					}
					close(childTerminal)
					<-returnChild
					return nil
				})
				registry := NewRegistry()
				if _, err := registry.RegisterTask(child); err != nil {
					t.Fatal(err)
				}
				var parent, ownedConversation ID
				if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "continuation_guard", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "continuation.guard", Version: 1, Execute: func(_ context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
					executions.Add(1)
					first.session.taskBookkeeping(func() {
						r := first.scheduler.invocations[api.TaskID()]
						toolHost <- r
						parent = api.task.Owner
						parentHost <- first.scheduler.invocations[parent]
						first.scheduler.enabled.Store(false)
					})
					close(toolEntered)
					<-releaseTool
					return ToolResult{Content: "ordered result", Usage: &goai.Usage{Output: 2, TotalTokens: 2}}, nil
				}}); err != nil {
					t.Fatal(err)
				}
				options := Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }}
				first = openHarness(t, b.store, options)
				cleanupTaskGates(t, releaseTool, releaseSnapshot, releaseChild, returnChild)
				conversation := root(t, first, ModelRef{model.Provider, model.ID})
				sub, err := conversation.Submit(bg, Input{Content: "real ordered tool continuation"})
				if err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, toolEntered)
				pickedTool, oldParent := <-toolHost, <-parentHost
				// Original tool parent host returns while the child executor is held;
				// every subsequent continuation reservation is a distinct real host.
				if oldParent != nil {
					awaitTaskSignal(t, oldParent.done)
				}
				_, err = first.CommitTasks(bg, conversation.ID(), func(tx *Tx) error {
					var err error
					ownedConversation, err = tx.MintID()
					if err != nil {
						return err
					}
					return tx.CreateConversation(Conversation{ID: ownedConversation, Owner: parent})
				})
				if err != nil {
					t.Fatal(err)
				}
				releaseTaskGate(releaseTool)
				awaitTaskSignal(t, pickedTool.done)
				state, err := first.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				var round generationCheckpoint
				if err := fromObject(state.Tasks[parent].Checkpoint, &round, first.session.limits); err != nil || round.Phase != "tools" || len(round.Children) != 1 || !terminalStatus(state.Tasks[round.Children[0]].Status) {
					t.Fatal("actual ordered round not drained", round, err)
				}
				assertTaskToolSpend(t, state, conversation.ID(), "continuation_guard", 2)
				assertRoundReceipts := func(snapshot Snapshot, limits Limits) {
					t.Helper()
					var callSeq, callPosition, resultSeq, resultPosition uint64
					calls, results := 0, 0
					for _, entry := range snapshot.Entries {
						if entry.Kind != "message" {
							continue
						}
						var receipt messageReceipt
						if err := fromObject(entry.Value, &receipt, limits); err != nil {
							t.Fatal(err)
						}
						if receipt.Role == goai.RoleAssistant && receipt.StopReason == goai.StopReasonToolUse {
							calls++
							callSeq, callPosition = entry.Seq, entry.Position
							if entry.ByTask != parent || len(receipt.Content) != 1 || receipt.Content[0].ID != "call-0" {
								t.Fatal("ordered assistant call identity", receipt)
							}
						}
						if receipt.Role == goai.RoleToolResult {
							results++
							resultSeq, resultPosition = entry.Seq, entry.Position
							if entry.ByTask != round.Children[0] || receipt.ToolCallID != "call-0" || receipt.ToolName != "continuation_guard" || receipt.IsError || receipt.Usage == nil || receipt.Usage.TotalTokens != 2 || len(receipt.Content) != 1 || receipt.Content[0].Text != "ordered result" {
								t.Fatal("ordered result identity", receipt)
							}
						}
					}
					if calls != 1 || results != 1 || callSeq > resultSeq || callSeq == resultSeq && callPosition >= resultPosition {
						t.Fatal("ordered round receipts missing/replayed", calls, results)
					}
				}
				assertRoundReceipts(state, first.session.limits)
				reservation, err := first.scheduler.reservePass(false)
				if err != nil || len(reservation) != 1 || reservation[0].taskID != parent {
					discardUnstartedTaskReservations(t, first, reservation)
					t.Fatal("real drained-round continuation reservation", err)
				}
				actual := reservation[0]
				started := false
				t.Cleanup(func() {
					if !started {
						discardUnstartedTaskReservations(t, first, []*TaskRuntime{actual})
					}
				})
				if placement == "close" || placement == "close-queued-admission" {
					// Snapshot wrappers still retain Session.enter until RETURN.
					// Safe for Close's atomic seal, never for public late admission.
					targetRead := int64(2 + len(round.Children))
					if placement == "close-queued-admission" {
						targetRead++
					}
					seam := &taskContinuationSnapshotGate{Storage: b.store, target: targetRead, entered: make(chan Snapshot, 1), release: releaseSnapshot}
					first.session.taskBookkeeping(func() { first.session.store = seam })
					started = true
					go first.scheduler.run(actual)
					var prefix Snapshot
					select {
					case prefix = <-seam.entered:
					case <-time.After(3 * time.Second):
						t.Fatal("real ordered-round Close snapshot barrier missing")
					}
					if prefix.Tasks[parent].Status != "running" || requests.Load() != 1 || executions.Load() != 1 {
						t.Fatal("continuation host prefix")
					}
					caller, cancel := context.WithCancel(bg)
					cancel()
					if err := first.Close(caller); !errors.Is(err, context.Canceled) {
						t.Fatal(err)
					}
					if !first.closing.Load() {
						t.Fatal("Close not sealed")
					}
					select {
					case <-first.closeDone:
						t.Fatal("Close did not retain actual continuation host")
					default:
					}
					releaseTaskGate(releaseSnapshot)
					awaitTaskSignal(t, actual.done)
					if err := first.Close(bg); err != nil {
						t.Fatal(err)
					}
					reopened := reopenStoreAfterHarnessClose(t, b.store)
					after := snap(t, reopened)
					var cp generationCheckpoint
					if err := fromObject(after.Tasks[parent].Checkpoint, &cp, first.session.limits); err != nil || after.Seq != prefix.Seq || cp.Phase != "tools" || len(cp.Children) != 1 || after.Submissions[sub.ID()].Status != "pending" || requests.Load() != 1 || executions.Load() != 1 {
						t.Fatal("fresh continuation adopted after Close", cp, err)
					}
					second := openHarness(t, reopened, options)
					if settled := waitSubmission(t, &SubmissionHandle{h: second, id: sub.ID()}); settled.Submission.Status != "done" {
						t.Fatal(settled)
					}
					if requests.Load() != 2 || executions.Load() != 1 {
						t.Fatal("Close continuation replayed tool or lost resumed request")
					}
					final, err := second.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					assertTaskToolSpend(t, final, conversation.ID(), "continuation_guard", 2)
					assertRoundReceipts(final, second.session.limits)
				} else {
					// reservePass returned after its real admission settled. The
					// Session line is free; host launch is explicitly controlled.
					// This covers late work AFTER reservation, not a post-offline
					// snapshot race (that separate placement stays unassigned).
					var late ID
					_, err = first.CommitTasks(bg, ownedConversation, func(tx *Tx) error {
						var err error
						late, err = tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
						return err
					})
					if err != nil {
						t.Fatal(err)
					}
					afterLate, err := first.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					started = true
					go first.scheduler.run(actual)
					awaitTaskSignal(t, actual.done)
					after, err := first.Snapshot(bg)
					var cp generationCheckpoint
					if err != nil {
						t.Fatal(err)
					}
					if err := fromObject(after.Tasks[parent].Checkpoint, &cp, first.session.limits); err != nil || after.Seq != afterLate.Seq || cp.Phase != "tools" || requests.Load() != 1 || executions.Load() != 1 {
						t.Fatal("late ownedconversation work bypassed continuation drain", cp, err)
					}
					reserved, err := first.scheduler.reservePass(false)
					if err != nil || len(reserved) != 1 || reserved[0].taskID != late {
						discardUnstartedTaskReservations(t, first, reserved)
						t.Fatal("latechild retained continuation priority", err)
					}
					lateRuntime := reserved[0]
					childStarted := false
					t.Cleanup(func() {
						if !childStarted {
							discardUnstartedTaskReservations(t, first, []*TaskRuntime{lateRuntime})
						}
					})
					childStarted = true
					go first.scheduler.run(lateRuntime)
					awaitTaskSignal(t, childEntered)
					if <-childHost != lateRuntime {
						t.Fatal("late host identity")
					}
					releaseTaskGate(releaseChild)
					awaitTaskSignal(t, childTerminal)
					blocked, err := first.scheduler.reservePass(false)
					discardUnstartedTaskReservations(t, first, blocked)
					if err != nil || len(blocked) != 0 || requests.Load() != 1 {
						t.Fatal("terminal-unreturned ownedhost lost join", err)
					}
					releaseTaskGate(returnChild)
					awaitTaskSignal(t, lateRuntime.done)
					// Actual return is the genuine capacity/progress witness. No Resume
					// rescue epoch is needed before the next native reservation.
					continued, err := first.scheduler.reservePass(false)
					if err != nil || len(continued) != 1 || continued[0].taskID != parent {
						discardUnstartedTaskReservations(t, first, continued)
						t.Fatal("actual latehost return did not release continuation", err)
					}
					continueRuntime := continued[0]
					continuationStarted := false
					t.Cleanup(func() {
						if !continuationStarted {
							discardUnstartedTaskReservations(t, first, []*TaskRuntime{continueRuntime})
						}
					})
					continuationStarted = true
					go first.scheduler.run(continueRuntime)
					awaitTaskSignal(t, continueRuntime.done)
					if settled := waitSubmission(t, sub); settled.Submission.Status != "done" {
						t.Fatal(settled)
					}
					if childRuns.Load() != 1 || executions.Load() != 1 || requests.Load() != 2 {
						t.Fatal("latechild continuation effects duplicated")
					}
					final, err := first.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					assertRoundReceipts(final, first.session.limits)
					assertTaskToolSpend(t, final, conversation.ID(), "continuation_guard", 2)
				}
			})
		})
	}
}

func TestTaskRecoveryPublicBlockedAbortDirectReturnRetiresDocuments(t *testing.T) {
	for _, variant := range []string{"missing", "too-old", "no-migrate", "cached-throw", "cached-invalid", "untried-migration", "replacement-token"} {
		t.Run(variant, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				var phases, migrations atomic.Int64
				definition := taskDefinition(t, "task.direct-orphan", func(context.Context, TaskRecord, *TaskRuntime) error {
					phases.Add(1)
					return errors.New("direct abort must not dispatch phase")
				})
				h := taskTestHarness(t, b.store)
				id := createPublicTask(t, h, definition, JSON{"kept": "input"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				foreign := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				var document ID
				_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
					var err error
					document, err = tx.MintID()
					if err != nil {
						return err
					}
					if _, err := tx.CreateDocument(Document{ID: document, Scope: "task", Owner: id, Kind: "app.direct-orphan", Version: 1, Value: JSON{"kept": true}}); err != nil {
						return err
					}
					task, err := copyTask(tx.state.Tasks[id], tx.limits)
					if err != nil {
						return err
					}
					task.Status = "waiting"
					task.Execution.Native.Memos = map[string]*TaskValue{"kept": {Present: true, Value: 1}}
					task.Execution.Native.State = TaskState{Status: "waiting", Checkpoint: JSON{"phase": "work"}, On: []ID{foreign}, Policy: "allSettled"}
					return tx.stage(Write{Op: "put-task", Task: &task})
				})
				if err != nil {
					t.Fatal(err)
				}
				var fitted *TaskDefinition
				if variant != "missing" {
					options := definition.options
					options.Version = 2
					if variant == "too-old" {
						options.Version = 1
						_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
							task, err := copyTask(tx.state.Tasks[id], tx.limits)
							if err != nil {
								return err
							}
							task.Execution.Native.Version = 2
							return tx.stage(Write{Op: "put-task", Task: &task})
						})
						if err != nil {
							t.Fatal(err)
						}
					}
					if variant != "no-migrate" && variant != "too-old" {
						options.Migrate = func(input any, checkpoint JSON, _ uint64) (any, JSON, error) {
							migrations.Add(1)
							if variant == "cached-invalid" {
								return func() {}, checkpoint, nil
							}
							return nil, nil, errors.New("known failed migration")
						}
					}
					fitted, err = DefineTask(options)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := h.options.Registry.RegisterTask(fitted); err != nil {
						t.Fatal(err)
					}
				}
				if variant == "cached-throw" || variant == "cached-invalid" || variant == "replacement-token" {
					// Fail migration through the actual reservation resolver, never
					// invoke it from AbortTask. Clear foreign wait temporarily so the
					// reservation is eligible for resolution but still runs no host.
					_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
						task, err := copyTask(tx.state.Tasks[id], tx.limits)
						if err != nil {
							return err
						}
						task.Status = "pending"
						task.Execution.Native.State = TaskState{Status: "pending", Checkpoint: JSON{"phase": "work"}}
						return tx.stage(Write{Op: "put-task", Task: &task})
					})
					if err != nil {
						t.Fatal(err)
					}
					h.session.taskBookkeeping(func() { h.scheduler.retryAfter[foreign] = ^uint64(0) })
					reserved, err := h.scheduler.reservePass(false)
					discardUnstartedTaskReservations(t, h, reserved)
					if err != nil || len(reserved) != 0 || migrations.Load() != 1 {
						t.Fatal("actual failed migration cache", err, migrations.Load())
					}
					_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
						task, err := copyTask(tx.state.Tasks[id], tx.limits)
						if err != nil {
							return err
						}
						task.Status = "waiting"
						task.Execution.Native.State = TaskState{Status: "waiting", Checkpoint: JSON{"phase": "work"}, On: []ID{foreign}, Policy: "allSettled"}
						return tx.stage(Write{Op: "put-task", Task: &task})
					})
					if err != nil {
						t.Fatal(err)
					}
					if variant == "replacement-token" {
						replacement, err := DefineTask(fitted.options)
						if err != nil {
							t.Fatal(err)
						}
						if _, err := h.options.Registry.RegisterTask(replacement); err != nil {
							t.Fatal(err)
						}
					}
				}
				before, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				beforeMigrations := migrations.Load()
				probe := &taskAdmissionContext{Context: bg, queued: make(chan struct{})}
				type result struct {
					status string
					err    error
				}
				returned := make(chan result, 1)
				// Actual public Resume happens while this line is held; its Done
				// witness proves AbortTask reached original admission. Disable ONLY
				// autonomous selection before release, without FIFO assumptions.
				_, err = h.session.taskCommit(bg, func(*Tx) error {
					go func() { status, err := h.AbortTask(probe, id); returned <- result{status, err} }()
					select {
					case <-probe.queued:
					case <-time.After(3 * time.Second):
						return errors.New("public abort not queued")
					}
					h.scheduler.enabled.Store(false)
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				select {
				case result := <-returned:
					if result.err != nil || result.status != "marked" {
						t.Fatal(result)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("public direct abort return missing")
				}
				record, found, err := h.Task(bg, id)
				if err != nil || !found || !record.AbortRequested {
					t.Fatal(record, err)
				}
				direct := variant != "untried-migration" && variant != "replacement-token"
				want := "migration_failed"
				if variant == "missing" {
					want = "missing_task"
				}
				if variant == "too-old" {
					want = "task_too_old"
				}
				if direct {
					if record.State.Status != "terminal" || record.State.Outcome == nil || record.State.Outcome.Status != "orphaned" || record.State.Outcome.Reason != want || record.Memos != nil {
						t.Fatal("marked return precedes direct orphan", record)
					}
				} else if record.State.Status == "terminal" || record.State.Outcome != nil {
					t.Fatal("Abort evaluated untried/replacement migration", record)
				}
				after, err := h.Snapshot(bg)
				if err != nil || after.Seq != before.Seq+1 || after.Documents[document].Retired != direct || migrations.Load() != beforeMigrations || phases.Load() != 0 || taskAborted(after.Tasks[foreign]) || after.Tasks[foreign].Status != before.Tasks[foreign].Status {
					t.Fatal("direct admission effect/retirement/foreignwait", err)
				}
				if !equalTaskValue(before.Tasks[id].Execution.Native.Input, after.Tasks[id].Execution.Native.Input, h.session.limits) {
					t.Fatal("orphan input changed")
				}
				query := b.store.(DocumentHistoryStorage)
				_, current, err := query.FindDocument(bg, DocumentAddress{Scope: "task", Owner: id, Kind: "app.direct-orphan"}, CurrentDocumentPoint())
				if err != nil || current == direct {
					t.Fatal("direct retirement currentquery", current, err)
				}
				if direct && after.Documents[document].RetiredAt != after.Seq {
					t.Fatal("orphan retirement not same admission")
				}
			})
		})
	}
}

func TestTaskRecoveryPublicBlockedAbortPreservesHeldAndOwnedActualJoin(t *testing.T) {
	for _, variant := range []string{"held", "owned-running", "owned-terminal-unreturned"} {
		t.Run(variant, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				entered, decide, terminal, returnHost := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
				picked := make(chan *TaskRuntime, 1)
				owner := taskDefinition(t, "task.direct-orphan.excluded", func(context.Context, TaskRecord, *TaskRuntime) error {
					return errors.New("missing owner phase invoked")
				})
				child := taskDefinition(t, "task.direct-orphan.owned", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					picked <- r
					close(entered)
					<-decide
					if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("child"), nil }); err != nil {
						return err
					}
					close(terminal)
					<-returnHost
					return nil
				})
				h := taskTestHarness(t, b.store, child)
				cleanupTaskGates(t, decide, returnHost)
				id := createPublicTask(t, h, owner, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				var childID ID
				if variant != "held" {
					childID = createPublicTask(t, h, child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: id}})
					if err := h.Resume(bg); err != nil {
						t.Fatal(err)
					}
					awaitTaskSignal(t, entered)
				} else {
					_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
						task, err := copyTask(tx.state.Tasks[id], tx.limits)
						if err != nil {
							return err
						}
						task.Status = "completing"
						task.Execution.Native.State = TaskState{Status: "completing", Outcome: taskDone("already decided").Outcome}
						return tx.stage(Write{Op: "put-task", Task: &task})
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				if variant == "owned-terminal-unreturned" {
					releaseTaskGate(decide)
					awaitTaskSignal(t, terminal)
				}
				before, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				probe := &taskAdmissionContext{Context: bg, queued: make(chan struct{})}
				returned := make(chan error, 1)
				_, err = h.session.taskCommit(bg, func(*Tx) error {
					go func() { _, err := h.AbortTask(probe, id); returned <- err }()
					select {
					case <-probe.queued:
					case <-time.After(3 * time.Second):
						return errors.New("excluded orphan abort not queued")
					}
					h.scheduler.enabled.Store(false)
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-returned:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("excluded orphan abort did not return")
				}
				state, err := h.Snapshot(bg)
				if err != nil || state.Seq != before.Seq+1 || !taskAborted(state.Tasks[id]) || terminalStatus(state.Tasks[id].Status) {
					t.Fatal("direct orphan bypassed Hold/ownedjoin", err)
				}
				if variant == "held" {
					if state.Tasks[id].Execution.Native.State.Outcome.Status != "completed" || state.Tasks[id].Execution.Native.State.Outcome.Result.Value != "already decided" {
						t.Fatal("held outcome changed")
					}
				} else {
					actual := <-picked
					select {
					case <-actual.done:
						t.Fatal("owned actualhost released before gate")
					default:
					}
					if state.Tasks[childID].Status != before.Tasks[childID].Status {
						t.Fatal("Abort original admission cascaded child subset")
					}
					releaseTaskGate(decide)
					releaseTaskGate(returnHost)
					awaitTaskSignal(t, actual.done)
				}
			})
		})
	}
}

func TestTaskRecoveryPublicBlockedAbortUnregisteredOwnerWaitsForFreshOwnedAbortHostReturn(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		runEntered, releaseRun := make(chan struct{}), make(chan struct{})
		abortEntered, abortCommitted, releaseAbortReturn := make(chan struct{}), make(chan struct{}), make(chan struct{})
		runRuntime, abortRuntime := make(chan *TaskRuntime, 1), make(chan *TaskRuntime, 1)
		var aborts atomic.Int64
		var logMu sync.Mutex
		log := []string{}
		owner := taskDefinition(t, "task.direct-orphan.actual-owner", func(context.Context, TaskRecord, *TaskRuntime) error {
			return errors.New("missing owner phase invoked")
		})
		child := taskDefinition(t, "task.direct-orphan.actual-child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			logMu.Lock()
			log = append(log, "run:child")
			logMu.Unlock()
			runRuntime <- r
			close(runEntered)
			<-releaseRun
			return ctx.Err()
		})
		options := child.options
		options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			aborts.Add(1)
			logMu.Lock()
			log = append(log, "abort:child")
			logMu.Unlock()
			abortRuntime <- r
			close(abortEntered)
			if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "stopped"}}, nil
			}); err != nil {
				return err
			}
			close(abortCommitted)
			<-releaseAbortReturn
			return nil
		}
		var err error
		child, err = DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarness(t, b.store, child)
		cleanupTaskGates(t, releaseRun, releaseAbortReturn)
		ownerID := createPublicTask(t, h, owner, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		childID := createPublicTask(t, h, child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: ownerID}})
		var ownerDoc ID
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			ownerDoc, err = tx.MintID()
			if err != nil {
				return err
			}
			_, err = tx.CreateDocument(Document{ID: ownerDoc, Scope: "task", Owner: ownerID, Kind: "app.actual-owned-orphan", Version: 1, Value: JSON{"kept": true}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, runEntered)
		pickedRun := <-runRuntime
		if status, err := h.AbortTask(bg, ownerID); err != nil || status != "marked" {
			t.Fatal("owner mark", status, err)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for {
			record, _, err := h.Task(ctx, ownerID)
			if err != nil {
				t.Fatal(err)
			}
			if record.AbortRequested && record.State.Status != "terminal" {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("owner mark not visible")
			default:
				runtime.Gosched()
			}
		}
		state, err := h.Snapshot(bg)
		live := h.scheduler.ownedLive(state, ownerID)
		if err != nil || !taskAborted(state.Tasks[ownerID]) || terminalStatus(state.Tasks[ownerID].Status) || len(live) != 1 || live[0] != childID || state.Documents[ownerDoc].Retired {
			t.Fatal("owner mark bypassed running child/doc hold", err)
		}
		for {
			record, _, err := h.Task(ctx, childID)
			if err != nil {
				t.Fatal(err)
			}
			if record.AbortRequested {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("child fresh abort mark missing")
			default:
				runtime.Gosched()
			}
		}
		select {
		case <-abortEntered:
			t.Fatal("child abort started before ordinary host returned")
		default:
		}
		select {
		case <-pickedRun.done:
			t.Fatal("child run returned before release")
		default:
		}
		releaseTaskGate(releaseRun)
		awaitTaskSignal(t, pickedRun.done)
		awaitTaskSignal(t, abortEntered)
		pickedAbort := <-abortRuntime
		select {
		case <-pickedAbort.done:
			t.Fatal("child abort returned before release")
		default:
		}
		awaitTaskSignal(t, abortCommitted)
		record, found, err := h.Task(bg, ownerID)
		if err != nil || !found || record.State.Status == "terminal" || record.State.Outcome != nil {
			t.Fatal("owner orphaned before child abort host return", record, err)
		}
		childRecord, found, err := h.Task(bg, childID)
		if err != nil || !found || childRecord.State.Outcome == nil || childRecord.State.Outcome.Status != "aborted" || childRecord.State.Outcome.Reason != "stopped" {
			t.Fatal("child abort outcome missing before host return", childRecord, err)
		}
		state, err = h.Snapshot(bg)
		live = h.scheduler.ownedLive(state, ownerID)
		if err != nil || state.Documents[ownerDoc].Retired || len(live) != 1 || live[0] != childID {
			t.Fatal("owner released before child host return", err)
		}
		releaseTaskGate(releaseAbortReturn)
		awaitTaskSignal(t, pickedAbort.done)
		record = waitPublicTask(t, h, ownerID)
		if record.State.Outcome.Status != "orphaned" || record.State.Outcome.Reason != "missing_task" {
			t.Fatal("owner did not orphan after child return", record)
		}
		childRecord = waitPublicTask(t, h, childID)
		if childRecord.State.Outcome.Status != "aborted" || childRecord.State.Outcome.Reason != "stopped" {
			t.Fatal(childRecord)
		}
		state, err = h.Snapshot(bg)
		if err != nil || !state.Documents[ownerDoc].Retired || state.Documents[ownerDoc].RetiredAt != state.Seq {
			t.Fatal("owner document not retired with final orphan", state.Documents[ownerDoc], err)
		}
		logMu.Lock()
		joined := strings.Join(log, ",")
		logMu.Unlock()
		if joined != "run:child,abort:child" || aborts.Load() != 1 {
			t.Fatal("actual abort log/count", joined, aborts.Load())
		}
	})
}

func TestTaskRecoveryCloseReleasesFailedDefinitionCaches(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		old := taskDefinition(t, "task.close.failed-cache", func(context.Context, TaskRecord, *TaskRuntime) error {
			t.Error("failed migration dispatched a host")
			return nil
		})
		h := taskTestHarness(t, b.store, old)
		id := createPublicTask(t, h, old, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		migrationError := errors.New("process-local migration error")
		options := old.options
		options.Version = 2
		options.Migrate = func(any, JSON, uint64) (any, JSON, error) { return nil, nil, migrationError }
		next, err := DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = h.options.Registry.RegisterTask(next); err != nil {
			t.Fatal(err)
		}
		reserved, err := h.scheduler.reservePass(false)
		discardUnstartedTaskReservations(t, h, reserved)
		if err != nil || len(reserved) != 0 {
			t.Fatal("failed migration reserved", err)
		}
		h.session.taskBookkeeping(func() {
			if h.scheduler.failed[id] != next || h.scheduler.failures[id] != migrationError {
				t.Error("failure cache missing before Close")
			}
		})
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		h.session.taskBookkeeping(func() {
			if len(h.scheduler.failed) != 0 || len(h.scheduler.failures) != 0 || len(h.scheduler.retryAfter) != 0 || h.scheduler.reconcileRetry != nil || len(h.scheduler.reports) != 0 {
				t.Error("closed Harness retained failure state")
			}
		})
		store := reopenStoreAfterHarnessClose(t, b.store)
		second := taskTestHarness(t, store, old)
		record, ok, err := second.Task(bg, id)
		if err != nil || !ok || record.Version != 1 || record.State.Status != "pending" {
			t.Fatal("Close changed durable failed-migration state", record, err)
		}
	})
}

func TestTaskRecoveryActiveDefinitionLossOrphansOnlyAfterRunReturn(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, cancelled, returnRun := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var captured *TaskRuntime
		var runs, aborts atomic.Int64
		definition := taskDefinition(t, "task.active.vanishing", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			runs.Add(1)
			captured = r
			close(entered)
			<-ctx.Done()
			close(cancelled)
			<-returnRun
			return ctx.Err()
		})
		options := definition.options
		options.Abort = func(context.Context, TaskRecord, *TaskRuntime) error {
			aborts.Add(1)
			return errors.New("vanished abort dispatched")
		}
		var err error
		definition, err = DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarness(t, b.store)
		cleanupTaskGates(t, returnRun)
		dispose, err := h.options.Registry.RegisterTask(definition)
		if err != nil {
			t.Fatal(err)
		}
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		dispose()
		type abortResult struct {
			status string
			err    error
		}
		result := make(chan abortResult, 1)
		go func() { status, err := h.AbortTask(bg, id); result <- abortResult{status, err} }()
		awaitTaskSignal(t, cancelled)
		if record, ok, err := h.Task(bg, id); err != nil || !ok || !record.AbortRequested || record.State.Status == "terminal" {
			t.Fatal("vanished owner orphaned before actual run return", record, err)
		}
		select {
		case got := <-result:
			t.Fatal("public abort skipped actual run", got)
		default:
		}
		releaseTaskGate(returnRun)
		awaitTaskSignal(t, captured.done)
		select {
		case got := <-result:
			if got.status != "marked" || got.err != nil {
				t.Fatal(got)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("public abort did not join old run")
		}
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "orphaned" || record.State.Outcome.Reason != "missing_task" || runs.Load() != 1 || aborts.Load() != 0 {
			t.Fatal("active definition loss", record, runs.Load(), aborts.Load())
		}
	})
}
