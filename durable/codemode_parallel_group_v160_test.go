package durable

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const groupedScriptV160 = `const a=receipt("a"), b=receipt("b"), c=receipt("c"); await a; return await b`

// While Next holds a's real idle frame, the coordinator cannot consume another
// completion. Let b and c run in order, actually observe them in the private
// queue, then requeue both before allowing a's idle to reach the coordinator.
type groupedSandboxV160 struct {
	*RemoteCodemodeSandbox
	ready chan codemodeCompletedCall
	gates [2]chan struct{}
	store *SQLiteStorage
	sent  *journalWorkerRemoteV160
}

func (s groupedSandboxV160) Start(ctx context.Context, code string, start CodemodeStart) (CodemodeExecution, error) {
	vm, err := s.RemoteCodemodeSandbox.Start(ctx, code, start)
	if err != nil {
		return nil, err
	}
	return &groupedExecutionV160{CodemodeExecution: vm, ack: vm.(CodemodeDrainExecution), harness: s}, nil
}

type groupedExecutionV160 struct {
	CodemodeExecution
	ack     CodemodeDrainExecution
	harness groupedSandboxV160
	before  uint64
}

func (e *groupedExecutionV160) Next(ctx context.Context) (CodemodeEvent, error) {
	event, err := e.CodemodeExecution.Next(ctx)
	if err != nil || event.Drain == nil || event.Drain.DeliveryID != 1 {
		return event, err
	}
	if event.Drain.Final || event.Drain.Issued != 3 {
		return CodemodeEvent{}, fmt.Errorf("unexpected first outcome drain: %+v", *event.Drain)
	}
	var pending [2]codemodeCompletedCall
	for i, gate := range e.harness.gates {
		close(gate)
		select {
		case result := <-e.harness.ready:
			if result.err != nil || result.delivery.Call.ID != i+2 || result.delivery.Value != string(rune('b'+i)) {
				return CodemodeEvent{}, fmt.Errorf("unexpected ready outcome: %+v; %v", result.delivery, result.err)
			}
			pending[i] = result
		case <-ctx.Done():
			return CodemodeEvent{}, ctx.Err()
		}
	}
	for _, result := range pending {
		e.harness.ready <- result
	}
	snapshot, err := e.harness.store.Snapshot(ctx)
	e.before = snapshot.Seq
	return event, err
}

func (e *groupedExecutionV160) AcknowledgeDrain(ctx context.Context, drain CodemodeDrain) error {
	if drain.DeliveryID != 1 {
		return e.ack.AcknowledgeDrain(ctx, drain)
	}
	s, err := e.harness.store.Snapshot(ctx)
	if err == nil && s.Seq != e.before+1 {
		err = fmt.Errorf("storage advanced %d -> %d, want one group publication", e.before, s.Seq)
	}
	if err == nil {
		err = checkGroupedSnapshotV160(s, false)
	}
	if err != nil || e.harness.sent.deliveries.Load() != 1 {
		fmt.Fprintf(os.Stderr, "before ack: %v; delivered=%d\n", err, e.harness.sent.deliveries.Load())
		os.Exit(74)
	}
	os.Exit(73) // No Ack, further delivery, graceful close, or defer.
	return nil
}

func checkGroupedSnapshotV160(s Snapshot, sealed bool) error {
	journals, runs := 0, 0
	for _, doc := range s.Documents {
		switch doc.Kind {
		case codemodeJournalKind:
			journals++
			var journal codemodeJournalState
			if err := fromObject(doc.Value, &journal, DefaultLimits()); err != nil {
				return err
			}
			if len(journal.Issued) != 3 || len(journal.Deliveries) != 3 {
				return fmt.Errorf("issued=%d recorded=%d; want 3 and 3", len(journal.Issued), len(journal.Deliveries))
			}
			for i, entry := range journal.Deliveries {
				if entry.Call.ID != i+1 || entry.Seq != i+1 || entry.Value != string(rune('a'+i)) ||
					(i == 0 && (entry.IssuedAfter == nil || *entry.IssuedAfter != 3)) || (i != 0 && entry.IssuedAfter != nil) {
					return fmt.Errorf("unexpected committed group: %+v", journal.Deliveries)
				}
			}
		case codemodeRunKind:
			runs++
			if !sealed {
				if doc.Value["result"] != nil || doc.Value["terminal"] != nil {
					return errors.New("published terminal before cutoff")
				}
				continue
			}
			if generationObject(doc.Value["result"])["value"] != "b" {
				return errors.New("missing settled result b")
			}
			position := generationObject(doc.Value["terminal"])
			if !equalJSONValue(position, JSON{"delivered": 2, "issued": 3, "committed": 3}) {
				return fmt.Errorf("terminal = %v; want delivered 2/issued 3/committed 3", position)
			}
		}
	}
	if journals != 1 || runs != 1 {
		return fmt.Errorf("journals=%d runs=%d; want one each", journals, runs)
	}
	return nil
}

func groupedHarnessV160(t *testing.T, dir string, store *SQLiteStorage, cut bool) (*Harness, *TaskDefinition, *journalWorkerRemoteV160) {
	t.Helper()
	remote, worker := newJournalWorkerSandboxV160(t)
	remote.groupJournal = cut // Reopen must use the recorded journal policy.
	var sandbox CodemodeSandbox = remote
	var ready chan codemodeCompletedCall
	var gates [2]chan struct{}
	if cut {
		ready = make(chan codemodeCompletedCall, 3)
		gates = [2]chan struct{}{make(chan struct{}), make(chan struct{})}
		sandbox = groupedSandboxV160{RemoteCodemodeSandbox: remote, ready: ready, gates: gates, store: store, sent: worker}
	}
	registry := NewRegistry()
	def := taskDefinition(t, "batch-drain-process-v160", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		result, err := r.runCodemodeWithCompletionQueue(ctx, "run", groupedScriptV160, sandbox, nil, nil, nil, nil, ready)
		if err != nil {
			return err
		}
		if result.Error != "" || !result.HasValue || result.Value != "b" {
			return fmt.Errorf("recovered result = %+v; want b", result)
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	if _, err := registry.RegisterTask(def); err != nil {
		t.Fatal(err)
	}
	globals := []CodemodeGlobal{{Name: "receipt", Version: 1, Execute: func(ctx context.Context, arg any) (any, error) {
		value, ok := arg.(string)
		if !ok || len(value) != 1 || value[0] < 'a' || value[0] > 'c' {
			return nil, fmt.Errorf("unexpected callback %v", arg)
		}
		if cut && value != "a" {
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
	return openHarness(t, store, Options{Registry: registry, CodemodeGlobals: globals}), def, worker
}

func TestV160CodemodeGroupCrashChild(t *testing.T) {
	dir := os.Getenv("GOAI_CODEMODE_GROUP_CRASH_DIR")
	if dir == "" {
		return
	}
	store, err := openSQLiteTestDirectory(filepath.Join(dir, "journal"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h, def, _ := groupedHarnessV160(t, dir, store, true)
	id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	row, err := h.WaitForTask(ctx, id)
	t.Fatalf("task did not exit at group commit: outcome=%+v, err=%v", row.State.Outcome, err)
}

// The new journal must remember outcomes that had already committed but the
// guest never consumed. The process exits before the first result drain ack.
func TestV160CodemodeCommittedGroupEarlyTerminalRecovery(t *testing.T) {
	officialWorkerPackageRootV160(t)
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestV160CodemodeGroupCrashChild$")
	cmd.Env = append(os.Environ(), "GOAI_CODEMODE_GROUP_CRASH_DIR="+dir)
	var exited *exec.ExitError
	if output, err := cmd.CombinedOutput(); !errors.As(err, &exited) || exited.ExitCode() != 73 {
		t.Fatalf("child did not exit after group adoption: %v\n%s", err, output)
	}
	store, err := openSQLiteTestDirectory(filepath.Join(dir, "journal"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkGroupedSnapshotV160(before, false); err != nil {
		t.Fatal(err)
	}
	var id ID
	for task, row := range before.Tasks {
		if row.Kind == "batch-drain-process-v160" {
			id = task
		}
	}
	if id == 0 {
		t.Fatal("missing original task")
	}
	h, _, worker := groupedHarnessV160(t, dir, store, false)
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	row, err := h.WaitForTask(ctx, id)
	if err != nil || row.State.Outcome == nil || row.State.Outcome.Status != "completed" {
		if row.State.Outcome != nil && row.State.Outcome.Error != nil {
			t.Fatal(row.State.Outcome.Error.Message)
		}
		t.Fatalf("recovered task: outcome=%+v, err=%v", row.State.Outcome, err)
	}
	after, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkGroupedSnapshotV160(after, true); err != nil {
		t.Fatal(err)
	}
	if effects, err := os.ReadFile(filepath.Join(dir, "effects")); err != nil || string(effects) != "abc" {
		t.Fatalf("effects=%q err=%v; want abc, no repeats", effects, err)
	}
	if worker.starts.Load() != 1 || worker.deliveries.Load() != 2 {
		t.Fatalf("replay starts=%d outcomes delivered=%d; want 1 and 2", worker.starts.Load(), worker.deliveries.Load())
	}
}
