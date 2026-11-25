package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const fullGroupScriptV160 = `const a=receipt("a"), b=receipt("b"), c=receipt("c"); return [await a, await b, await c]`

var fullGroupValueV160 = []any{"a", "b", "c"}

// Only observe the host boundaries of the real portable execution. The
// first-drain wrapper is the existing readiness fixture: it observes b and c
// enter the completion queue in order before releasing a's idle frame.
type fullGroupCutoffSandboxV160 struct {
	*RemoteCodemodeSandbox // Keep the worker's optional capability declarations.
	starter                CodemodeSandbox
	store                  *SQLiteStorage
	worker                 *journalWorkerRemoteV160
	dir                    string
	cut                    string
	proof                  fullGroupCutoffReceiptV160
}

type fullGroupJournalImageV160 struct {
	ID      ID
	Version uint64
	Value   json.RawMessage
}

type fullGroupCutoffReceiptV160 struct {
	GroupSeq    uint64
	BoundarySeq uint64
	Journal     fullGroupJournalImageV160
	Steps       []string
	Calls       []json.RawMessage
	Deliveries  []json.RawMessage
}

func (s *fullGroupCutoffSandboxV160) Start(ctx context.Context, code string, start CodemodeStart) (CodemodeExecution, error) {
	if code != fullGroupScriptV160 || start.JournalVersion != codemodeGroupedJournalVersion || !start.ParallelCalls || !start.DrainAcknowledgements {
		return nil, fmt.Errorf("changed grouped execution identity: %q, policy %d/%t/%t", code, start.JournalVersion, start.ParallelCalls, start.DrainAcknowledgements)
	}
	if s.cut == "" {
		// Recovery itself may advance the task. From this new worker's start
		// until the final result, replay must not publish another journal write.
		state, err := s.store.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		s.proof.GroupSeq = state.Seq
		if err := s.checkBoundary(state); err != nil {
			return nil, err
		}
	}
	vm, err := s.starter.Start(ctx, code, start)
	if err != nil {
		return nil, err
	}
	return &fullGroupCutoffExecutionV160{CodemodeExecution: vm, ack: vm.(CodemodeDrainExecution), probe: s}, nil
}

type fullGroupCutoffExecutionV160 struct {
	CodemodeExecution
	ack   CodemodeDrainExecution
	probe *fullGroupCutoffSandboxV160
}

func (e *fullGroupCutoffExecutionV160) Next(ctx context.Context) (CodemodeEvent, error) {
	event, err := e.CodemodeExecution.Next(ctx)
	if err != nil {
		return event, err
	}
	p := &e.probe.proof
	switch {
	case event.Call != nil:
		data, err := json.Marshal(event.Call)
		if err != nil {
			return CodemodeEvent{}, err
		}
		p.Calls = append(p.Calls, data)
		p.Steps = append(p.Steps, fmt.Sprintf("call:%d", event.Call.ID))
	case event.Drain != nil:
		p.Steps = append(p.Steps, fullGroupDrainFrameV160("drain", *event.Drain))
	case event.Result != nil:
		if !event.Result.HasValue || event.Result.Error != "" || !equalJSONValue(event.Result.Value, fullGroupValueV160) {
			return CodemodeEvent{}, fmt.Errorf("real guest result = %+v; want [a,b,c]", event.Result)
		}
		if e.probe.cut == "" {
			state, err := e.probe.store.Snapshot(ctx)
			if err != nil {
				return CodemodeEvent{}, err
			}
			if err := e.probe.checkBoundary(state); err != nil {
				return CodemodeEvent{}, err
			}
		}
		p.Steps = append(p.Steps, "result:[a,b,c]")
	}
	return event, nil
}

func fullGroupDrainFrameV160(action string, drain CodemodeDrain) string {
	return fmt.Sprintf("%s:%d/%d/%t", action, drain.DeliveryID, drain.Issued, drain.Final)
}

func (e *fullGroupCutoffExecutionV160) Deliver(ctx context.Context, delivery CodemodeDelivery) error {
	if e.probe.cut == "before-c" && delivery.Seq == 3 {
		// b has entered the guest and its idle drain has been acknowledged.
		// Do not call Deliver for c, even though its record is durable.
		return e.probe.exitAtBoundary(ctx, delivery.Seq-1)
	}
	if err := e.CodemodeExecution.Deliver(ctx, delivery); err != nil {
		return err
	}
	data, err := json.Marshal(delivery)
	if err != nil {
		return err
	}
	p := &e.probe.proof
	p.Deliveries = append(p.Deliveries, data)
	p.Steps = append(p.Steps, fmt.Sprintf("deliver:%d", delivery.Seq))
	return nil
}

func (e *fullGroupCutoffExecutionV160) AcknowledgeDrain(ctx context.Context, drain CodemodeDrain) error {
	s := e.probe
	if s.cut == "before-terminal-ack" && drain.Final {
		// The guest reports c's final position, but it has not been
		// acknowledged and neither a result nor a terminal position is saved.
		return s.exitAtBoundary(ctx, drain.DeliveryID)
	}
	if s.cut != "" && drain.DeliveryID == 1 {
		state, err := s.store.Snapshot(ctx)
		if err != nil {
			return err
		}
		// The existing fixture also proves this is exactly one publication
		// since a's idle arrived. Take a detached copy before forwarding it.
		if s.proof.Journal.ID != 0 {
			return errors.New("first grouped drain repeated")
		}
		s.proof.GroupSeq = state.Seq
		s.proof.Journal, err = fullGroupJournalV160(state)
		if err != nil {
			return err
		}
	}
	if err := e.ack.AcknowledgeDrain(ctx, drain); err != nil {
		return err
	}
	s.proof.Steps = append(s.proof.Steps, fullGroupDrainFrameV160("ack", drain))
	return nil
}

func (s *fullGroupCutoffSandboxV160) checkBoundary(state Snapshot) error {
	if state.Seq != s.proof.GroupSeq {
		return fmt.Errorf("group publication seq=%d, boundary seq=%d; want no intervening write", s.proof.GroupSeq, state.Seq)
	}
	if err := checkGroupedSnapshotV160(state, false); err != nil {
		return err
	}
	image, err := fullGroupJournalV160(state)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(image, s.proof.Journal) {
		return errors.New("boundary changed the detached grouped journal")
	}
	return nil
}

func (s *fullGroupCutoffSandboxV160) exitAtBoundary(ctx context.Context, delivered int) error {
	state, err := s.store.Snapshot(ctx)
	if err != nil {
		return err
	}
	if err := s.checkBoundary(state); err != nil {
		return err
	}
	if s.worker.starts.Load() != 1 || s.worker.deliveries.Load() != int32(delivered) {
		return fmt.Errorf("worker starts=%d, deliveries=%d; want 1/%d", s.worker.starts.Load(), s.worker.deliveries.Load(), delivered)
	}
	s.proof.BoundarySeq = state.Seq
	data, err := json.Marshal(s.proof)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.dir, "boundary.json"), data, 0600); err != nil {
		return err
	}
	os.Exit(73) // No terminal ack, settlement, graceful storage close, or defer.
	return nil
}

func fullGroupJournalV160(state Snapshot) (fullGroupJournalImageV160, error) {
	var image fullGroupJournalImageV160
	for _, doc := range state.Documents {
		if doc.Kind != codemodeJournalKind {
			continue
		}
		if image.ID != 0 {
			return image, errors.New("more than one grouped journal")
		}
		data, err := json.Marshal(doc.Value)
		if err != nil {
			return image, err
		}
		image = fullGroupJournalImageV160{ID: doc.ID, Version: doc.Version, Value: data}
	}
	if image.ID == 0 {
		return image, errors.New("grouped journal not found")
	}
	return image, nil
}

func checkFullGroupFramesV160(t *testing.T, proof fullGroupCutoffReceiptV160, cut string) {
	t.Helper()
	want := []string{"call:1", "call:2", "call:3", "drain:0/3/false", "ack:0/3/false",
		"deliver:1", "drain:1/3/false", "ack:1/3/false", "deliver:2", "drain:2/3/false", "ack:2/3/false"}
	delivered := 2
	if cut != "before-c" {
		want = append(want, "deliver:3", "drain:3/3/true")
		delivered = 3
	}
	if cut == "" {
		want = append(want, "ack:3/3/true", "result:[a,b,c]")
	}
	if !reflect.DeepEqual(proof.Steps, want) || len(proof.Calls) != 3 || len(proof.Deliveries) != delivered {
		t.Fatalf("cut=%q frames=%v calls=%d deliveries=%d; want %v/3/%d", cut, proof.Steps, len(proof.Calls), len(proof.Deliveries), want, delivered)
	}
	var journal codemodeJournalState
	if err := json.Unmarshal(proof.Journal.Value, &journal); err != nil {
		t.Fatal(err)
	}
	if journal.Version != codemodeGroupedJournalVersion || journal.FirstIssued == nil || *journal.FirstIssued != 3 || !reflect.DeepEqual(journal.BatchEnds, []int{1, 3}) ||
		len(journal.Issued) != 3 || len(journal.Deliveries) != 3 {
		t.Fatalf("invalid saved batch shape: %+v", journal)
	}
	for i, call := range journal.Issued {
		if call.ID != i+1 || call.Name != "receipt" || !call.Global {
			t.Fatalf("issued call %d = %+v", i+1, call)
		}
		checkFullGroupFrameV160(t, "issued", i+1, proof.Calls[i], call)
	}
	for i, entry := range journal.Deliveries {
		if entry.Call.ID != i+1 || entry.Seq != i+1 || !entry.HasValue || entry.Value != fullGroupValueV160[i] || entry.Error != "" ||
			i == 0 && (entry.IssuedAfter == nil || *entry.IssuedAfter != 3) || i != 0 && entry.IssuedAfter != nil {
			t.Fatalf("bad stored result %d: %+v", i+1, entry)
		}
		if i == 0 && cut != "" {
			entry.IssuedAfter = nil // a was sent before its count was saved.
		}
		if i < delivered {
			checkFullGroupFrameV160(t, "delivered", i+1, proof.Deliveries[i], entry)
		}
	}
}

func checkFullGroupFrameV160(t *testing.T, kind string, index int, got json.RawMessage, want any) {
	t.Helper()
	data, err := json.Marshal(want)
	if err != nil || string(got) != string(data) {
		t.Fatalf("%s frame %d = %s, want %s (err=%v)", kind, index, got, data, err)
	}
}

func fullGroupHarnessV160(t *testing.T, dir, cut string, store *SQLiteStorage, saved fullGroupJournalImageV160) (*Harness, *TaskDefinition, *fullGroupCutoffSandboxV160) {
	t.Helper()
	remote, worker := newJournalWorkerSandboxV160(t)
	remote.legacyJournal = cut == "" // Only the first run opts in; recovery must use the stored policy.
	sandbox := &fullGroupCutoffSandboxV160{RemoteCodemodeSandbox: remote, starter: remote, store: store, worker: worker, dir: dir, cut: cut}
	sandbox.proof.Journal = saved
	var ready chan codemodeCompletedCall
	var gates [2]chan struct{}
	if cut != "" {
		ready = make(chan codemodeCompletedCall, 4)
		gates = [2]chan struct{}{make(chan struct{}), make(chan struct{})}
		sandbox.starter = groupedSandboxV160{RemoteCodemodeSandbox: remote, ready: ready, gates: gates, store: store, sent: worker}
	}
	registry := NewRegistry()
	def := taskDefinition(t, "batch-full-cutoff-v160", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		result, err := r.runCodemodeWithCompletionQueue(ctx, "run", fullGroupScriptV160, sandbox, nil, nil, nil, nil, ready)
		if err != nil {
			return err
		}
		if cut != "" || !result.HasValue || result.Error != "" || !equalJSONValue(result.Value, fullGroupValueV160) {
			return fmt.Errorf("unexpected full group settlement at cut=%q: %+v", cut, result)
		}
		seal, err := store.Snapshot(ctx)
		if err != nil {
			return err
		}
		if seal.Seq != sandbox.proof.GroupSeq+1 {
			return fmt.Errorf("replay seq %d -> %d; want one result seal before task settlement", sandbox.proof.GroupSeq, seal.Seq)
		}
		sandbox.proof.BoundarySeq = seal.Seq
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	if _, err := registry.RegisterTask(def); err != nil {
		t.Fatal(err)
	}
	globals := []CodemodeGlobal{{Name: "receipt", Version: 1, Execute: func(ctx context.Context, arg any) (any, error) {
		value, ok := arg.(string)
		if !ok || len(value) != 1 || value[0] < 'a' || value[0] > 'c' {
			return nil, fmt.Errorf("unexpected callback argument %v", arg)
		}
		if cut != "" && value != "a" {
			select {
			case <-gates[value[0]-'b']:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		f, err := os.OpenFile(filepath.Join(dir, "effects"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		_, err = f.WriteString(value)
		return value, errors.Join(err, f.Close())
	}}}
	return openHarness(t, store, Options{Registry: registry, CodemodeGlobals: globals}), def, sandbox
}

func TestV160CodemodeFullGroupCutoffChild(t *testing.T) {
	dir := os.Getenv("GOAI_CODEMODE_FULL_GROUP_DIR")
	if dir == "" {
		return
	}
	cut := os.Getenv("GOAI_CODEMODE_FULL_GROUP_CUT")
	if cut != "before-c" && cut != "before-terminal-ack" {
		t.Fatalf("unknown group cutoff %q", cut)
	}
	store, err := openSQLiteTestDirectory(filepath.Join(dir, "journal"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h, def, _ := fullGroupHarnessV160(t, dir, cut, store, fullGroupJournalImageV160{})
	id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	row, err := h.WaitForTask(ctx, id)
	if row.State.Outcome != nil && row.State.Outcome.Error != nil {
		t.Fatalf("task fault before %s: %s", cut, row.State.Outcome.Error.Message)
	}
	t.Fatalf("task did not exit at %s: outcome=%+v err=%v", cut, row.State.Outcome, err)
}

func TestV160CodemodeFullGroupCutoffRecovery(t *testing.T) {
	officialWorkerPackageRootV160(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, cut := range []string{"before-c", "before-terminal-ack"} {
		t.Run(cut, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestV160CodemodeFullGroupCutoffChild$")
			cmd.Env = append(os.Environ(), "GOAI_CODEMODE_FULL_GROUP_DIR="+dir, "GOAI_CODEMODE_FULL_GROUP_CUT="+cut)
			var exited *exec.ExitError
			if output, err := cmd.CombinedOutput(); !errors.As(err, &exited) || exited.ExitCode() != 73 {
				t.Fatalf("child did not exit at %s: %v\n%s", cut, err, output)
			}
			data, err := os.ReadFile(filepath.Join(dir, "boundary.json"))
			if err != nil {
				t.Fatal(err)
			}
			var receipt fullGroupCutoffReceiptV160
			if err := json.Unmarshal(data, &receipt); err != nil {
				t.Fatal(err)
			}
			checkFullGroupFramesV160(t, receipt, cut)
			store, err := openSQLiteTestDirectory(filepath.Join(dir, "journal"), SQLiteOptions{})
			if err != nil {
				t.Fatal(err)
			}
			before, err := store.Snapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := (&fullGroupCutoffSandboxV160{proof: receipt}).checkBoundary(before); err != nil || before.Seq != receipt.BoundarySeq {
				t.Fatalf("physical reopen differs from seq %d: seq=%d err=%v", receipt.BoundarySeq, before.Seq, err)
			}
			var id ID
			for task, row := range before.Tasks {
				if row.Kind == "batch-full-cutoff-v160" {
					if id != 0 || row.Execution == nil || row.Execution.Native == nil || row.Execution.Native.State.Outcome != nil {
						t.Fatal("reopened SQLite must contain one unsettled original task")
					}
					id = task
				}
			}
			checkFullGroupEffectsV160(t, dir)
			h, _, replay := fullGroupHarnessV160(t, dir, "", store, receipt.Journal)
			if id == 0 {
				t.Fatal("original task missing from reopened SQLite")
			}
			if err := h.Resume(ctx); err != nil {
				t.Fatal(err)
			}
			row, err := h.WaitForTask(ctx, id)
			if err != nil || row.State.Outcome == nil || row.State.Outcome.Status != "completed" {
				if row.State.Outcome != nil && row.State.Outcome.Error != nil {
					t.Fatalf("replay faulted: %s", row.State.Outcome.Error.Message)
				}
				t.Fatalf("replay not completed: outcome=%+v err=%v", row.State.Outcome, err)
			}
			checkFullGroupFramesV160(t, replay.proof, "")
			if replay.worker.starts.Load() != 1 || replay.worker.deliveries.Load() != 3 {
				t.Fatalf("replay starts=%d deliveries=%d, want 1/3", replay.worker.starts.Load(), replay.worker.deliveries.Load())
			}
			checkFullGroupEffectsV160(t, dir)
			after, err := store.Snapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			detached, err := fullGroupJournalV160(after)
			if err != nil || !reflect.DeepEqual(detached, receipt.Journal) {
				t.Fatalf("replay rewrote the grouped journal: %v", err)
			}
			if after.Seq != replay.proof.BoundarySeq+1 {
				t.Fatalf("result seal seq %d, settled seq %d; want one task settlement write", replay.proof.BoundarySeq, after.Seq)
			}
			var results int
			for _, doc := range after.Documents {
				if doc.Kind != codemodeRunKind {
					continue
				}
				results++
				var run codemodeRunState
				if err := fromObject(doc.Value, &run, DefaultLimits()); err != nil {
					t.Fatal(err)
				}
				if run.Result == nil || !run.Result.HasValue || run.Result.Error != "" || !equalJSONValue(run.Result.Value, fullGroupValueV160) ||
					!sameCodemodeTerminal(run.Terminal, codemodeTerminalPosition(3, 3, 3)) {
					t.Fatalf("replay result=%+v terminal=%+v; want [a,b,c] and 3/3/3", run.Result, run.Terminal)
				}
			}
			if results != 1 {
				t.Fatalf("replay has %d run receipts; want one", results)
			}
			t.Logf("cut seq=%d; replay start/sealed/settled seq=%d/%d/%d; %v", receipt.BoundarySeq, replay.proof.GroupSeq, replay.proof.BoundarySeq, after.Seq, receipt.Steps)
		})
	}
}

func checkFullGroupEffectsV160(t *testing.T, dir string) {
	t.Helper()
	if effects, err := os.ReadFile(filepath.Join(dir, "effects")); err != nil || string(effects) != "abc" {
		t.Fatalf("effects = %q err=%v; want abc exactly once", effects, err)
	}
}
