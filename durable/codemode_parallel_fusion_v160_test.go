package durable

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const fusedScriptV160 = `const a = result("a"), b = result("b"); return [await a, await b]`

// Gate the actual worker's first non-final drain on b being in the Go
// scheduler's queue. Merely reaching or returning from b's callback would not
// prove this ordering. While Next holds the drain, the scheduler cannot read b.
type fusedDrainSandboxV160 struct {
	*RemoteCodemodeSandbox
	ready      chan codemodeCompletedCall
	b          chan struct{}
	store      *SQLiteStorage
	sent       *journalWorkerRemoteV160
	boundedAck *bool
}

func (s fusedDrainSandboxV160) Start(ctx context.Context, code string, start CodemodeStart) (CodemodeExecution, error) {
	vm, err := s.RemoteCodemodeSandbox.Start(ctx, code, start)
	if err != nil {
		return nil, err
	}
	return &fusedDrainExecutionV160{CodemodeExecution: vm, ack: vm.(CodemodeDrainExecution), sandbox: s}, nil
}

type fusedDrainExecutionV160 struct {
	CodemodeExecution
	ack     CodemodeDrainExecution
	sandbox fusedDrainSandboxV160
	before  uint64
}

func (e *fusedDrainExecutionV160) Next(ctx context.Context) (CodemodeEvent, error) {
	event, err := e.CodemodeExecution.Next(ctx)
	if err != nil || event.Drain == nil || event.Drain.DeliveryID != 1 {
		return event, err
	}
	if event.Drain.Final || event.Drain.Issued != 2 {
		return CodemodeEvent{}, fmt.Errorf("unexpected first result drain: %+v", *event.Drain)
	}
	close(e.sandbox.b)
	select {
	case ready := <-e.sandbox.ready:
		if ready.err != nil || ready.delivery.Call.ID != 2 || ready.delivery.Value != e.sandbox.bValue() {
			return CodemodeEvent{}, fmt.Errorf("unexpected ready outcome for call %d (value type %T): %v", ready.delivery.Call.ID, ready.delivery.Value, ready.err)
		}
		e.sandbox.ready <- ready
		s, err := e.sandbox.store.Snapshot(ctx)
		e.before = s.Seq
		return event, err
	case <-ctx.Done():
		return CodemodeEvent{}, ctx.Err()
	}
}

func (e *fusedDrainExecutionV160) AcknowledgeDrain(ctx context.Context, drain CodemodeDrain) error {
	if drain.DeliveryID != 1 {
		return e.ack.AcknowledgeDrain(ctx, drain)
	}
	s, err := e.sandbox.store.Snapshot(ctx)
	if err == nil && s.Seq != e.before+1 {
		err = fmt.Errorf("storage advanced from %d to %d; want one publication", e.before, s.Seq)
	}
	if e.sandbox.boundedAck != nil {
		if err != nil {
			return err
		}
		for _, doc := range s.Documents {
			if doc.Kind != codemodeJournalKind {
				continue
			}
			var journal codemodeJournalState
			if err := fromObject(doc.Value, &journal, DefaultLimits()); err != nil {
				return err
			}
			if len(journal.Deliveries) == 1 && journal.Deliveries[0].IssuedAfter != nil && *journal.Deliveries[0].IssuedAfter == 2 {
				if err := e.ack.AcknowledgeDrain(ctx, drain); err != nil {
					return err
				}
				*e.sandbox.boundedAck = true
				return nil
			}
		}
		return errors.New("oversize outcome blocked publication of the count")
	}
	if err == nil {
		err = checkFusedDrainSnapshotV160(s)
	}
	if err != nil || e.sandbox.sent.deliveries.Load() != 1 {
		fmt.Fprintf(os.Stderr, "before transport ack: %v; sent=%d\n", err, e.sandbox.sent.deliveries.Load())
		os.Exit(74)
	}
	// SQLite has settled and the Session has returned from Commit. Exit
	// before the result transport ack and before the second delivery.
	os.Exit(73)
	return nil
}

func (s fusedDrainSandboxV160) bValue() string {
	if s.boundedAck != nil {
		// The outcome by itself fits, but the cumulative journal doesn't.
		return strings.Repeat("b", DefaultLimits().MaxDocumentBytes-64)
	}
	return "b"
}

func checkFusedDrainSnapshotV160(snapshot Snapshot) error {
	journals, runs := 0, 0
	for _, doc := range snapshot.Documents {
		switch doc.Kind {
		case codemodeJournalKind:
			journals++
			var journal codemodeJournalState
			if err := fromObject(doc.Value, &journal, DefaultLimits()); err != nil {
				return err
			}
			if err := validateCodemodeWatermarks(CodemodeStart{DrainAcknowledgements: true}, journal, false); err != nil {
				return err
			}
			if len(journal.Issued) != 2 || len(journal.Deliveries) != 2 {
				return fmt.Errorf("issued=%d delivered=%d; want 2 and 2", len(journal.Issued), len(journal.Deliveries))
			}
			a, b := journal.Deliveries[0], journal.Deliveries[1]
			if a.Seq != 1 || a.Call.ID != 1 || a.Value != "a" || a.IssuedAfter == nil || *a.IssuedAfter != 2 || b.Seq != 2 || b.Call.ID != 2 || b.Value != "b" || b.IssuedAfter != nil {
				return fmt.Errorf("invalid fused prefix: %+v", journal.Deliveries)
			}
			journal.Deliveries[0].IssuedAfter = nil // detached adversarial copy
			if err := validateCodemodeWatermarks(CodemodeStart{DrainAcknowledgements: true}, journal, false); err == nil || err.Error() != "durable: rejected: codemode delivery watermark missing" {
				return fmt.Errorf("nonfinal nil watermark must remain invalid: %v", err)
			}
		case codemodeRunKind:
			runs++
			var run codemodeRunState
			if err := fromObject(doc.Value, &run, DefaultLimits()); err != nil {
				return err
			}
			if run.Result != nil {
				return errors.New("published a terminal result before cutoff")
			}
		}
	}
	if journals != 1 || runs != 1 {
		return fmt.Errorf("journals=%d runs=%d; want one each", journals, runs)
	}
	return nil
}

func fusedDrainHarnessV160(t *testing.T, dir string, store *SQLiteStorage, gateDrain, bounded bool) (*Harness, *TaskDefinition, *journalWorkerRemoteV160) {
	t.Helper()
	remote, worker := newJournalWorkerSandboxV160(t)
	// The crash child records legacy mode; the recovered host would choose
	// grouping for a new run, but must replay the original saved mode.
	remote.groupJournal = !gateDrain
	var sandbox CodemodeSandbox = remote
	var ready chan codemodeCompletedCall
	var b chan struct{}
	var gated fusedDrainSandboxV160
	boundedAck := false
	if gateDrain {
		ready, b = make(chan codemodeCompletedCall, 2), make(chan struct{})
		gated = fusedDrainSandboxV160{RemoteCodemodeSandbox: remote, ready: ready, b: b, store: store, sent: worker}
		if bounded {
			gated.boundedAck = &boundedAck
		}
		sandbox = gated
	}
	registry := NewRegistry()
	def := taskDefinition(t, "fused-drain-process-v160", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		result, err := r.runCodemodeWithCompletionQueue(ctx, "run", fusedScriptV160, sandbox, nil, nil, nil, nil, ready)
		if bounded {
			if err == nil || err.Error() != "durable: rejected: encoded JSON byte limit" || !boundedAck {
				return fmt.Errorf("expected size rejection after durable count/ack; ack=%v err=%v", boundedAck, err)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		}
		if err != nil {
			return err
		}
		if result.Error != "" || !result.HasValue || !reflect.DeepEqual(result.Value, []any{"a", "b"}) {
			return fmt.Errorf("recovered result = %+v", result)
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	if _, err := registry.RegisterTask(def); err != nil {
		t.Fatal(err)
	}
	globals := []CodemodeGlobal{{Name: "result", Version: 1, Execute: func(ctx context.Context, args any) (any, error) {
		value, ok := args.(string)
		if !ok || value != "a" && value != "b" {
			return nil, fmt.Errorf("unexpected result callback with %v", args)
		}
		if gateDrain && value == "b" {
			select {
			case <-b:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		file, err := os.OpenFile(filepath.Join(dir, "effects"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		_, err = file.WriteString(value + "\n")
		if value == "b" {
			value = gated.bValue()
		}
		return value, errors.Join(err, file.Close())
	}}}
	return openHarness(t, store, Options{Registry: registry, CodemodeGlobals: globals}), def, worker
}

func TestV160CodemodeFusedDrainCrashChild(t *testing.T) {
	dir := os.Getenv("GOAI_CODEMODE_FUSED_CRASH_DIR")
	if dir == "" {
		return
	}
	store, err := openSQLiteTestDirectory(filepath.Join(dir, "journal"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h, def, _ := fusedDrainHarnessV160(t, dir, store, true, false)
	id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	if err := h.Resume(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	row, err := h.WaitForTask(ctx, id)
	t.Fatalf("task did not exit at fused commit: %+v; %v", row, err)
}

// Supplemental to L913: a real process cutoff in one fused publication, not
// source parity for its separate <10 performance assertion.
func TestV160CodemodeFusedDrainProcessRecovery(t *testing.T) {
	officialWorkerPackageRootV160(t)
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestV160CodemodeFusedDrainCrashChild$")
	cmd.Env = append(os.Environ(), "GOAI_CODEMODE_FUSED_CRASH_DIR="+dir)
	output, err := cmd.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 73 {
		t.Fatalf("child did not exit after fused adoption: %v\n%s", err, output)
	}
	store, err := openSQLiteTestDirectory(filepath.Join(dir, "journal"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkFusedDrainSnapshotV160(snapshot); err != nil {
		t.Fatal(err)
	}
	var id ID
	for task, row := range snapshot.Tasks {
		if row.Kind == "fused-drain-process-v160" {
			id = task
		}
	}
	if id == 0 {
		t.Fatal("missing original task")
	}
	h, _, worker := fusedDrainHarnessV160(t, dir, store, false, false)
	policy := make(chan int, 1)
	worker.observeStart = func(start CodemodeStart) {
		select {
		case policy <- start.JournalVersion:
		default:
		}
	}
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	if row, err := h.WaitForTask(ctx, id); err != nil || row.State.Outcome == nil || row.State.Outcome.Status != "completed" {
		t.Fatalf("recovered task: %+v; %v", row, err)
	}
	if effects, err := os.ReadFile(filepath.Join(dir, "effects")); err != nil || string(effects) != "a\nb\n" {
		t.Fatalf("effects=%q err=%v; want only original a then b", effects, err)
	}
	if worker.starts.Load() != 1 || worker.deliveries.Load() != 2 {
		t.Fatalf("replayed starts=%d deliveries=%d; want 1 and 2", worker.starts.Load(), worker.deliveries.Load())
	}
	select {
	case version := <-policy:
		if version != 0 {
			t.Fatalf("legacy replay started with journal policy %d", version)
		}
	default:
		t.Fatal("missing legacy replay start receipt")
	}
	finished, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range finished.Documents {
		if doc.Owner != id {
			continue
		}
		if doc.Kind == codemodeJournalKind && (doc.Value["journalVersion"] != nil || doc.Value["batchEnds"] != nil) || doc.Kind == codemodeRunKind && doc.Value["terminal"] != nil {
			t.Fatal("legacy journal gained grouped state after reopen", doc.Value)
		}
	}
}

// A local bound refusal can fall back before staging, unlike a failure after
// submitting to storage. The guest observes the count ack but not the oversize
// delivery, which still raises the original commit diagnostic.
func TestV160CodemodeFusedDrainDocumentLimit(t *testing.T) {
	dir := t.TempDir()
	store, err := openSQLiteTestDirectory(filepath.Join(dir, "journal"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h, def, worker := fusedDrainHarnessV160(t, dir, store, true, true)
	id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	if row, err := h.WaitForTask(ctx, id); err != nil || row.State.Outcome == nil || row.State.Outcome.Status != "completed" {
		if row.State.Outcome != nil && row.State.Outcome.Error != nil {
			t.Fatal(row.State.Outcome.Error.Message)
		}
		t.Fatalf("bound task: %+v; %v", row, err)
	}
	if worker.deliveries.Load() != 1 {
		t.Fatalf("deliveries=%d; want only a", worker.deliveries.Load())
	}
}

func TestV160CodemodeFusedDrainCoordinator(t *testing.T) {
	for _, mode := range []string{"empty", "fused", "no-room", "completion-error", "commit-error"} {
		t.Run(mode, func(t *testing.T) {
			calls := []CodemodeCall{{ID: 1, Name: "a", Global: true}, {ID: 2, Name: "b", Global: true}}
			journal := codemodeJournalState{FirstIssued: watermark120(2), Issued: calls, Deliveries: []CodemodeDelivery{{Seq: 1, Now: 10, Call: calls[0]}}}
			queue := make(chan codemodeCompletedCall, 1)
			completion := codemodeCompletedCall{delivery: CodemodeDelivery{Call: calls[1], Now: 8}}
			failure := errors.New("unconfirmed commit")
			if mode == "completion-error" {
				completion.err = errors.New("host stopped")
			}
			if mode != "empty" {
				queue <- completion
			}
			vm := &drainCommitVM120{}
			commits := 0
			held, err := settleCodemodeDrain(t.Context(), vm, &journal, CodemodeDrain{DeliveryID: 1, Issued: 2}, 2, 1, queue,
				func(_ context.Context, count codemodeJournalState, fused *codemodeJournalState) (bool, error) {
					commits++
					if vm.acknowledgements != 0 || journal.Deliveries[0].IssuedAfter != nil {
						t.Fatal("unconfirmed call count adopted or sent to VM")
					}
					if len(count.Deliveries) != 1 || count.Deliveries[0].IssuedAfter == nil || *count.Deliveries[0].IssuedAfter != 2 {
						t.Fatalf("expected a prepared count: %+v", count.Deliveries)
					}
					if mode == "empty" || mode == "completion-error" {
						if fused != nil {
							t.Fatalf("unexpected fused result: %+v", fused.Deliveries)
						}
					} else if fused == nil || len(fused.Deliveries) != 2 || fused.Deliveries[1].Seq != 2 || fused.Deliveries[1].Now != 10 || fused.Deliveries[1].Call.ID != 2 || fused.Deliveries[1].IssuedAfter != nil {
						t.Fatalf("expected detached, time-clamped candidate: %+v", fused)
					}
					if mode == "commit-error" {
						return false, failure
					}
					return mode == "fused", nil
				})
			if commits != 1 || len(queue) != 0 {
				t.Fatalf("commits=%d pending=%d; want one attempt and no queued copy", commits, len(queue))
			}
			if mode == "commit-error" {
				if !errors.Is(err, failure) || vm.acknowledgements != 0 || len(journal.Deliveries) != 1 || journal.Deliveries[0].IssuedAfter != nil {
					t.Fatalf("failed commit adopted or released: %+v, acknowledgements=%d, %v", journal.Deliveries, vm.acknowledgements, err)
				}
				return
			}
			if err != nil || vm.acknowledgements != 1 || journal.Deliveries[0].IssuedAfter == nil || (len(journal.Deliveries) == 2) != (mode == "fused") {
				t.Fatalf("incorrect adoption: %+v, acknowledgements=%d, %v", journal.Deliveries, vm.acknowledgements, err)
			}
			if mode == "no-room" || mode == "completion-error" {
				if held == nil || !reflect.DeepEqual(*held, completion) {
					t.Fatalf("completion lost or changed: %+v", held)
				}
			} else if held != nil {
				t.Fatalf("unexpected retained completion: %+v", held)
			}
		})
	}
}
