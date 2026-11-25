package durable

import (
	"context"
	"fmt"
)

// acknowledgeCodemodeDrain commits a new count or checks a recorded one before
// releasing the VM. observed and delivered refer to this execution, not the
// entire persisted journal (which may contain a suffix still to replay).
func acknowledgeCodemodeDrain(ctx context.Context, vm CodemodeDrainExecution, journal *codemodeJournalState, drain CodemodeDrain, observed, delivered int, commit func(context.Context, codemodeJournalState) error) error {
	if ctx == nil || vm == nil || journal == nil || commit == nil {
		return reject("invalid codemode drain coordinator")
	}
	_, err := settleCodemodeDrain(ctx, vm, journal, drain, observed, delivered, nil, func(ctx context.Context, next codemodeJournalState, _ *codemodeJournalState) (bool, error) {
		return false, commit(ctx, next)
	})
	return err
}

// checkCodemodeDrain verifies only the frame and a saved observation. A
// rejected replay can compare counts without adopting any of its calls.
func checkCodemodeDrain(journal *codemodeJournalState, drain CodemodeDrain, observed, delivered int) (int, *int, error) {
	if drain.Issued < 0 || drain.Issued != observed || drain.DeliveryID < 0 {
		return -1, nil, reject("codemode drain issued count divergence")
	}
	var recorded *int
	deliveryIndex := -1
	if delivered == 0 {
		if drain.DeliveryID != 0 {
			return -1, nil, reject("codemode initial drain identity mismatch")
		}
		recorded = journal.FirstIssued
	} else {
		if journal.FirstIssued == nil || delivered < 1 || delivered > len(journal.Deliveries) {
			return -1, nil, reject("codemode drain delivery missing")
		}
		deliveryIndex = delivered - 1
		if journal.Deliveries[deliveryIndex].Call.ID != drain.DeliveryID {
			return -1, nil, reject("codemode drain delivery identity mismatch")
		}
		recorded = journal.Deliveries[deliveryIndex].IssuedAfter
	}
	if recorded != nil {
		if *recorded != observed {
			const reason = "codemode drain replay count divergence"
			if !drain.Final && delivered < len(journal.Deliveries) {
				if next := journal.Deliveries[delivered].Call.ID; next > observed {
					return -1, nil, reject(fmt.Sprintf("%s: it waits without making call %d, which it made before", reason, next))
				}
			}
			seq := 0
			if deliveryIndex >= 0 {
				seq = journal.Deliveries[deliveryIndex].Seq
			}
			if drain.Final && observed < *recorded {
				return -1, nil, reject(fmt.Sprintf("%s: it ended after result %d, where it went on before", reason, seq))
			}
			if deliveryIndex >= 0 {
				return -1, nil, reject(fmt.Sprintf("%s: after result %d it had made %d calls, where it had made %d", reason, seq, observed, *recorded))
			}
			return -1, nil, reject(fmt.Sprintf("%s: its first run made %d calls, where it had made %d", reason, observed, *recorded))
		}
	}
	return deliveryIndex, recorded, nil
}

// prepareCodemodeDrain checks replay counts or returns a detached journal with
// the newly observed count, without publishing or releasing the transport.
func prepareCodemodeDrain(journal *codemodeJournalState, drain CodemodeDrain, observed, delivered int) (*codemodeJournalState, error) {
	if observed > len(journal.Issued) {
		return nil, reject("codemode drain issued count divergence")
	}
	deliveryIndex, recorded, err := checkCodemodeDrain(journal, drain, observed, delivered)
	if err != nil {
		return nil, err
	}
	if recorded != nil {
		return nil, nil
	}
	next := *journal
	next.Deliveries = append([]CodemodeDelivery(nil), journal.Deliveries...)
	count := observed
	if deliveryIndex < 0 {
		next.FirstIssued = &count
	} else {
		next.Deliveries[deliveryIndex].IssuedAfter = &count
	}
	start := CodemodeStart{DrainAcknowledgements: true}
	if next.Version != 0 {
		start.JournalVersion, start.ParallelCalls = next.Version, true
		if err := validateGroupedCodemodeJournal(start, next, nil, false); err != nil {
			return nil, err
		}
	} else if err := validateCodemodeWatermarks(start, next, false); err != nil {
		return nil, err
	}
	return &next, nil
}

// settleCodemodeDrain may publish one covered outcome with the prior
// live drain. ready is nil unless all observed calls are already admitted and
// this is a non-final live drain. A completion that cannot be fused remains
// next in order; an empty queue never delays the drain.
func settleCodemodeDrain(ctx context.Context, vm CodemodeDrainExecution, journal *codemodeJournalState, drain CodemodeDrain, observed, delivered int, ready <-chan codemodeCompletedCall, commit func(context.Context, codemodeJournalState, *codemodeJournalState) (bool, error)) (*codemodeCompletedCall, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	next, err := prepareCodemodeDrain(journal, drain, observed, delivered)
	if err != nil {
		return nil, err
	}
	var held *codemodeCompletedCall
	if next != nil {
		var withDelivery *codemodeJournalState
		select {
		case completion := <-ready:
			held = &completion
			if completion.err == nil && completion.delivery.Call.ID > 0 && completion.delivery.Call.ID <= observed {
				delivery := nextParallelCodemodeDelivery(*next, completion.delivery)
				candidate := *next
				candidate.Deliveries = append(next.Deliveries, delivery)
				withDelivery = &candidate
			}
		default:
		}
		fused, err := commit(ctx, *next, withDelivery)
		if err != nil {
			return nil, err
		}
		if fused {
			next, held = withDelivery, nil
		}
		*journal = *next
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return held, vm.AcknowledgeDrain(ctx, drain)
}

func nextParallelCodemodeDelivery(journal codemodeJournalState, delivery CodemodeDelivery) CodemodeDelivery {
	delivery.Seq = len(journal.Deliveries) + 1
	if last := len(journal.Deliveries) - 1; last >= 0 && delivery.Now < journal.Deliveries[last].Now {
		delivery.Now = journal.Deliveries[last].Now
	}
	return delivery
}
