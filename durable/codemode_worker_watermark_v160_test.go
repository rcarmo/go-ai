package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

type pauseFirstDeliveryDrainV160 struct {
	*journalWorkerRemoteV160
	reached chan<- CodemodeDrain
}

func (r pauseFirstDeliveryDrainV160) Execute(ctx context.Context, code string, start CodemodeStart, exchange func(context.Context, CodemodeEvent) (CodemodeRemoteReply, error), receive func(context.Context) (CodemodeDelivery, error)) error {
	return r.journalWorkerRemoteV160.Execute(ctx, code, start, func(ctx context.Context, event CodemodeEvent) (CodemodeRemoteReply, error) {
		if r.starts.Load() == 1 && event.Drain != nil && event.Drain.DeliveryID == 1 {
			select {
			case r.reached <- *event.Drain:
			case <-ctx.Done():
			}
			<-ctx.Done()
			return CodemodeRemoteReply{Stop: true}, ctx.Err()
		}
		return exchange(ctx, event)
	}, receive)
}

// codemode.test.ts:419–446. Instead of deleting a confirmed count, stop the
// first guest after it emits the real drain but before the host can adopt it.
func TestSourceV160CodemodePublishedWorkerRestoresUnconfirmedWatermark(t *testing.T) {
	const source = `await tools.echo({ text: "a" }); await tools.hold({});`
	sandbox, remote := newJournalWorkerSandboxV160(t)
	reached := make(chan CodemodeDrain, 1)
	sandbox.Remote = pauseFirstDeliveryDrainV160{journalWorkerRemoteV160: remote, reached: reached}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	registry := NewRegistry()
	codemode, err := NewCodemodeExtension(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Install(&codemode); err != nil {
		t.Fatal(err)
	}
	var echoes, holds atomic.Int32
	holding := make(chan struct{})
	echo := wrapRegistration("echo")
	echo.ReplaySafe = false
	echo.Execute = func(_ context.Context, raw any, _ *ToolAPI) (ToolResult, error) {
		echoes.Add(1)
		return ToolResult{Content: "echo " + raw.(JSON)["text"].(string)}, nil
	}
	hold := wrapRegistration("hold")
	hold.ReplaySafe = true
	hold.Execute = func(ctx context.Context, _ any, _ *ToolAPI) (ToolResult, error) {
		if holds.Add(1) == 1 {
			close(holding)
			<-ctx.Done()
			return ToolResult{}, ctx.Err()
		}
		return ToolResult{Content: "held"}, nil
	}
	for _, tool := range []ToolRegistration{echo, hold} {
		if err := registry.Register(tool); err != nil {
			t.Fatal(err)
		}
	}
	var requests atomic.Int32
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			ch <- toolAnswer("watermark", "codemode", JSON{"code": source})
		} else {
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, options)
	conversation := root(t, h, ref)
	sub, err := conversation.Submit(ctx, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case drain := <-reached:
		if drain.Issued != 2 || drain.Final {
			t.Fatalf("initial guest emitted %+v; want result-1 idle drain at count 2", drain)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened := reopenStoreAfterHarnessClose(t, store).(*SQLiteStorage)
	next := openHarness(t, reopened, options)
	before := watermarkJournalV160(t, ctx, next)
	if before.FirstIssued == nil || *before.FirstIssued != 1 || len(before.Issued) != 1 || len(before.Deliveries) != 1 ||
		before.Deliveries[0].IssuedAfter != nil || before.Deliveries[0].Call.Name != "echo" || before.Deliveries[0].Value != "echo a" || holds.Load() != 0 {
		t.Fatalf("reopened journal = %+v, holds=%d; want a confirmed echo but no result-1 count or admitted hold", before, holds.Load())
	}
	if err := next.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-holding:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// L437–439: the hold callback can run only after the replayed drain has
	// been acknowledged. Its signal is a deterministic readback barrier.
	live := watermarkJournalV160(t, ctx, next)
	if len(live.Issued) != 2 || live.Issued[1].Name != "hold" || len(live.Deliveries) != 1 ||
		live.Deliveries[0].IssuedAfter == nil || *live.Deliveries[0].IssuedAfter != 2 {
		t.Fatalf("journal at replayed hold = %+v; want result 1 acknowledged at count 2", live)
	}
	if err := next.Close(ctx); err != nil {
		t.Fatal(err)
	}
	last := openHarness(t, reopenStoreAfterHarnessClose(t, reopened), options)
	if err := last.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	pending, err := last.Submission(ctx, sub.ID())
	if err != nil || pending == nil {
		t.Fatalf("second reopen submission: %v, %v", pending, err)
	}
	if result, err := pending.Wait(ctx); err != nil || result.Submission.Status != "done" { // L444
		t.Fatalf("second reopen submission status=%q err=%v; want done", result.Submission.Status, err)
	}
	if err := last.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if echoes.Load() != 1 || holds.Load() != 2 || remote.starts.Load() != 3 || requests.Load() != 2 {
		t.Errorf("echoes=%d holds=%d guest starts=%d model requests=%d; want 1, 2, 3, 2", echoes.Load(), holds.Load(), remote.starts.Load(), requests.Load())
	}
}

func watermarkJournalV160(t *testing.T, ctx context.Context, h *Harness) codemodeJournalState {
	t.Helper()
	state, err := h.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var journal codemodeJournalState
	count := 0
	for _, doc := range state.Documents {
		if doc.Kind == codemodeJournalKind {
			count++
			if err := decodeDocumentValue(doc, DefaultLimits(), &journal); err != nil {
				t.Fatal(err)
			}
		}
	}
	if count != 1 {
		t.Fatalf("journals = %d; want one", count)
	}
	return journal
}
