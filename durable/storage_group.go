package durable

import "context"

// These are internal control values, never durable JSON. Presence is the pointer
// in commitGroupResult: nil/null and undefined/falsy values are still refusals.
type commitUndefinedValue struct{}
type commitRefusal struct{ value any }

func (r *commitRefusal) Error() string { return "durable: batch refused" }
func (r *commitRefusal) Unwrap() error {
	err, _ := r.value.(error)
	return err
}

type commitGroupResult struct {
	stored  int
	lastSeq uint64
	refused *commitRefusal
}

// refusalError preserves legacy native diagnostics while representing a
// present null/undefined/falsy refusal with a non-nil error at Go boundaries.
func (r commitGroupResult) refusalError() error {
	if r.refused == nil {
		return nil
	}
	if original := r.refused.Unwrap(); original != nil {
		return original
	}
	return r.refused
}

type storageFrame struct {
	kind    byte
	ordinal uint64
	high    uint64
	payload []byte
}

// commitReceipt carries one detached encoded part, never a later raw image.
// Its payload is private immutable storage-owned data. Holding receipts retains
// encoded frames, not one full set of materialised tables per prefix.
type commitReceipt struct {
	index int
	seq   uint64
	frame storageFrame
}

// checkNext validates the receipt header against the published prefix. Both
// trusted native candidates and frame reconstruction must pass this check.
func (r commitReceipt) checkNext(base Snapshot) error {
	if r.index < 0 || base.Seq >= MaxID || r.seq != base.Seq+1 || r.frame.kind != 2 || r.frame.ordinal == 0 || r.frame.ordinal > MaxID || r.frame.high > MaxID {
		return reject("invalid publication receipt")
	}
	return nil
}

// nextImage reconstructs just this prefix without entering storage (receipt
// delivery owns the core gate). It leaves base unchanged. Replay intentionally
// reuses strict decoding/preparation until a separately verified trusted patch
// path can avoid that work; it does not retain all prepared prefix images.
func (r commitReceipt) nextImage(base Snapshot, l Limits) (Snapshot, error) {
	if err := r.checkNext(base); err != nil {
		return Snapshot{}, err
	}
	var record commitRecord
	if err := decodeStrict(r.frame.payload, l, l.MaxFramePayloadBytes, &record); err != nil {
		return Snapshot{}, err
	}
	if record.Seq != r.seq {
		return Snapshot{}, reject("publication receipt sequence mismatch")
	}
	if r.frame.high > base.HighWater {
		base.HighWater = r.frame.high
	}
	return prepare(base, record, l)
}

// prepareCommit detaches input and validates against an immutable candidate.
// It neither writes nor changes the live image/allocator/ordinal.
func (c *storeCore) prepareCommit(base Snapshot, b Batch, ordinal uint64) (storageFrame, Snapshot, error) {
	if base.Seq >= MaxID || ordinal >= MaxID {
		return storageFrame{}, Snapshot{}, reject("sequence exhausted")
	}
	if err := validateBatchJSON(b, c.limits); err != nil {
		return storageFrame{}, Snapshot{}, err
	}
	payload, err := encodeBounded(commitRecord{Seq: base.Seq + 1, Writes: b.Writes}, c.limits, c.limits.MaxFramePayloadBytes)
	if err != nil {
		return storageFrame{}, Snapshot{}, err
	}
	var record commitRecord
	if err := decodeStrict(payload, c.limits, c.limits.MaxFramePayloadBytes, &record); err != nil {
		return storageFrame{}, Snapshot{}, err
	}
	next, err := prepare(base, record, c.limits)
	if err != nil {
		return storageFrame{}, Snapshot{}, err
	}
	return storageFrame{kind: 2, ordinal: ordinal + 1, high: base.HighWater, payload: payload}, next, nil
}

// commitBatches owns one storage admission through all settlement/receipts. The
// receipt sink is internal, synchronous and must not reenter storage. Its error
// rejects the call without undoing durability or earlier receipts. No Session
// publication or optional-interface dispatch is performed here.
//
// SQLite can settle the prepared prefix atomically. Memory/journal settle one
// batch at a time; an error preserves their confirmed prefix. Total encoded
// payload is bounded by the existing per-call frame cap, and batch count by
// MaxWrites. Once admitted, cancellation cannot abandon settlement.
func (c *storeCore) commitBatches(ctx context.Context, batches []Batch, receive func(int, uint64) error, owned bool) (commitGroupResult, error) {
	var sink func(commitReceipt) error
	if receive != nil {
		sink = func(receipt commitReceipt) error { return receive(receipt.index, receipt.seq) }
	}
	return c.commitParts(ctx, batches, sink, owned)
}

// commitParts is the frame-bearing internal settlement path. Legacy sinks keep
// their index/seq contract through commitBatches; neither path is exported or
// dispatched through wrappers. Callers must treat receipt payloads as immutable.
func (c *storeCore) commitParts(ctx context.Context, batches []Batch, receive func(commitReceipt) error, owned bool) (commitGroupResult, error) {
	return c.commitPartsAt(ctx, batches, receive, owned, nil)
}

// commitPartsAt checks a prepared caller's global baseline while owning core
// admission, before preparation or persistence. Nil retains raw storage Apply
// semantics. This is a conservative barrier, not keyed/disjoint admission.
func (c *storeCore) commitPartsAt(ctx context.Context, batches []Batch, receive func(commitReceipt) error, owned bool, expectedSeq *uint64) (commitGroupResult, error) {
	if err := c.enter(ctx); err != nil {
		return commitGroupResult{}, err
	}
	defer c.leave()
	if c.sessionOwned && !owned {
		return commitGroupResult{}, ErrOwned
	}
	if expectedSeq != nil && c.state.Seq != *expectedSeq {
		return commitGroupResult{}, reject("stale prepared storage baseline")
	}
	result := commitGroupResult{lastSeq: c.state.Seq}
	if len(batches) > c.limits.MaxWrites {
		result.refused = &commitRefusal{reject("invalid batch count")}
		return result, nil
	}
	if len(batches) == 0 {
		return result, nil
	}
	atomicGroup := c.appendFrames != nil && len(batches) > 1
	base, ordinal := c.state, c.ordinal
	var frames []storageFrame
	var bytes int
	for index, batch := range batches {
		frame, next, err := c.prepareCommit(base, batch, ordinal)
		if err == nil && len(frame.payload) > c.limits.MaxFramePayloadBytes-bytes {
			err = reject("group payload limit exceeded")
		}
		if err != nil {
			result.refused = &commitRefusal{err}
			break
		}
		bytes += len(frame.payload)
		if atomicGroup {
			frames = append(frames, frame)
		} else {
			if err := c.settle(frame.kind, frame.high, frame.payload); err != nil {
				return commitGroupResult{}, err
			}
			c.state = next
			if receive != nil {
				if err := receive(commitReceipt{index: index, seq: next.Seq, frame: frame}); err != nil {
					return commitGroupResult{}, err
				}
			}
		}
		base, ordinal = next, frame.ordinal
		result.stored++
		result.lastSeq = next.Seq
	}
	if atomicGroup && len(frames) != 0 {
		if err := c.appendFrames(frames); err != nil {
			return commitGroupResult{}, c.settlementError(err)
		}
		c.state, c.ordinal = base, ordinal
		if receive != nil {
			firstSeq := base.Seq - uint64(len(frames)) + 1
			for index := range frames {
				if err := receive(commitReceipt{index: index, seq: firstSeq + uint64(index), frame: frames[index]}); err != nil {
					return commitGroupResult{}, err
				}
			}
		}
	}
	return result, nil
}
