package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

type mainToolRun struct {
	number int64
	output chan string
	finish chan struct{}
}
type mainToolRecovery struct {
	ref     ModelRef
	options Options
	runs    chan *mainToolRun
	count   *atomic.Int64
	clock   *atomic.Int64
}

func mainToolRecoverySetup(t *testing.T, safe bool) mainToolRecovery {
	t.Helper()
	var requests atomic.Int64
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			ch <- toolAnswer("c1", "work", JSON{})
		} else {
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	f := mainToolRecovery{ref: ref, options: options, runs: make(chan *mainToolRun, 8), count: new(atomic.Int64), clock: new(atomic.Int64)}
	f.clock.Store(1000)
	registry := NewRegistry()
	if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "work", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "main.recovery.work", Version: 1, ReplaySafe: safe, Execute: func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
		run := &mainToolRun{number: f.count.Add(1), output: make(chan string, 1), finish: make(chan struct{})}
		f.runs <- run
		if !safe && run.number > 1 {
			// As in the source helper, an unexpected unsafe replay finishes;
			// the assertions then expose its count and result, not a timeout.
			if err := api.Output(fmt.Sprintf("rerun %d\n", run.number)); err != nil {
				return ToolResult{}, err
			}
			return ToolResult{}, nil
		}
		for {
			select {
			case <-ctx.Done():
				return ToolResult{}, ctx.Err()
			case text := <-run.output:
				if err := api.Output(text); err != nil {
					return ToolResult{}, err
				}
			case <-run.finish:
				return ToolResult{}, nil
			}
		}
	}}); err != nil {
		t.Fatal(err)
	}
	f.options.Registry = registry
	return f
}
func mainToolOutput(live JSON) (string, bool) {
	tools, _ := live["tools"].([]any)
	if len(tools) == 0 {
		return "", false
	}
	tool, _ := tools[0].(map[string]any)
	text, ok := tool["output"].(string)
	return text, ok
}
func mainRecoveryAdvance(f *mainRecoveryHarness) {
	now := f.clock.Add(100)
	f.timerMu.Lock()
	var jobs []func()
	for _, timer := range f.timers {
		if !timer.cancelled && timer.due <= now {
			timer.cancelled = true
			jobs = append(jobs, timer.fire)
		}
	}
	f.timerMu.Unlock()
	for _, job := range jobs {
		job()
	}
}
func mainWriteToolOutput(t *testing.T, f *mainRecoveryHarness, run *mainToolRun, text, want string) {
	t.Helper()
	mainRecoveryAdvance(f)
	run.output <- text
	for {
		frame := watchReceive(t, f.frames)
		if got, ok := mainToolOutput(frame.value.Docs["pi.live"]); ok && got == want {
			return
		}
	}
}
func mainStoredToolPrefix(t *testing.T, f *mainRecoveryHarness, run *mainToolRun) string {
	t.Helper()
	var full string
	for n := 0; n < 3; n++ {
		line := fmt.Sprintf("run %d line %d long output\n", run.number, n)
		full += line
		mainWriteToolOutput(t, f, run, line, full)
	}
	stored, ok := mainToolOutput(f.live(f.stored()))
	if !ok || len(stored) <= 20 || !strings.HasPrefix(full, stored) {
		t.Fatal("stored progress is not an emitted prefix", stored, full)
	}
	return stored
}
func mainToolResults(t *testing.T, f *mainRecoveryHarness) []string {
	t.Helper()
	state, err := f.h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	var results []string
	for _, id := range ids(state.Entries) {
		e := state.Entries[id]
		if e.Conversation != f.c.ID() || messageEntryValue(e)["role"] != "toolResult" {
			continue
		}
		var receipt MessageReceipt
		if err := decodeEntryMessage(e, f.h.session.limits, &receipt); err != nil {
			t.Fatal(err)
		}
		var texts []string
		for _, block := range receipt.Content {
			if block.Type == "text" {
				texts = append(texts, block.Text)
			}
		}
		results = append(results, strings.Join(texts, "|"))
	}
	return results
}
func mainFinishToolSubmission(t *testing.T, f *mainRecoveryHarness, id ID) {
	t.Helper()
	sub, err := f.h.Submission(bg, id)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitSubmission(t, sub); got.Submission.Status != "done" {
		t.Fatal(got)
	}
	if err := f.h.WaitForIdle(bg); err != nil {
		t.Fatal(err)
	}
}

// f9a26365 harness-deferred-progress.test.ts:302-339. Commands replace the
// source's 20ms output loop, without adding checkpoint/Details barriers.
func TestMainToolProgressDoesNotResetCrashRestarts(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		setup := mainToolRecoverySetup(t, true)
		f := mainOpenSlowRecovery(t, b.store, setup.ref, setup.options, setup.clock, 50)
		if _, err := f.c.Submit(bg, Input{Content: "go"}); err != nil {
			t.Fatal(err)
		}
		for crashes := uint64(1); crashes <= 2; crashes++ {
			if err := f.h.Resume(bg); err != nil {
				t.Fatal(err)
			}
			run := watchReceive(t, setup.runs)
			if run.number != int64(crashes) {
				t.Fatalf("stored-own-progress run=%d want=%d", run.number, crashes)
			}
			mainStoredToolPrefix(t, f, run)
			store := f.crash()
			f = mainOpenSlowRecovery(t, store, setup.ref, setup.options, setup.clock, 50)
			state, err := f.h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, task := range state.Tasks {
				if task.Kind == "pi.tool" {
					found = true
					if task.Restarts != crashes {
						t.Errorf("restarts=%d want=%d execution=%+v checkpoint=%v", task.Restarts, crashes, task.Execution, task.Checkpoint)
					}
				}
			}
			if !found {
				t.Fatal("missing tool task")
			}
		}
		if err := f.h.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}

// Source341-363: no unsafe replay, exact retained output and interruption text.
func TestMainUnsafeToolCrashUsesStoredOutput(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		setup := mainToolRecoverySetup(t, false)
		f := mainOpenSlowRecovery(t, b.store, setup.ref, setup.options, setup.clock, 150)
		sub, err := f.c.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		run := watchReceive(t, setup.runs)
		stored := mainStoredToolPrefix(t, f, run)
		store := f.crash()
		state, err := testStorageProjection(store).Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := mainToolOutput(f.live(state)); !ok || got != stored {
			t.Fatal("crash changed stored output", got, stored)
		}
		f = mainOpenSlowRecovery(t, store, setup.ref, setup.options, setup.clock, 150)
		mainFinishToolSubmission(t, f, sub.ID())
		if setup.count.Load() != 1 {
			t.Error("unsafe tool reran", setup.count.Load())
		}
		want := []string{stored + "|<harness>\n[error] Tool work was interrupted and may have partially run\n</harness>"}
		if got := mainToolResults(t, f); !reflect.DeepEqual(got, want) {
			t.Errorf("results=%q want=%q", got, want)
		}
		if err := f.h.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}

// Source365-393: a safe rerun durably clears old output before executing, then
// survives another crash before emitting its own output. Existing native safe
// replay tests use healthy Close, not this repeated failed-Session boundary.
func TestMainSafeToolReplayClearsOutputThroughSecondCrash(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		setup := mainToolRecoverySetup(t, true)
		f := mainOpenSlowRecovery(t, b.store, setup.ref, setup.options, setup.clock, 150)
		sub, err := f.c.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		run := watchReceive(t, setup.runs)
		mainStoredToolPrefix(t, f, run)
		store := f.crash()
		f = mainOpenSlowRecovery(t, store, setup.ref, setup.options, setup.clock, 150)
		if err := f.h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		watchReceive(t, setup.runs) // second execution stays silent, awaiting cancellation
		if _, ok := mainToolOutput(f.live(f.stored())); ok {
			t.Error("safe replay did not store output removal before execute")
		}
		store = f.crash()
		state, err := testStorageProjection(store).Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := mainToolOutput(f.live(state)); ok {
			t.Error("cleared output reappeared after second crash")
		}
		f = mainOpenSlowRecovery(t, store, setup.ref, setup.options, setup.clock, 150)
		if err := f.h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		run = watchReceive(t, setup.runs)
		mainWriteToolOutput(t, f, run, "rerun 3\n", "rerun 3\n")
		close(run.finish)
		mainFinishToolSubmission(t, f, sub.ID())
		if setup.count.Load() != 3 {
			t.Error("wrong replay count", setup.count.Load())
		}
		if got, want := mainToolResults(t, f), []string{"rerun 3\n"}; !reflect.DeepEqual(got, want) {
			t.Errorf("results=%q want=%q", got, want)
		}
		if err := f.h.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}

// harness-tool-parks.test.ts:494-517 and :558-604. The recovery cases use
// real process loss after a woken execute intent instead of overlapping writers.
func TestSourceV160CanonicalToolParkRecovery(t *testing.T) {
	for _, mode := range []string{"removed", "unsafe", "safe"} {
		t.Run(mode, func(t *testing.T) { testV160ToolParkRecovery(t, mode) })
	}
}

func testV160ToolParkRecovery(t *testing.T, mode string) {
	t.Helper()
	const crashDir = "GOAI_TEST_TOOL_PARK_CRASH"
	const report = "Tool ci is not available: its parked call c1 cannot run again"
	childDir := os.Getenv(crashDir)
	starting := childDir != "" || mode == "removed"
	ctx, cancel := context.WithTimeout(bg, 15*time.Second)
	defer cancel()
	dir := childDir
	if dir == "" {
		dir = t.TempDir()
	}
	type addresses struct{ Conversation, Submission, Tool ID }
	var ids addresses
	if !starting {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSourceV160CanonicalToolParkRecovery$/^"+mode+"$")
		cmd.Env = append(os.Environ(), crashDir+"="+dir)
		output, err := cmd.CombinedOutput()
		exit, ok := err.(*exec.ExitError)
		if !ok || !exit.ProcessState.Sys().(syscall.WaitStatus).Signaled() || exit.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
			t.Fatalf("tool process did not reach its crash boundary: %v\n%s", err, output)
		}
		data, err := os.ReadFile(filepath.Join(dir, "addresses.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &ids); err != nil {
			t.Fatal(err)
		}
	}
	native, err := openSQLiteTestDirectory(filepath.Join(dir, "db"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = native.Close(bg) })
	wantDetails := JSON{"build": 1}
	wantDiagnostics := []any{JSON{"severity": "info", "message": "parked"}}
	readSlot := func(ctx context.Context) (JSON, error) {
		point := CurrentDocumentPoint()
		doc, found, err := native.FindDocument(ctx, DocumentAddress{Scope: "conversation", Owner: ids.Conversation, Kind: "pi.live"}, point)
		if err != nil || !found {
			return nil, fmt.Errorf("stored live document lookup: found=%v err=%v", found, err)
		}
		doc, found, err = native.DocumentAt(ctx, doc.ID, point)
		if err != nil || !found {
			return nil, fmt.Errorf("stored live document read: found=%v err=%v", found, err)
		}
		return canonicalToolSlot(doc.Value, ids.Tool), nil
	}
	if mode == "safe" && !starting {
		// Check SQLite before opening the writable Harness. These are the
		// crashed process's committed values, not its in-memory live view.
		crashed, found, err := native.Task(ctx, ids.Tool)
		checkpoint := taskCheckpointMap(crashed.State.Checkpoint)
		if err != nil || !found || crashed.State.Status != "running" || crashed.Restarts != 0 || checkpoint["phase"] != "execute" || checkpoint["checkpoint"] != "first" || checkpoint["replay"] != "safe" {
			t.Fatal("persisted woken intent after crash", crashed, found, err)
		}
		slot, err := readSlot(ctx)
		if err != nil || slot["status"] != "running" || slot["output"] != "cut off\n" || !equalJSONValue(slot["details"], wantDetails) || !equalJSONValue(slot["diagnostics"], wantDiagnostics) {
			t.Fatal("persisted woken slot after crash", slot, err)
		}
	}
	storage := &toolParkTestStorage{Storage: native, parked: make(chan ID, 2)}
	token := testSignalAnswer(t)
	key, err := token.Key("done")
	if err != nil {
		t.Fatal(err)
	}
	type attempt struct {
		Wake       *WakeReason
		Checkpoint any
	}
	seen, cleaned := make(chan attempt, 4), make(chan attempt, 4)
	type storedReplaySlot struct {
		slot JSON
		err  error
	}
	replaySlots := make(chan storedReplaySlot, 4)
	crashReady := make(chan struct{}, 1)
	reports := make(chan error, 32)
	var calls atomic.Int32
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if calls.Add(1) == 1 && starting {
			ch <- toolAnswer("c1", "ci", JSON{})
		} else {
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	options.Registry = NewRegistry()
	options.OnReport = func(err error) { reports <- err }
	if mode == "safe" {
		// Supply a durable cut-off output for the stronger Go recovery
		// control, rather than relying on any timer to store it.
		interval, window := int64(0), float64(0)
		options.Settings = &HarnessSettings{Progress: &ProgressSettings{OutputIntervalMs: &interval, PersistWithinMs: &window}}
	}
	if err := options.Registry.Register(ToolRegistration{
		Definition: goai.Tool{Name: "ci", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "test.park.recovery", Version: 1, ReplaySafe: mode == "safe",
		Execute: func(run context.Context, _ any, api *ToolAPI) (ToolResult, error) {
			seen <- attempt{api.Wake, api.Checkpoint}
			if mode == "removed" {
				signal, err := api.ReadSignal(run, token, "done")
				if err != nil {
					return ToolResult{}, err
				}
				if signal != nil {
					text, _ := generationObject(signal.Value)["text"].(string)
					return ToolResult{Content: text}, nil
				}
				if err := api.Output("started\n"); err != nil {
					return ToolResult{}, err
				}
				if err := api.Diagnostic(ToolDiagnostic{Severity: "info", Message: "run 1"}); err != nil {
					return ToolResult{}, err
				}
				if err := api.Details(JSON{"build": 1}); err != nil {
					return ToolResult{}, err
				}
				return ToolResult{Park: &ToolPark{Sleep: &ToolSleep{Signals: []SignalKey{key}, Checkpoint: JSON{"build": 1}}}}, nil
			}
			if api.Wake == nil && starting {
				if mode != "unsafe" {
					if err := api.Diagnostic(ToolDiagnostic{Severity: "info", Message: "parked"}); err != nil {
						return ToolResult{}, err
					}
					if err := api.Details(JSON{"build": 1}); err != nil {
						return ToolResult{}, err
					}
				}
				return ToolResult{Park: &ToolPark{Sleep: &ToolSleep{Signals: []SignalKey{key}, Checkpoint: "first"}}}, nil
			}
			if childDir != "" {
				text, details := "cut off\n", JSON{"build": 1}
				if mode == "unsafe" {
					text, details = "rerun\n", JSON{"woken": true}
				}
				if err := api.Output(text); err != nil {
					return ToolResult{}, err
				}
				if err := api.Details(details); err != nil {
					return ToolResult{}, err
				}
				crashReady <- struct{}{}
				<-run.Done()
				return ToolResult{}, run.Err()
			}
			if mode == "safe" {
				slot, err := readSlot(run)
				replaySlots <- storedReplaySlot{slot, err}
			}
			return ToolResult{}, nil
		},
		Cancel: func(_ context.Context, _ any, api *ToolAPI) error {
			cleaned <- attempt{api.Wake, api.Checkpoint}
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, storage, options)
	var c *ConversationHandle
	var sub *SubmissionHandle
	if starting {
		c = root(t, h, ref)
		sub, err = c.Submit(ctx, Input{Type: "input", Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		ids.Conversation, ids.Submission = c.ID(), sub.ID()
		select {
		case ids.Tool = <-storage.parked:
		case <-ctx.Done():
			t.Fatal("tool did not park", ctx.Err())
		}
		signalText := "x"
		if mode == "removed" {
			parked, found, err := h.Task(ctx, ids.Tool)
			if err != nil || !found || parked.State.Status != "sleeping" || !reflect.DeepEqual(parked.State.Signals, []SignalKey{key}) || !equalJSONValue(taskCheckpointMap(parked.State.Checkpoint)["checkpoint"], JSON{"build": 1}) {
				t.Fatal("parked before registry removal", parked.State, found, err)
			}
			options.Registry.Remove("ci")
			signalText = "late"
		}
		if got, err := h.Signal(ctx, ids.Tool, token, "done", JSON{"text": signalText}); err != nil || got != SignalDelivered {
			t.Fatalf("signal = %s, %v", got, err)
		}
		if childDir != "" {
			select {
			case <-crashReady:
			case <-ctx.Done():
				t.Fatal("woken run did not commit progress", ctx.Err())
			}
			wake := &WakeReason{Reason: "signal", Signal: key.Signal, Key: "done"}
			if len(seen) != 2 || !reflect.DeepEqual(<-seen, attempt{}) || !reflect.DeepEqual(<-seen, attempt{wake, "first"}) {
				t.Fatal("wrong pre-crash invocations")
			}
			state, err := h.Snapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			slot := canonicalToolSlot(mainContinueBoundaryDocument(t, state, ids.Conversation, "pi.live"), ids.Tool)
			wantOutput := "cut off\n"
			if mode == "unsafe" {
				wantOutput = "rerun\n"
			}
			if slot["status"] != "running" || slot["output"] != wantOutput {
				t.Fatalf("pre-crash slot = %v", slot)
			}
			data, err := json.Marshal(ids)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "addresses.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Kill(os.Getpid(), syscall.SIGKILL); err != nil {
				t.Fatal(err)
			}
			return
		}
	} else {
		if mode == "safe" {
			// Writable Open recovers the task before scheduling resumes.
			recovered, found, err := h.Task(ctx, ids.Tool)
			if err != nil || !found || recovered.State.Status != "pending" || recovered.Restarts != 1 || len(seen) != 0 {
				t.Fatal("paused recovered task", recovered, found, err)
			}
		}
		c, err = h.Conversation(ctx, ids.Conversation)
		if err != nil {
			t.Fatal(err)
		}
		sub, err = h.Submission(ctx, ids.Submission)
		if err != nil {
			t.Fatal(err)
		}
	}
	if settled, err := sub.Wait(ctx); err != nil || settled.Record.Status != "done" {
		t.Fatalf("submission = %+v, %v", settled.Record, err)
	}
	if mode == "safe" {
		observed := watchReceive(t, replaySlots)
		slot := observed.slot
		if observed.err != nil || slot["status"] != "running" || slot["output"] != nil || !equalJSONValue(slot["details"], wantDetails) || !equalJSONValue(slot["diagnostics"], wantDiagnostics) {
			t.Fatalf("before replay effect: stored slot=%v, err=%v; want old details and diagnostics but no output", slot, observed.err)
		}
	}
	record, err := h.WaitForTask(ctx, ids.Tool)
	if err != nil || record.State.Outcome == nil {
		t.Fatalf("task = %+v, %v", record, err)
	}
	// Pinned scheduler.ts withState drops the restart count once decided.
	if mode == "safe" && (record.State.Status != "terminal" || record.State.Outcome.Status != "completed" || record.Restarts != 0) {
		t.Fatalf("safe tool outcome = %+v; restarts=%d; error=%+v", record.State.Outcome, record.Restarts, record.State.Outcome.Error)
	}
	result := v160ToolResult(t, ctx, c, "c1")
	text := v160ToolResultText(result)
	switch mode {
	case "removed":
		outcome := record.State.Outcome
		if record.State.Status != "terminal" || outcome.Status != "failed" || outcome.Error == nil || outcome.Error.Message != "Tool ci is not available" {
			t.Fatalf("woken unavailable tool outcome = %+v, want failed", outcome)
		}
		if !result.IsError || !equalJSONValue(result.DetailsValue, JSON{"build": 1}) {
			t.Fatal("woken unavailable result lost retained details or error flag", result)
		}
		if !strings.Contains(text, "[info] run 1") || !strings.Contains(text, "Tool ci is not available") {
			t.Fatalf("woken unavailable result text = %q", text)
		}
		if len(seen) != 1 || len(cleaned) != 0 || !reflect.DeepEqual(<-seen, attempt{}) {
			t.Fatalf("removed tool ran or cleaned up again: runs=%d cancels=%d", len(seen), len(cleaned))
		}
		if outcome.Result == nil || !outcome.Result.Present {
			t.Fatal("woken unavailable result entry not linked to task", outcome)
		}
		entryID := submissionEntryID(generationObject(outcome.Result.Value)["entryId"])
		stored, found, err := native.Entry(ctx, entryID)
		wantDiagnostics := []any{JSON{"severity": "info", "message": "run 1"}, JSON{"severity": "error", "code": "tool_unavailable", "message": "Tool ci is not available"}}
		if err != nil || !found || stored.CommitSeq == 0 || stored.Entry.ByTask != ids.Tool || !equalJSONValue(generationObject(stored.Entry.Data)["diagnostics"], wantDiagnostics) {
			t.Fatalf("woken unavailable stored diagnostics = %v, found=%v err=%v; want %v", generationObject(stored.Entry.Data)["diagnostics"], found, err, wantDiagnostics)
		}
		for {
			select {
			case err := <-reports:
				if err.Error() == report {
					return
				}
			case <-ctx.Done():
				t.Fatal("missing report:", report)
			}
		}
	case "unsafe":
		if len(seen) != 0 || len(cleaned) != 1 || !reflect.DeepEqual(<-cleaned, attempt{nil, "first"}) || record.State.Outcome.Status != "failed" || !result.IsError || !strings.Contains(text, "Tool ci was interrupted and may have partially run") {
			t.Fatalf("unsafe tool: runs=%d, cancels=%d, task=%+v, result=%+v", len(seen), len(cleaned), record.State.Outcome, result)
		}
	case "safe":
		// The child checked the initial and woken attempts before it died;
		// the new process must add exactly one more with the same wake.
		want := attempt{&WakeReason{Reason: "signal", Signal: key.Signal, Key: "done"}, "first"}
		if len(seen) != 1 || !reflect.DeepEqual(<-seen, want) || len(cleaned) != 0 {
			t.Fatalf("safe replay attempts: pending receipts=%d cleanup calls=%d", len(seen), len(cleaned))
		}
		if result.IsError || !equalJSONValue(result.DetailsValue, wantDetails) {
			t.Fatal("safe replay lost details or returned an error", result)
		}
		if text != "<harness>\n[info] parked\n</harness>" {
			t.Fatalf("safe replay result text = %q", text)
		}
		outcome := record.State.Outcome.Result
		if outcome == nil || !outcome.Present {
			t.Fatal("safe replay lacks the terminal result link", record.State.Outcome)
		}
		entryID := submissionEntryID(generationObject(outcome.Value)["entryId"])
		stored, found, err := native.Entry(ctx, entryID)
		if err != nil || !found || stored.CommitSeq == 0 || stored.Entry.ByTask != ids.Tool || !equalJSONValue(generationObject(stored.Entry.Data)["diagnostics"], wantDiagnostics) {
			t.Fatal("safe replay lost the stored tool diagnostic", stored, found, err)
		}
	}
}

func v160ToolResult(t *testing.T, ctx context.Context, c *ConversationHandle, callID string) MessageReceipt {
	t.Helper()
	page, err := c.Entries(ctx, EntryQuery{}, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	var results []MessageReceipt
	for _, entry := range page.Items {
		if len(entry.Model) == 0 && entry.Kind != legacyMessageKind {
			continue
		}
		result, err := entryMessage(entry, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		if result.Role == goai.RoleToolResult && result.ToolCallID == callID {
			results = append(results, result)
		}
	}
	if len(results) != 1 {
		t.Fatalf("tool results for %s = %d; want one; entries=%+v", callID, len(results), page.Items)
	}
	return results[0]
}

func v160ToolResultText(result MessageReceipt) string {
	var text strings.Builder
	for _, block := range result.Content {
		text.WriteString(block.Text)
	}
	return text.String()
}

// Source: harness-park-children.test.ts, a running replay-unsafe caller does
// not retain its parked child across process loss.
func TestSourceV160ParkedChildAbandonedOnCallerRestart(t *testing.T) {
	const witnessEnv = "GOAI_TEST_PARKED_CHILD_CRASH_DIR"
	dir := os.Getenv(witnessEnv)
	crashing := dir != ""
	ctx, cancel := context.WithTimeout(bg, 15*time.Second)
	defer cancel()
	var ids struct{ Conversation, Submission, Parent, Child ID }
	if !crashing {
		dir = t.TempDir()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSourceV160ParkedChildAbandonedOnCallerRestart$")
		cmd.Env = append(os.Environ(), witnessEnv+"="+dir)
		output, err := cmd.CombinedOutput()
		exit, ok := err.(*exec.ExitError)
		if ctx.Err() != nil || !ok || exit.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
			t.Fatalf("unsafe caller did not crash: %v\n%s", err, output)
		}
		raw, err := os.ReadFile(filepath.Join(dir, "addresses.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &ids); err != nil {
			t.Fatal(err)
		}
	}
	store, err := OpenSQLite(filepath.Join(dir, "store.sqlite"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	order := make(chan string, 8)
	observed := &toolParkTestStorage{Storage: store, parked: make(chan ID, 2), onTool: func(record TaskRecord) {
		if !crashing && record.ID == ids.Child && record.State.Status == "terminal" {
			order <- "child-terminal"
		}
		if !crashing && record.ID == ids.Parent && record.State.Status == "running" {
			order <- "caller-running"
		}
	}}
	token := testSignalAnswer(t)
	key, err := token.Key("done")
	if err != nil {
		t.Fatal(err)
	}
	var requests, callerStarts, childStarts atomic.Int32
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		count := requests.Add(1)
		out := make(chan goai.Event, 1)
		if crashing && count == 1 {
			out <- toolAnswer("c1", "unsafe", JSON{})
		} else {
			out <- terminal("done")
		}
		close(out)
		return out
	})
	options.Registry = NewRegistry()
	started := make(chan ID, 1)
	diagnostics := make(chan []ToolDiagnostic, 4)
	research := ToolRegistration{Definition: goai.Tool{Name: "research", Parameters: json.RawMessage(`{"type":"object","properties":{"topic":{"type":"string"}},"required":["topic"]}`)}, Implementation: "test.restart.research", Version: 1, Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) {
		childStarts.Add(1)
		return ToolResult{Park: &ToolPark{Sleep: &ToolSleep{Signals: []SignalKey{key}}}}, nil
	}}
	unsafe := ToolRegistration{Definition: goai.Tool{Name: "unsafe", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "test.restart.unsafe", Version: 1, Execute: func(run context.Context, _ any, api *ToolAPI) (ToolResult, error) {
		callerStarts.Add(1)
		child, err := api.StartTool(run, "a", "research", JSON{"topic": "x"})
		if err != nil {
			return ToolResult{}, err
		}
		started <- child.ID()
		<-run.Done()
		return ToolResult{}, run.Err()
	}}
	if err := options.Registry.Install(&Extension{Name: "restart-tools", Tools: []ToolRegistration{research, unsafe}, ToolHooks: ToolHooks{
		AfterTool: func(_ context.Context, call goai.ToolCall, result ToolResult, _ *HookAPI) (*ToolResult, error) {
			if call.Name == "research" {
				diagnostics <- result.Diagnostics
			}
			return nil, nil
		},
	}}); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, observed, options)
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	if crashing {
		c := root(t, h, ref)
		sub, err := c.Submit(ctx, Input{Type: "input", Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case ids.Child = <-started:
		case <-ctx.Done():
			t.Fatal("unsafe caller did not issue its child", ctx.Err())
		}
		select {
		case parked := <-observed.parked:
			if parked != ids.Child {
				t.Fatalf("parked task=%d; want %d", parked, ids.Child)
			}
		case <-ctx.Done():
			t.Fatal("child did not park", ctx.Err())
		}
		child, found, err := h.Task(ctx, ids.Child)
		if err != nil || !found || child.Owner == 0 || child.State.Status != "sleeping" {
			t.Fatalf("pre-crash child=%+v, %v", child, err)
		}
		parent, found, err := h.Task(ctx, child.Owner)
		if err != nil || !found || parent.State.Status != "running" || taskCheckpointMap(parent.State.Checkpoint)["phase"] != "execute" {
			t.Fatalf("pre-crash caller=%+v, %v", parent, err)
		}
		ids.Conversation, ids.Submission, ids.Parent = c.ID(), sub.ID(), child.Owner
		raw, err := json.Marshal(ids)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "addresses.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Kill(os.Getpid(), syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
		return
	}
	sub, err := h.Submission(ctx, ids.Submission)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := sub.Wait(ctx); err != nil || result.Record.Status != "done" {
		t.Fatalf("submission after restart=%+v, %v", result.Record, err)
	}
	child, err := h.WaitForTask(ctx, ids.Child)
	if err != nil || child.Owner != ids.Parent || child.AbortReason != "restart" || child.State.Outcome == nil || child.State.Outcome.Status != "aborted" {
		t.Fatalf("child outcome=%+v, %v", child, err)
	}
	// Supplemental scheduler witness: the caller's restart cannot overtake
	// the child's independently confirmed cancellation and overwrite its reason.
	if len(order) != 2 || <-order != "child-terminal" || <-order != "caller-running" {
		t.Fatal("the caller resumed before its parked child ended")
	}
	const expected = "Tool research was parked: its caller ended with a restart"
	if len(diagnostics) != 1 {
		t.Fatalf("nested hook reports=%d; want one", len(diagnostics))
	}
	if d := <-diagnostics; len(d) != 1 || d[0].Message != expected {
		t.Fatalf("diagnostics=%+v; want %q", d, expected)
	}
	live, found, err := h.SnapshotDefinition(ctx, LiveDoc(), ids.Conversation, nil)
	_, hasNested := live.Get("nestedTools")
	if err != nil || !found || hasNested || requests.Load() != 1 || callerStarts.Load() != 0 || childStarts.Load() != 0 {
		t.Fatalf("after restart live=%v, %v; requests=%d caller starts=%d child starts=%d", live.Copy(), err, requests.Load(), callerStarts.Load(), childStarts.Load())
	}
}
