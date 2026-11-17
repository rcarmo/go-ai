package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestM1bCloseJoinsNoncooperativeAndReopenIntent(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		n := calls.Add(1)
		ch := make(chan goai.Event)
		go func() {
			if n == 1 {
				close(entered)
				<-release
				close(ch)
				return
			}
			ch <- terminal("answer")
			close(ch)
		}()
		return ch
	})
	dir := filepath.Join(t.TempDir(), "private")
	store, e := openSQLiteTestDirectory(dir, SQLiteOptions{})
	if e != nil {
		t.Fatal(e)
	}
	h := openHarness(t, store, options)
	r := root(t, h, ref)
	sub, e := r.Submit(bg, Input{Content: "recover", RequestID: "recover-1"})
	if e != nil {
		t.Fatal(e)
	}
	awaitTaskSignal(t, entered)
	view, err := r.View(bg)
	if err != nil {
		t.Fatal(err)
	}
	live, _ := view.Docs.Get("pi.live")
	run, _ := live.Get("run")
	taskNode, _ := run.Get("taskId")
	taskID := submissionEntryID(taskNode.Copy())
	if taskID == 0 {
		t.Fatal("request missing generation", live.Copy())
	}
	ctx, cancel := context.WithTimeout(bg, 20*time.Millisecond)
	defer cancel()
	if e = h.Close(ctx); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	if _, e = openSQLiteTestDirectory(dir, SQLiteOptions{}); !errors.Is(e, ErrOwned) {
		t.Fatal("successor opened during live code", e)
	}
	close(release)
	if e = h.Close(bg); e != nil {
		t.Fatal(e)
	}
	store, e = openSQLiteTestDirectory(dir, SQLiteOptions{})
	if e != nil {
		t.Fatal(e)
	}
	h2 := openHarness(t, store, options)
	snapshot, e := h2.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	if snapshot.Submissions[sub.ID()].Status != "pending" || calls.Load() != 1 {
		t.Fatal("close invented terminal or open dispatched")
	}
	pending, found, err := h2.Task(bg, taskID)
	if err != nil || !found || pending.Restarts != 0 || pending.State.Status != "pending" || taskCheckpointMap(pending.State.Checkpoint)["phase"] != "request" || submissionEntryID(taskCheckpointMap(pending.State.Checkpoint)["attempt"]) != 1 || calls.Load() != 1 {
		t.Fatal("clean-close request recovery", pending, err, calls.Load())
	}
	resumed, e := h2.Submission(bg, sub.ID())
	if e != nil {
		t.Fatal(e)
	}
	result := waitSubmission(t, resumed)
	if result.Submission.Status != "done" || calls.Load() != 2 {
		t.Fatal("recovery missing/duplicate")
	}
	snapshot, e = h2.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	// Terminal source tasks discard checkpoints. Observe the request record
	// after reopen, before dispatch, to verify its unchanged charged attempt.
	if len(snapshot.Entries) != 2 {
		t.Fatal("recovery duplicate user/outcome entries")
	}
}

func TestM1bRejectedAndUncertainIntentNoEffect(t *testing.T) {
	var calls atomic.Int64
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		calls.Add(1)
		ch := make(chan goai.Event, 1)
		ch <- terminal("answer")
		close(ch)
		return ch
	})
	for _, mode := range []string{"rejected", "uncertain"} {
		t.Run(mode, func(t *testing.T) {
			store, dir := newSQLiteTestStore(t)
			h := openHarness(t, store, options)
			r := root(t, h, ref)
			// Admission failure during submission is discovered by request ID on reopen;
			// no worker launches before acknowledgement. Full landed append is uncertain.
			if mode == "rejected" {
				_, e := r.Submit(bg, Input{Content: "prompt", RequestID: string(make([]byte, h.session.limits.MaxRequestIDBytes+1))})
				rejected(t, e)
				if calls.Load() != 0 {
					t.Fatal("effect on rejection")
				}
				return
			}
			installSQLiteAcknowledgement(store, func() error { return errors.New("unknown commit acknowledgement") })
			_, e := r.Submit(bg, Input{Content: "prompt", RequestID: "uncertain"})
			if !errors.Is(e, ErrPoisoned) {
				t.Fatal(e)
			}
			if calls.Load() != 0 {
				t.Fatal("effect on uncertain submission")
			}
			if e = h.Close(bg); e != nil {
				t.Fatal(e)
			}
			store, e = openSQLiteTestDirectory(dir, SQLiteOptions{})
			if e != nil {
				t.Fatal(e)
			}
			h2 := openHarness(t, store, options)
			rr, e := h2.Conversation(bg, 1)
			if e != nil {
				t.Fatal(e)
			}
			sub, e := rr.Submit(bg, Input{Content: "retry", RequestID: "uncertain"})
			if e != nil {
				t.Fatal(e)
			}
			if waitSubmission(t, sub).Submission.Status != "done" {
				t.Fatal("uncertain winner unrecoverable")
			}
		})
	}
}

func TestM1bCloseWaitDoesNotBlockOnAdmittedSubmit(t *testing.T) {
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		t.Error("effect after Close fence")
		ch := make(chan goai.Event)
		close(ch)
		return ch
	})
	s, dir := newSQLiteTestStore(t)
	h := openHarness(t, s, options)
	r := root(t, h, ref)
	entered, release := make(chan struct{}), make(chan struct{})
	blockSQLiteAcknowledgement(s, entered, release)
	done := make(chan error, 1)
	go func() { _, e := r.Submit(bg, Input{Content: "admitted", RequestID: "close-submit"}); done <- e }()
	<-entered
	ctx, cancel := context.WithTimeout(bg, 20*time.Millisecond)
	defer cancel()
	if e := h.Close(ctx); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	if _, e := openSQLiteTestDirectory(dir, SQLiteOptions{}); !errors.Is(e, ErrOwned) {
		t.Fatal(e)
	}
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if e := h.Close(bg); e != nil {
		t.Fatal(e)
	}
	reopened, e := openSQLiteTestDirectory(dir, SQLiteOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close(bg)
	snapshot := snap(t, reopened)
	if len(snapshot.Submissions) != 1 {
		t.Fatal("admission vanished")
	}
}

func TestM1bIntentAndOutcomeFaultBarriers(t *testing.T) {
	for _, phase := range []string{"intent", "outcome"} {
		t.Run(phase, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int64
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				calls.Add(1)
				ch := make(chan goai.Event)
				go func() { close(entered); <-release; ch <- terminal("answer"); close(ch) }()
				return ch
			})
			s, dir := newSQLiteTestStore(t)
			h := openHarness(t, s, options)
			r := root(t, h, ref)
			if phase == "intent" {
				installSQLiteAcknowledgement(s, func() error { return errors.New("unknown commit acknowledgement") })
				if _, e := r.Submit(bg, Input{Content: "intent", RequestID: "fault"}); !errors.Is(e, ErrPoisoned) {
					t.Fatal(e)
				}
				if calls.Load() != 0 {
					t.Fatal("effect beforeadopt")
				}
				_ = h.Close(bg)
				return
			}
			sub, e := r.Submit(bg, Input{Content: "outcome", RequestID: "fault"})
			if e != nil {
				t.Fatal(e)
			}
			<-entered
			installSQLiteAcknowledgement(s, func() error { return errors.New("unknown commit acknowledgement") })
			close(release)
			// First outcome allocation is itself a reservation barrier. Failed ack leaves
			// intent pending; reopen must recover through request ID rather than duplicate
			// committed outcomes. The next worker may retry remote billing, documented.
			ctx, cancel := context.WithTimeout(bg, 100*time.Millisecond)
			defer cancel()
			_, e = sub.Wait(ctx)
			if !errors.Is(e, ErrPoisoned) {
				t.Fatal(e)
			}
			_ = h.Close(bg)
			s, e = openSQLiteTestDirectory(dir, SQLiteOptions{})
			if e != nil {
				t.Fatal(e)
			}
			h2 := openHarness(t, s, options)
			snapshot, e := h2.Snapshot(bg)
			if e != nil {
				t.Fatal(e)
			}
			if snapshot.Submissions[sub.ID()].Status != "pending" {
				t.Fatal("uncertain invented terminal")
			}
		})
	}
}

// Source generation.ts classify checks runtime.signal before publishing any
// terminal provider message. A result returned after Close is not committed;
// clean reopen resends the unchanged attempt (generation-recovery:75-125).
// This supplemental noncooperative provider probe must not expect the legacy
// private producer's close-time terminal receipt or preserve it for compatibility.
func TestGenerationCloseBeforeTerminalClassificationResendsRequest(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		n := calls.Add(1)
		ch := make(chan goai.Event)
		go func() {
			defer close(ch)
			if n == 1 {
				close(entered)
				<-release
				ch <- terminal("late terminal")
			} else {
				ch <- terminal("reopened answer")
			}
		}()
		return ch
	})
	store, dir := newSQLiteTestStore(t)
	h := openHarness(t, store, options)
	t.Cleanup(func() { releaseTaskGate(release) })
	r := root(t, h, ref)
	sub, err := r.Submit(bg, Input{Content: "close-terminal", RequestID: "terminal"})
	if err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, entered)
	view, err := r.View(bg)
	if err != nil {
		t.Fatal(err)
	}
	live, _ := view.Docs.Get("pi.live")
	run, _ := live.Get("run")
	task, _ := run.Get("taskId")
	id := submissionEntryID(task.Copy())
	ctx, cancel := context.WithTimeout(bg, 20*time.Millisecond)
	defer cancel()
	if err := h.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("close failed to join live provider", err)
	}
	releaseTaskGate(release)
	if err := h.Close(bg); err != nil {
		t.Fatal(err)
	}
	store, err = openSQLiteTestDirectory(dir, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h2 := openHarness(t, store, options)
	record, found, err := h2.Task(bg, id)
	if err != nil || !found || record.Restarts != 0 || record.State.Status != "pending" || taskCheckpointMap(record.State.Checkpoint)["phase"] != "request" || submissionEntryID(taskCheckpointMap(record.State.Checkpoint)["attempt"]) != 1 {
		t.Fatal("late terminal changed request", record, err)
	}
	recovered, err := h2.Submission(bg, sub.ID())
	if err != nil {
		t.Fatal(err)
	}
	status, err := recovered.Status(bg)
	if err != nil || status.Status != "placed" || status.Answer != 0 || calls.Load() != 1 {
		t.Fatal("late terminal settled input or Open dispatched", status, calls.Load(), err)
	}
	c, err := h2.Conversation(bg, r.ID())
	if err != nil {
		t.Fatal(err)
	}
	before, err := c.Entries(bg, EntryQuery{Order: Ascending}, 100, nil)
	if err != nil || len(before.Items) != 1 || before.Items[0].Kind != "pi.user" {
		t.Fatal("late terminal was stored", before, err)
	}
	result := waitSubmission(t, recovered)
	if result.Record.Status != "done" || result.Message == nil || result.Message.Content[0].Text != "reopened answer" || calls.Load() != 2 {
		t.Fatal("resend result", result, calls.Load())
	}
	after, err := c.Entries(bg, EntryQuery{Order: Ascending}, 100, nil)
	if err != nil || len(after.Items) != 2 || after.Items[0].Kind != "pi.user" || after.Items[1].Kind != "pi.assistant" {
		t.Fatal("duplicate input or terminal", after, err)
	}
}

// Source request checkpoints pin the model reference and stream options, not
// process-local model metadata. A clean-close resend resolves that reference again.
func TestGenerationRecoveryUsesCurrentModelMetadataAndCommittedStreamOptions(t *testing.T) {
	var originalCalls, changedCalls atomic.Int64
	firstEntered, release := make(chan struct{}), make(chan struct{})
	originalAPI, changedAPI := goai.Api("recovery-metadata-original"), goai.Api("recovery-metadata-changed")
	goai.RegisterApi(&goai.ApiProvider{Api: originalAPI, Stream: func(_ context.Context, model *goai.Model, _ *goai.Context, options *goai.StreamOptions) <-chan goai.Event {
		if originalCalls.Add(1) != 1 {
			t.Error("resend dispatched the old API")
			ch := make(chan goai.Event, 1)
			ch <- terminal("old API replay")
			close(ch)
			return ch
		}
		if model.Api != originalAPI || model.MaxTokens != 256 || !equalJSONValue(model.SamplingParams, map[string]any{"top_p": 0.5}) || options.TimeoutMs == nil || *options.TimeoutMs != 1234 {
			t.Error("initial model metadata/options", model, options)
		}
		ch := make(chan goai.Event)
		go func() { close(firstEntered); <-release; close(ch) }()
		return ch
	}})
	goai.RegisterApi(&goai.ApiProvider{Api: changedAPI, Stream: func(_ context.Context, model *goai.Model, input *goai.Context, options *goai.StreamOptions) <-chan goai.Event {
		changedCalls.Add(1)
		if model.Api != changedAPI || model.MaxTokens != 999 || !equalJSONValue(model.SamplingParams, map[string]any{"top_p": 0.99}) || options.TimeoutMs == nil || *options.TimeoutMs != 1234 {
			t.Error("resend must resolve current metadata but keep committed options", model, options)
		}
		if len(input.Messages) != 1 || input.Messages[0].Role != goai.RoleUser || len(input.Messages[0].Content) != 1 || input.Messages[0].Content[0].Text != "pinned" {
			t.Error("resend context changed", input)
		}
		ch := make(chan goai.Event, 1)
		ch <- terminal("recovered")
		close(ch)
		return ch
	}})
	t.Cleanup(func() { goai.UnregisterApi(originalAPI); goai.UnregisterApi(changedAPI) })
	model := fakeModel(originalAPI)
	model.SamplingParams = map[string]any{"top_p": 0.5}
	timeout := 1234
	options := Options{Models: func(goai.Provider, string) *goai.Model { return model }, Settings: &HarnessSettings{Stream: RequestSettings{TimeoutMs: &timeout}}}
	store, dir := newSQLiteTestStore(t)
	h := openHarness(t, store, options)
	cleanupTaskGates(t, release)
	r := root(t, h, ModelRef{Provider: goai.ProviderOpenAI, ID: model.ID})
	sub, err := r.Submit(bg, Input{Content: "pinned", RequestID: "pinned"})
	if err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, firstEntered)
	view, err := r.View(bg)
	if err != nil {
		t.Fatal(err)
	}
	live := immutableViewMember(t, view.Docs, "pi.live")
	run := immutableViewMember(t, live, "run")
	id := submissionEntryID(immutableViewMember(t, run, "taskId").Number())
	before, found, err := h.Task(bg, id)
	if err != nil || !found {
		t.Fatal("request task", before, err)
	}
	checkpoint := before.State.Checkpoint
	cp := taskCheckpointMap(checkpoint)
	if cp["phase"] != "request" || submissionEntryID(cp["attempt"]) != 1 || !equalJSONValue(cp["model"], JSON{"provider": "openai", "modelId": "durable-test"}) || !equalJSONValue(cp["streamOptions"], JSON{"timeoutMs": 1234}) {
		t.Fatal("canonical committed request", cp)
	}
	ctx, cancel := context.WithTimeout(bg, 20*time.Millisecond)
	defer cancel()
	if err := h.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("close must join provider", err)
	}
	releaseTaskGate(release)
	if err := h.Close(bg); err != nil {
		t.Fatal(err)
	}
	model = fakeModel(changedAPI)
	model.MaxTokens, model.SamplingParams = 999, map[string]any{"top_p": 0.99}
	changedTimeout := 999
	options.Settings = &HarnessSettings{Stream: RequestSettings{TimeoutMs: &changedTimeout}}
	store, err = openSQLiteTestDirectory(dir, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h2 := openHarness(t, store, options)
	reopened, found, err := h2.Task(bg, id)
	if err != nil || !found || reopened.State.Status != "pending" || reopened.Restarts != 0 || !equalJSONValue(reopened.State.Checkpoint, checkpoint) || originalCalls.Load() != 1 || changedCalls.Load() != 0 {
		t.Fatal("Open must retain the request without dispatch", reopened, err)
	}
	resumed, err := h2.Submission(bg, sub.ID())
	if err != nil {
		t.Fatal(err)
	}
	if result := waitSubmission(t, resumed); result.Record.Status != "done" || result.Message == nil || len(result.Message.Content) != 1 || result.Message.Content[0].Text != "recovered" || originalCalls.Load() != 1 || changedCalls.Load() != 1 {
		t.Fatal("current metadata resend failed", result, originalCalls.Load(), changedCalls.Load())
	}
}

func TestM1bActualRequestProcessCrashAndRecovery(t *testing.T) {
	if dir := os.Getenv("GOAI_M1B_CRASH_DIR"); dir != "" {
		api := goai.Api("m1b-processcrash")
		goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
			return nil
		}})
		s, e := openSQLiteTestDirectory(dir, SQLiteOptions{})
		if e != nil {
			os.Exit(91)
		}
		h, e := Open(bg, s, Options{Models: func(goai.Provider, string) *goai.Model { return fakeModel(api) }})
		if e != nil {
			os.Exit(92)
		}
		r, e := h.Root(bg, AgentChange{Model: ModelRef{goai.ProviderOpenAI, "durable-test"}})
		if e != nil {
			os.Exit(93)
		}
		sub, e := r.Submit(bg, Input{Content: "crash", RequestID: "process-crash"})
		if e != nil {
			os.Exit(94)
		}
		_, _ = sub.Wait(bg)
		os.Exit(95)
	}
	dir := filepath.Join(t.TempDir(), "private")
	cmd := exec.Command(os.Args[0], "-test.run=^TestM1bActualRequestProcessCrashAndRecovery$")
	cmd.Env = append(os.Environ(), "GOAI_M1B_CRASH_DIR="+dir)
	if e := cmd.Run(); e == nil {
		t.Fatal("child not killed")
	}
	var calls atomic.Int64
	api := goai.Api("m1b-processcrash")
	goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		calls.Add(1)
		ch := make(chan goai.Event, 1)
		ch <- terminal("recovered")
		close(ch)
		return ch
	}})
	defer goai.UnregisterApi(api)
	s, e := openSQLiteTestDirectory(dir, SQLiteOptions{})
	if e != nil {
		t.Fatal(e)
	}
	h := openHarness(t, s, Options{Models: func(goai.Provider, string) *goai.Model { return fakeModel(api) }})
	snapshot, e := h.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	if calls.Load() != 0 || len(snapshot.Submissions) != 1 || len(snapshot.Entries) != 1 {
		t.Fatal("Opendispatched/crashprefixwrong")
	}
	for _, task := range snapshot.Tasks {
		if task.Status != "pending" {
			t.Fatal("recoveredrunning")
		}
	}
	r, e := h.Conversation(bg, 1)
	if e != nil {
		t.Fatal(e)
	}
	sub, e := r.Submit(bg, Input{Content: "retry-different", RequestID: "process-crash"})
	if e != nil {
		t.Fatal(e)
	}
	result := waitSubmission(t, sub)
	if result.Submission.Status != "done" || calls.Load() != 1 {
		t.Fatal("requestrecovery")
	}
	snapshot, e = h.Snapshot(bg)
	if e != nil || len(snapshot.Entries) != 2 {
		t.Fatal("duplicatecontext/outcome")
	}
}

func TestM1bFullTerminalUncertainCommitReopenWinner(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		calls.Add(1)
		ch := make(chan goai.Event)
		go func() { close(entered); <-release; ch <- terminal("landed outcome"); close(ch) }()
		return ch
	})
	s, dir := newSQLiteTestStore(t)
	h := openHarness(t, s, options)
	r := root(t, h, ref)
	sub, e := r.Submit(bg, Input{Content: "uncertainoutcome", RequestID: "outcome"})
	if e != nil {
		t.Fatal(e)
	}
	<-entered
	installSQLiteCommitAcknowledgement(s, func() error { return errors.New("terminal commit acknowledgement lost") })
	close(release)
	ctx, cancel := context.WithTimeout(bg, time.Second)
	defer cancel()
	if _, e = sub.Wait(ctx); !errors.Is(e, ErrPoisoned) {
		t.Fatal("uncertaincommitnotpoisoned", e)
	}
	if e = h.Close(bg); e != nil {
		t.Fatal(e)
	}
	s, e = openSQLiteTestDirectory(dir, SQLiteOptions{})
	if e != nil {
		t.Fatal(e)
	}
	h2 := openHarness(t, s, options)
	rr, e := h2.Conversation(bg, 1)
	if e != nil {
		t.Fatal(e)
	}
	recovered, e := rr.Submit(bg, Input{Content: "retry", RequestID: "outcome"})
	if e != nil || recovered.ID() != sub.ID() {
		t.Fatal(e)
	}
	result := waitSubmission(t, recovered)
	if result.Submission.Status != "done" || calls.Load() != 1 {
		t.Fatal("committedwinnerredispatched")
	}
	state, e := h2.Snapshot(bg)
	if e != nil || len(state.Entries) != 2 {
		t.Fatal("duplicateoutcome")
	}
}
