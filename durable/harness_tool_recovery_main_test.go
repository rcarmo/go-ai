package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

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
	if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "work", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "main.recovery.work", Version: 1, ReplaySafe: safe, Execute: func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
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
		state, err := store.Snapshot(bg)
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
		state, err := store.Snapshot(bg)
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
