package durable

import "context"

// settleBatch is the current serial writer. Capture its exact immutable native
// candidate inside receipt delivery, while the core gate still protects it.
// Append hooks, owned admission and legacy validation/refusal errors are shared
// with Apply; no wrapper optional-interface dispatch is introduced.
func (s *Session) settleBatch(ctx context.Context, batch Batch) (commitReceipt, Snapshot, error) {
	var receipt commitReceipt
	var candidate Snapshot
	received := 0
	result, err := s.core.commitParts(ctx, []Batch{batch}, func(r commitReceipt) error {
		received++
		if received != 1 || r.index != 0 || r.seq != s.core.state.Seq {
			return reject("invalid serial receipt")
		}
		receipt, candidate = r, s.core.state
		return nil
	}, true)
	if err != nil {
		return commitReceipt{}, Snapshot{}, err
	}
	if err := result.refusalError(); err != nil {
		return commitReceipt{}, Snapshot{}, err
	}
	if received != 1 || result.stored != 1 || result.lastSeq != receipt.seq {
		return commitReceipt{}, Snapshot{}, reject("missing serial receipt")
	}
	return receipt, candidate, nil
}

// adoptReceipt runs on the Session line. The optional candidate is trusted only
// when captured from native storage at this receipt's exact sequence. Ordinary
// serial commits use that candidate without decoding/revalidating. An earlier
// group part reconstructs from its detached frame instead. A final group image
// passed for an earlier receipt rejects; it is never silently substituted.
func (s *Session) adoptReceipt(receipt commitReceipt, candidate *Snapshot) error {
	if err := receipt.checkNext(s.readImage); err != nil {
		return err
	}
	var image Snapshot
	if candidate != nil {
		if candidate.Seq != receipt.seq {
			return reject("publication candidate sequence mismatch")
		}
		image = *candidate
	} else {
		var err error
		image, err = receipt.nextImage(s.readImage, s.limits)
		if err != nil {
			return err
		}
	}
	return s.adoptReadImage(image, receipt.seq)
}
