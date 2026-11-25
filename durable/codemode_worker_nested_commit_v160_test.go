package durable

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Interpose only at the host's delivery and acknowledgement boundaries. The
// published guest still receives the real frames, in their original order.
type journalProofSandboxV160 struct {
	*RemoteCodemodeSandbox
	journal func(context.Context) (codemodeJournalState, error)
	steps   []string
}

func (s *journalProofSandboxV160) Start(ctx context.Context, source string, start CodemodeStart) (CodemodeExecution, error) {
	vm, err := s.RemoteCodemodeSandbox.Start(ctx, source, start)
	if err != nil {
		return nil, err
	}
	return &journalProofExecutionV160{CodemodeExecution: vm, proof: s}, nil
}

type journalProofExecutionV160 struct {
	CodemodeExecution
	proof *journalProofSandboxV160
}

func (e *journalProofExecutionV160) Deliver(ctx context.Context, delivery CodemodeDelivery) error {
	journal, err := e.proof.journal(ctx)
	if err != nil {
		return err
	}
	if len(journal.Deliveries) != delivery.Seq || delivery.Seq <= 0 || !reflect.DeepEqual(journal.Deliveries[delivery.Seq-1], delivery) {
		return fmt.Errorf("delivery %d is not yet confirmed in the journal (confirmed=%d)", delivery.Seq, len(journal.Deliveries))
	}
	e.proof.steps = append(e.proof.steps, fmt.Sprintf("deliver:%d=%v", delivery.Seq, delivery.Value))
	return e.CodemodeExecution.Deliver(ctx, delivery)
}

func (e *journalProofExecutionV160) AcknowledgeDrain(ctx context.Context, drain CodemodeDrain) error {
	journal, err := e.proof.journal(ctx)
	if err != nil {
		return err
	}
	issued := journal.FirstIssued
	if drain.DeliveryID != 0 {
		if drain.DeliveryID != len(journal.Deliveries) {
			return fmt.Errorf("unexpected drain for delivery %d", drain.DeliveryID)
		}
		issued = journal.Deliveries[drain.DeliveryID-1].IssuedAfter
	}
	if issued == nil || *issued != drain.Issued || len(journal.Issued) != drain.Issued {
		return fmt.Errorf("drain for delivery %d is not yet confirmed at count %d", drain.DeliveryID, drain.Issued)
	}
	e.proof.steps = append(e.proof.steps, fmt.Sprintf("ack:%d=%d", drain.DeliveryID, drain.Issued))
	return e.CodemodeExecution.(CodemodeDrainExecution).AcknowledgeDrain(ctx, drain)
}

// codemode.test.ts:200–212. The exact source script must see each nested
// result only after the real SQLite journal contains it.
func TestSourceV160CodemodePublishedWorkerNestedCallsCommitBeforeDelivery(t *testing.T) {
	const source = `const a = await tools.echo({ text: "a" }); const b = await tools.echo({ text: "b" }); return [a, b];`
	sandbox, remote := newJournalWorkerSandboxV160(t)
	probe := &journalProofSandboxV160{RemoteCodemodeSandbox: sandbox}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	registry := NewRegistry()
	codemode, err := NewCodemodeExtension(probe)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Install(&codemode); err != nil {
		t.Fatal(err)
	}
	echo := wrapRegistration("echo")
	echo.ReplaySafe = false
	echo.Execute = func(_ context.Context, raw any, _ *ToolAPI) (ToolResult, error) {
		return ToolResult{Content: "echo " + raw.(JSON)["text"].(string)}, nil
	}
	if err := registry.Register(echo); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			ch <- toolAnswer("nested", "codemode", JSON{"code": source})
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
	probe.journal = func(ctx context.Context) (codemodeJournalState, error) {
		state, err := h.Snapshot(ctx)
		if err != nil {
			return codemodeJournalState{}, err
		}
		for _, doc := range state.Documents {
			if doc.Kind == codemodeJournalKind {
				var journal codemodeJournalState
				err := decodeDocumentValue(doc, DefaultLimits(), &journal)
				return journal, err
			}
		}
		return codemodeJournalState{}, fmt.Errorf("missing Codemode journal")
	}
	conversation := root(t, h, ref)
	submission, err := conversation.Submit(ctx, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	if result, err := submission.Wait(ctx); err != nil || result.Submission.Status != "done" {
		t.Fatalf("submission status=%q err=%v; want done", result.Submission.Status, err)
	}
	result := v160ToolResult(t, ctx, conversation, "nested")
	if result.IsError || len(result.Content) != 2 || result.Content[1].Text != `["echo a","echo b"]` { // L209
		t.Errorf("tool result=%q error=%t; want [\"echo a\",\"echo b\"]", v160ToolResultText(result), result.IsError)
	}
	page, err := h.Commit(ctx, func(tx *Tx) (any, error) {
		return tx.ScanTasks(TaskQuery{Kind: "pi.tool"}, 100, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	nested := 0
	for _, task := range page.(Page[TaskRecord]).Items {
		if task.Input != nil && generationObject(task.Input.Value)["kind"] == "nested" {
			nested++
		}
	}
	if nested != 2 { // L210
		t.Errorf("nested tasks=%d; want 2", nested)
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	want := []string{"ack:0=1", "deliver:1=echo a", "ack:1=2", "deliver:2=echo b", "ack:2=2"}
	if !reflect.DeepEqual(probe.steps, want) || remote.deliveries.Load() != 2 || remote.starts.Load() != 1 {
		t.Errorf("confirmed boundaries=%v, guest deliveries=%d, guest starts=%d; want %v, 2 deliveries, 1 start", probe.steps, remote.deliveries.Load(), remote.starts.Load(), want)
	}
}
