package durable

import "context"

// acknowledgeCodemodeDrain commits a new count or checks a recorded one before
// releasing the VM. observed and delivered refer to this execution, not the
// entire persisted journal (which may contain a suffix still to replay).
func acknowledgeCodemodeDrain(ctx context.Context, vm CodemodeDrainExecution, journal *codemodeJournalState, drain CodemodeDrain, observed, delivered int, commit func(context.Context, codemodeJournalState) error) error {
	if ctx == nil || vm == nil || journal == nil || commit == nil {
		return reject("invalid codemode drain coordinator")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if drain.Issued < 0 || drain.Issued != observed || observed > len(journal.Issued) || drain.DeliveryID < 0 {
		return reject("codemode drain issued count divergence")
	}
	var recorded *int
	deliveryIndex := -1
	if delivered == 0 {
		if drain.DeliveryID != 0 {
			return reject("codemode initial drain identity mismatch")
		}
		recorded = journal.FirstIssued
	} else {
		if journal.FirstIssued == nil || delivered < 1 || delivered > len(journal.Deliveries) {
			return reject("codemode drain delivery missing")
		}
		deliveryIndex = delivered - 1
		if journal.Deliveries[deliveryIndex].Call.ID != drain.DeliveryID {
			return reject("codemode drain delivery identity mismatch")
		}
		recorded = journal.Deliveries[deliveryIndex].IssuedAfter
	}
	if recorded != nil {
		if *recorded != observed {
			return reject("codemode drain replay count divergence")
		}
	} else {
		// Work on a detached slice: a failed transaction must not adopt its count.
		next := *journal
		next.Deliveries = append([]CodemodeDelivery(nil), journal.Deliveries...)
		count := observed
		if deliveryIndex < 0 {
			next.FirstIssued = &count
		} else {
			next.Deliveries[deliveryIndex].IssuedAfter = &count
		}
		if err := validateCodemodeWatermarks(CodemodeStart{DrainAcknowledgements: true}, next, false); err != nil {
			return err
		}
		if err := commit(ctx, next); err != nil {
			return err
		}
		*journal = next
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return vm.AcknowledgeDrain(ctx, drain)
}
