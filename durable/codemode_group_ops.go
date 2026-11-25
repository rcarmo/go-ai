package durable

import (
	"context"
	"fmt"
)

// A ready queue owns completions taken from the scheduler, including a result
// that did not fit in the proposed commit. It never waits to fill a batch.
type codemodeGroupReady struct {
	pending []codemodeCompletedCall
}

func (q *codemodeGroupReady) collect(ready <-chan codemodeCompletedCall) {
	for len(q.pending) < codemodeJournalBatchLimit {
		select {
		case completed := <-ready:
			q.pending = append(q.pending, completed)
			if completed.err != nil {
				return
			}
		default:
			return
		}
	}
}

func (q *codemodeGroupReady) candidate(base codemodeJournalState, covered, maximum int) (*codemodeJournalState, int) {
	var n int
	for _, result := range q.pending {
		if n == maximum || result.err != nil || result.delivery.Call.ID < 1 || result.delivery.Call.ID > covered {
			break
		}
		n++
	}
	if n == 0 {
		return nil, 0
	}
	next := base
	next.Deliveries = append([]CodemodeDelivery(nil), base.Deliveries...)
	for _, result := range q.pending[:n] {
		next.Deliveries = append(next.Deliveries, nextParallelCodemodeDelivery(next, result.delivery))
	}
	next.BatchEnds = append(append([]int(nil), base.BatchEnds...), len(next.Deliveries))
	return &next, n
}

func (q *codemodeGroupReady) consume(n int) {
	clear(q.pending[:n])
	q.pending = q.pending[n:]
}

// Return the durable count immediately before this drain, not a count from a
// later stored result. A transport ACK may use existing coverage but cannot
// authorize a callback on its own.
func codemodeCoverageBefore(journal codemodeJournalState, delivered int) int {
	for i := delivered - 2; i >= 0; i-- {
		if count := journal.Deliveries[i].IssuedAfter; count != nil {
			return *count
		}
	}
	if journal.FirstIssued != nil {
		return *journal.FirstIssued
	}
	return 0
}

func settleGroupedCodemodeDrain(ctx context.Context, vm CodemodeDrainExecution, start CodemodeStart, journal *codemodeJournalState, drain CodemodeDrain, observed, delivered int, queue *codemodeGroupReady, ready <-chan codemodeCompletedCall, commit func(context.Context, codemodeJournalState, *codemodeJournalState) (bool, error)) (*codemodeTerminal, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	next, err := prepareCodemodeDrain(journal, drain, observed, delivered)
	if err != nil {
		return nil, err
	}
	if drain.Final {
		if observed != len(journal.Issued) {
			return nil, reject("codemode drain ended before saved calls")
		}
		terminal := &codemodeTerminal{Delivered: delivered, Issued: observed, Committed: len(journal.Deliveries)}
		if err := validateGroupedCodemodeJournal(start, *journal, terminal, true); err != nil {
			return nil, fmt.Errorf("codemode drain terminal position: %w", err)
		}
		// No call may launch after this position. Save it atomically with the
		// result; a cutoff before that write can replay the committed group.
		return terminal, vm.AcknowledgeDrain(ctx, drain)
	}
	covered := codemodeCoverageBefore(*journal, delivered)
	if delivered < len(journal.Deliveries) {
		if id := journal.Deliveries[delivered].Call.ID; id > observed {
			return nil, reject(fmt.Sprintf("codemode drain replay count divergence: it waits without making call %d, which it made before", id))
		}
		if observed <= covered {
			return nil, vm.AcknowledgeDrain(ctx, drain)
		}
	}
	if next != nil {
		var candidate *codemodeJournalState
		var n int
		if ready != nil {
			queue.collect(ready)
			candidate, n = queue.candidate(*next, covered, codemodeJournalBatchLimit)
		}
		fused, err := commit(ctx, *next, candidate)
		if err != nil {
			return nil, err
		}
		if fused {
			next = candidate
			queue.consume(n)
		}
		*journal = *next
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, vm.AcknowledgeDrain(ctx, drain)
}
