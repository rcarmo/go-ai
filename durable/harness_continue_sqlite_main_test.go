package durable

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Official f9a26365 harness-continue.test.ts:233-260. Use real pure-Go SQLite.
// Native process-loss adaptation: fail the actual Session, join the host, and
// reopen its original file; Close cannot flush a failed Session. This is not
// an OS-kill/power-loss assertion. The upstream helper similarly kills its
// storage facade rather than the test process. No task/checkpoint is seeded.
func TestMainContinueInterruptedSQLiteAfterStoredPartial(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "continue.sqlite"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	stream := &mainRecoveryStream{events: make(chan goai.Event, 1)}
	started := make(chan struct{})
	seen := make(chan []goai.Message, 1)
	var calls atomic.Int64
	ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, request *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		if calls.Add(1) == 1 {
			close(started)
			go func() { <-ctx.Done(); stream.close() }()
			return stream.events
		}
		seen <- request.Messages
		ch := make(chan goai.Event, 1)
		ch <- terminal("after the crash")
		close(ch)
		return ch
	})
	clock := new(atomic.Int64)
	clock.Store(1000)
	f := mainOpenRecovery(t, store, ref, options, clock, 0)
	first, err := f.c.Submit(bg, Input{Content: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, started)
	prefix := strings.Repeat("z", 64)
	f.partial(stream, prefix)
	for {
		if text, ok := mainRecoveryText(f.live(f.stored())); ok && text == prefix {
			break
		}
		watchReceive(t, f.replicated)
	}
	reopened := f.crash()
	disabled := false
	f = mainOpenRecoveryWithRetry(t, reopened, ref, options, clock, 0, &RetrySettings{Enabled: &disabled})
	old, err := f.h.Submission(bg, first.ID())
	if err != nil || old == nil {
		t.Fatal("recovered input", err)
	}
	interrupted := waitSubmission(t, old)
	if interrupted.Record.Status != "unanswered" || interrupted.Record.Reason != "interrupted" || calls.Load() != 1 {
		t.Fatal("crash must charge disabled retry before explicit continuation", interrupted.Record, calls.Load())
	}
	continued, err := f.c.Submit(bg, Input{Type: "continue"})
	if err != nil {
		t.Fatal(err)
	}
	got := waitSubmission(t, continued)
	if got.Record.Type != "continue" || got.Record.Status != "done" || got.Record.Entry != 0 || got.Record.Answer == 0 || got.Message == nil || len(got.Message.Content) != 1 || got.Message.Content[0].Text != "after the crash" {
		t.Fatal("continue must answer after crash without a new input", got)
	}
	messages := watchReceive(t, seen)
	if len(messages) != 1 || messages[0].Role != goai.RoleUser || len(messages[0].Content) != 1 || messages[0].Content[0].Text != "hi" || calls.Load() != 2 {
		t.Fatal("aborted partial must be excluded from actual provider context", messages, calls.Load())
	}
	state, err := f.h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	mainContinueOracleOneUser(t, state, f.c.ID())
	if status, err := old.Status(bg); err != nil || status.Status != "unanswered" || status.Reason != "interrupted" {
		t.Fatal("continued run rewrote the interrupted input", status, err)
	}
}
