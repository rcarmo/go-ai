package durable

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

type heldSubmissionWaitRead struct {
	Storage
	hold    atomic.Bool
	entered chan SubmissionRecord
	release chan struct{}
}

func (s *heldSubmissionWaitRead) Submission(ctx context.Context, id ID) (SubmissionRecord, bool, error) {
	record, found, err := s.Storage.Submission(ctx, id)
	if s.hold.CompareAndSwap(true, false) {
		s.entered <- record
		<-s.release
	}
	return record, found, err
}

// harness-submissions.test.ts:184-216. The sole SQLite engine is reopened;
// the public Storage read, not a replacement Session snapshot, spans Close.
func TestSourceV160SubmissionWaitReadAcrossClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.sqlite")
	ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event)
		go func() { <-ctx.Done(); close(ch) }()
		return ch
	})
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, options)
	input, err := root(t, h, ref).Submit(bg, Input{Type: "input", Content: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Close(bg); err != nil {
		t.Fatal(err)
	}
	store, err = OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	wrapper := &heldSubmissionWaitRead{
		Storage: store, entered: make(chan SubmissionRecord, 1), release: make(chan struct{}),
	}
	reopened, err := Open(bg, wrapper, options)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close(bg)
	defer releaseTaskGate(wrapper.release)
	sub, err := reopened.Submission(bg, input.ID())
	if err != nil || sub == nil || sub.ID() != input.ID() {
		t.Fatal(sub, err)
	}
	wrapper.hold.Store(true)
	result := make(chan error, 1)
	go func() { _, err := sub.Wait(bg); result <- err }()
	record := watchReceive(t, wrapper.entered)
	if record.ID != input.ID() || record.Status != "placed" {
		t.Fatal("held read", record)
	}

	// Close seals synchronously. Only its caller's join is canceled, so this
	// acknowledges the seal while the in-flight delegated read still holds.
	caller, cancel := context.WithCancel(bg)
	cancel()
	if err := reopened.Close(caller); !errors.Is(err, context.Canceled) {
		t.Fatalf("start close without joining: %v", err)
	}
	close(wrapper.release)
	if err := watchReceive(t, result); !errors.Is(err, ErrClosed) {
		t.Fatalf("wait after close began: %v; want ErrClosed", err)
	}
	if err := reopened.Close(bg); err != nil {
		t.Fatal(err)
	}
}

type lateSubmissionReceipt struct {
	seq    uint64
	record SubmissionRecord
}

type lateSubmissionCommit struct {
	Storage
	adopted chan lateSubmissionReceipt
	release chan struct{}
}

func (s *lateSubmissionCommit) Commit(ctx context.Context, batches [][]StorageWrite, receive func(int, uint64)) (CommitResult, error) {
	var receipt lateSubmissionReceipt
	result, err := s.Storage.Commit(ctx, batches, func(index int, seq uint64) {
		if receive != nil {
			receive(index, seq)
		}
		for _, write := range batches[index] {
			if write.Type == "submission" {
				if record, ok := recordValue[SubmissionRecord](write.Value); ok && record.Type == "input" && record.Status == "done" {
					receipt = lateSubmissionReceipt{seq, record}
				}
			}
		}
	})
	if err == nil && receipt.seq != 0 {
		s.adopted <- receipt
		<-s.release
	}
	return result, err
}

// harness-submissions.test.ts:319-336. Read the observer's witness in the
// waiter's continuation, not later in the test (which could mask a race).
func TestSourceV160SubmissionWaitFollowsSettledPublication(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	wrapper := &lateSubmissionCommit{
		Storage: store, adopted: make(chan lateSubmissionReceipt, 1), release: make(chan struct{}),
	}
	respond := make(chan struct{})
	ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		go func() {
			defer close(ch)
			select {
			case <-respond:
				ch <- terminal("hi")
			case <-ctx.Done():
			}
		}()
		return ch
	})
	h := openHarness(t, wrapper, options)
	t.Cleanup(func() { releaseTaskGate(wrapper.release) })
	c := root(t, h, ref)
	var id, observedSeq, observedAnswer, observedCount atomic.Uint64
	observer, err := h.ObserveCommits(CommitObserver{Advance: func(_ context.Context, p CommitPublication) error {
		for i := range p.Changes.Len() {
			change, _ := p.Changes.Index(i)
			member, _ := change.Copy().(map[string]any)
			record, _ := member["value"].(map[string]any)
			if member["type"] == "submission" && record["status"] == "done" && uint64(submissionEntryID(record["id"])) == id.Load() {
				observedSeq.Store(p.Seq)
				observedAnswer.Store(uint64(submissionEntryID(record["answer"])))
				observedCount.Add(1)
			}
		}
		return nil
	}, End: func(WatchEnd) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Detach()
	sub, err := c.Submit(bg, Input{Type: "input", Content: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	id.Store(uint64(sub.ID()))
	type waitResult struct {
		seq, answer, count uint64
		result             Settlement
		err                error
	}
	returned := make(chan waitResult, 1)
	progress := make(chan struct{}, 16)
	go func() {
		result, err := sub.Wait(submissionWaitContext{bg, progress})
		returned <- waitResult{observedSeq.Load(), observedAnswer.Load(), observedCount.Load(), result, err}
	}()
	awaitSubmissionWaiters(t, h, sub.ID(), 1, progress)
	close(respond)
	receipt := watchReceive(t, wrapper.adopted)
	if receipt.record.ID != sub.ID() || receipt.record.Answer == 0 || observedCount.Load() != 0 {
		t.Fatal("held storage reply", receipt, observedCount.Load())
	}
	close(wrapper.release)
	got := watchReceive(t, returned)
	if got.err != nil || got.result.Record.ID != sub.ID() || got.result.Record.Status != "done" || got.result.Record.Answer != receipt.record.Answer {
		t.Fatal("wait settlement", got.result, got.err)
	}
	if got.count != 1 || got.seq != receipt.seq || got.answer != uint64(receipt.record.Answer) {
		t.Fatalf("observation at Wait return: count=%d seq=%d answer=%d; want 1/%d/%d", got.count, got.seq, got.answer, receipt.seq, receipt.record.Answer)
	}
}
