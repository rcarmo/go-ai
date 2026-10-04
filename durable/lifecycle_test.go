package durable

import (
	"context"
	"encoding/json"
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
	store, e := OpenJournal(dir, JournalOptions{})
	if e != nil {
		t.Fatal(e)
	}
	h := openHarness(t, store, options)
	r := root(t, h, ref)
	sub, e := r.Submit(bg, Input{Content: "recover", RequestID: "recover-1"})
	if e != nil {
		t.Fatal(e)
	}
	<-entered
	ctx, cancel := context.WithTimeout(bg, 20*time.Millisecond)
	defer cancel()
	if e = h.Close(ctx); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	if _, e = OpenJournal(dir, JournalOptions{}); !errors.Is(e, ErrOwned) {
		t.Fatal("successor opened during live code", e)
	}
	close(release)
	if e = h.Close(bg); e != nil {
		t.Fatal(e)
	}
	store, e = OpenJournal(dir, JournalOptions{})
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
	time.Sleep(20 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatal("Open dispatch effect")
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
	var cp generationCheckpoint
	if e = fromObject(result.Task.Checkpoint, &cp, h2.session.limits); e != nil || cp.Attempt != 2 {
		t.Fatal(cp, e)
	}
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
			store, dir := newJournal(t)
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
			store.file = &faultFile{journalFile: store.file, writeLimit: -1, syncErr: errors.New("unknownsync")}
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
			store, e = OpenJournal(dir, JournalOptions{})
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
	s, dir := newJournal(t)
	h := openHarness(t, s, options)
	r := root(t, h, ref)
	entered, release := make(chan struct{}), make(chan struct{})
	s.file = &faultFile{journalFile: s.file, writeLimit: -1, syncEntered: entered, release: release}
	done := make(chan error, 1)
	go func() { _, e := r.Submit(bg, Input{Content: "admitted", RequestID: "close-submit"}); done <- e }()
	<-entered
	ctx, cancel := context.WithTimeout(bg, 20*time.Millisecond)
	defer cancel()
	if e := h.Close(ctx); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	if _, e := OpenJournal(dir, JournalOptions{}); !errors.Is(e, ErrOwned) {
		t.Fatal(e)
	}
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if e := h.Close(bg); e != nil {
		t.Fatal(e)
	}
	reopened, e := OpenJournal(dir, JournalOptions{})
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
			s, dir := newJournal(t)
			h := openHarness(t, s, options)
			r := root(t, h, ref)
			if phase == "intent" {
				s.file = &faultFile{journalFile: s.file, writeLimit: -1, syncErr: errors.New("sync failure")}
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
			s.file = &faultFile{journalFile: s.file, writeLimit: -1, syncErr: errors.New("unknown outcome")}
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
			s, e = OpenJournal(dir, JournalOptions{})
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

func TestM1bCloseReceivedTerminalCommitsNoReplay(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		calls.Add(1)
		ch := make(chan goai.Event)
		go func() { close(entered); <-release; ch <- terminal("actual terminal"); close(ch) }()
		return ch
	})
	s, dir := newJournal(t)
	h := openHarness(t, s, options)
	r := root(t, h, ref)
	sub, e := r.Submit(bg, Input{Content: "close-terminal", RequestID: "terminal"})
	if e != nil {
		t.Fatal(e)
	}
	<-entered
	ctx, cancel := context.WithTimeout(bg, 20*time.Millisecond)
	defer cancel()
	if e = h.Close(ctx); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	close(release)
	if e = h.Close(bg); e != nil {
		t.Fatal(e)
	}
	s, e = OpenJournal(dir, JournalOptions{})
	if e != nil {
		t.Fatal(e)
	}
	h2 := openHarness(t, s, options)
	recovered, e := h2.Submission(bg, sub.ID())
	if e != nil {
		t.Fatal(e)
	}
	if waitSubmission(t, recovered).Submission.Status != "done" || calls.Load() != 1 {
		t.Fatal("observed terminal lost/replayed")
	}
}

func TestM1bPinnedModelRecoveryIgnoresRegistryBehaviorChange(t *testing.T) {
	var calls atomic.Int64
	firstEntered := make(chan struct{})
	release := make(chan struct{})
	originalAPI := goai.Api("m1b-pinned-original")
	changedAPI := goai.Api("m1b-pinned-changed")
	goai.RegisterApi(&goai.ApiProvider{Api: originalAPI, Stream: func(ctx context.Context, m *goai.Model, c *goai.Context, o *goai.StreamOptions) <-chan goai.Event {
		n := calls.Add(1)
		if m.Api != originalAPI || m.MaxTokens != 256 || m.SamplingParams["top_p"] != json.Number("0.5") || o.MaxTokens == nil || *o.MaxTokens != 77 {
			t.Errorf("behavior drift %+v %+v", m, o)
		}
		ch := make(chan goai.Event)
		go func() {
			if n == 1 {
				close(firstEntered)
				<-release
				close(ch)
				return
			}
			ch <- terminal("recovered")
			close(ch)
		}()
		return ch
	}})
	goai.RegisterApi(&goai.ApiProvider{Api: changedAPI, Stream: func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		t.Error("changed API dispatched")
		ch := make(chan goai.Event)
		close(ch)
		return ch
	}})
	defer goai.UnregisterApi(originalAPI)
	defer goai.UnregisterApi(changedAPI)
	model := fakeModel(originalAPI)
	model.SamplingParams = map[string]any{"top_p": 0.5}
	options := Options{Models: func(goai.Provider, string) *goai.Model { return model }}
	s, dir := newJournal(t)
	h := openHarness(t, s, options)
	tokens := 77
	r, e := h.Root(bg, AgentChange{Model: ModelRef{goai.ProviderOpenAI, model.ID}, Settings: RequestSettings{MaxTokens: &tokens}})
	if e != nil {
		t.Fatal(e)
	}
	sub, e := r.Submit(bg, Input{Content: "pinned", RequestID: "pinned"})
	if e != nil {
		t.Fatal(e)
	}
	<-firstEntered
	ctx, cancel := context.WithTimeout(bg, 20*time.Millisecond)
	defer cancel()
	_ = h.Close(ctx)
	close(release)
	_ = h.Close(bg)
	model = fakeModel(changedAPI)
	model.MaxTokens = 999
	model.SamplingParams = map[string]any{"top_p": 0.99}
	s, e = OpenJournal(dir, JournalOptions{})
	if e != nil {
		t.Fatal(e)
	}
	h2 := openHarness(t, s, options)
	snapshot, e := h2.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	for _, task := range snapshot.Tasks {
		if task.Status != "pending" {
			t.Fatal("recovered running without invocation", task.Status)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("Open dispatched")
	}
	resumed, e := h2.Submission(bg, sub.ID())
	if e != nil {
		t.Fatal(e)
	}
	if waitSubmission(t, resumed).Submission.Status != "done" || calls.Load() != 2 {
		t.Fatal("pinned recovery failed")
	}
}

func TestM1bActualRequestProcessCrashAndRecovery(t *testing.T) {
	if dir := os.Getenv("GOAI_M1B_CRASH_DIR"); dir != "" {
		api := goai.Api("m1b-processcrash")
		goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
			return nil
		}})
		s, e := OpenJournal(dir, JournalOptions{})
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
	s, e := OpenJournal(dir, JournalOptions{})
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

type terminalCommitFault struct {
	journalFile
	fail bool
}

func (f *terminalCommitFault) Write(p []byte) (int, error) {
	if len(p) >= 64 && p[10] == 2 {
		f.fail = true
	}
	return f.journalFile.Write(p)
}
func (f *terminalCommitFault) Sync() error {
	if f.fail {
		return errors.New("terminal acknowledgment uncertain")
	}
	return f.journalFile.Sync()
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
	s, dir := newJournal(t)
	h := openHarness(t, s, options)
	r := root(t, h, ref)
	sub, e := r.Submit(bg, Input{Content: "uncertainoutcome", RequestID: "outcome"})
	if e != nil {
		t.Fatal(e)
	}
	<-entered
	s.file = &terminalCommitFault{journalFile: s.file}
	close(release)
	ctx, cancel := context.WithTimeout(bg, time.Second)
	defer cancel()
	if _, e = sub.Wait(ctx); !errors.Is(e, ErrPoisoned) {
		t.Fatal("uncertaincommitnotpoisoned", e)
	}
	if e = h.Close(bg); e != nil {
		t.Fatal(e)
	}
	s, e = OpenJournal(dir, JournalOptions{})
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
