package durable

import (
	"context"
	"errors"
	"testing"
	"time"
)

type drainCommitVM120 struct{ acknowledgements int }

func (v *drainCommitVM120) AcknowledgeDrain(context.Context, CodemodeDrain) error {
	v.acknowledgements++
	return nil
}

func TestCodemode120DrainCommitBeforeAcknowledgement(t *testing.T) {
	call := CodemodeCall{ID: 1, Name: "leaf", Arguments: JSON{}}
	journal := codemodeJournalState{Issued: []CodemodeCall{call}}
	vm := &drainCommitVM120{}
	commits := 0
	commit := func(_ context.Context, j codemodeJournalState) error {
		commits++
		if vm.acknowledgements != commits-1 {
			t.Fatal("VM released before commit")
		}
		if commits == 1 && (j.FirstIssued == nil || *j.FirstIssued != 1) {
			t.Fatal("initial count missing")
		}
		return nil
	}
	if err := acknowledgeCodemodeDrain(context.Background(), vm, &journal, CodemodeDrain{Issued: 1}, 1, 0, commit); err != nil {
		t.Fatal(err)
	}
	journal.Deliveries = []CodemodeDelivery{{Seq: 1, Call: call, HasValue: true, Value: "ok"}}
	if err := acknowledgeCodemodeDrain(context.Background(), vm, &journal, CodemodeDrain{DeliveryID: 1, Issued: 1}, 1, 1, commit); err != nil {
		t.Fatal(err)
	}
	if journal.Deliveries[0].IssuedAfter == nil || commits != 2 || vm.acknowledgements != 2 {
		t.Fatal(journal, commits, vm)
	}
	// Reopened VM verifies both counts without rewriting the journal.
	reopened := &drainCommitVM120{}
	noCommit := func(context.Context, codemodeJournalState) error { t.Fatal("replay rewrote count"); return nil }
	for delivered := 0; delivered < 2; delivered++ {
		if err := acknowledgeCodemodeDrain(context.Background(), reopened, &journal, CodemodeDrain{DeliveryID: delivered, Issued: 1}, 1, delivered, noCommit); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCodemode120DrainCommitFailureAndReplayDivergenceFence(t *testing.T) {
	for _, mode := range []string{"commit", "count", "identity", "observed", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			journal := codemodeJournalState{Issued: []CodemodeCall{{ID: 1, Name: "leaf", Arguments: JSON{}}}}
			vm := &drainCommitVM120{}
			drain := CodemodeDrain{Issued: 1}
			observed := 1
			sentinel := errors.New("commit failed")
			commit := func(context.Context, codemodeJournalState) error {
				if mode == "commit" {
					return sentinel
				}
				if mode == "cancel" {
					cancel()
				}
				return nil
			}
			switch mode {
			case "count":
				journal.FirstIssued = watermark120(0)
			case "identity":
				drain.DeliveryID = 1
			case "observed":
				observed = 0
			}
			err := acknowledgeCodemodeDrain(ctx, vm, &journal, drain, observed, 0, commit)
			if err == nil || vm.acknowledgements != 0 {
				t.Fatal("VM fence bypassed", err, vm)
			}
			if mode == "commit" && (journal.FirstIssued != nil || !errors.Is(err, sentinel)) {
				t.Fatal("failed count adopted", journal, err)
			}
		})
	}
}

func TestCodemode120DrainCoordinatorQuickJS(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	vm, err := sandbox.Start(ctx, `await tools.leaf({});return "done";`, CodemodeStart{DrainAcknowledgements: true, Tools: []CodemodeToolDescription{{Name: "leaf"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	journal := codemodeJournalState{}
	commits := 0
	delivered := 0
	for {
		event, err := vm.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if event.Call != nil {
			journal.Issued = append(journal.Issued, *event.Call)
		}
		if event.Drain != nil {
			err := acknowledgeCodemodeDrain(ctx, vm.(CodemodeDrainExecution), &journal, *event.Drain, len(journal.Issued), delivered, func(context.Context, codemodeJournalState) error { commits++; return nil })
			if err != nil {
				t.Fatal(err)
			}
			if delivered == 0 {
				delivery := CodemodeDelivery{Seq: 1, Call: journal.Issued[0], HasValue: true, Value: "ok"}
				journal.Deliveries = append(journal.Deliveries, delivery)
				if err := vm.Deliver(ctx, delivery); err != nil {
					t.Fatal(err)
				}
				delivered++
			}
		}
		if event.Result != nil {
			if commits != 2 || journal.FirstIssued == nil || journal.Deliveries[0].IssuedAfter == nil {
				t.Fatal("missing acknowledged counts", commits, journal)
			}
			break
		}
	}
}
