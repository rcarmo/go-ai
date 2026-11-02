package durable

import "context"

// A prepared publication owns detached observer work for one sealed transaction.
// Preparation, settlement and publication still run on the same Session line.
// Keeping the stages explicit does not enable overlap or relax wrapper gates.
type preparedSessionPublication struct {
	ctx           context.Context
	tx            *Tx
	base          Snapshot
	frames        []preparedWatchFrame
	subs          []preparedSubscription
	storageWrites []Write
	storedHeld    []ID
}

func (s *Session) prepareSessionPublication(ctx context.Context, tx *Tx, base Snapshot, ready <-chan struct{}) (preparedSessionPublication, error) {
	publication, err := tx.preparePublication(ctx)
	if err != nil {
		return preparedSessionPublication{}, err
	}
	if tx.memoryOnly {
		// No durable sequence is consumed by this memory publication.
		publication.Seq = base.Seq
	}
	frames, err := s.prepareWatchFrames(ctx, publication, ready)
	if err != nil {
		return preparedSessionPublication{}, err
	}
	p := preparedSessionPublication{ctx: ctx, tx: tx, base: base, frames: frames}
	if tx.memoryOnly {
		p.subs, err = s.prepareSubscriptionKinds(ctx, publication, tx, ready, true, false)
	} else if len(s.heldDocuments) == 0 {
		p.subs, err = s.prepareSubscriptions(ctx, publication, tx, ready)
	} else {
		p.storageWrites, p.storedHeld, err = s.writesWithHeld(tx)
		if err != nil {
			return preparedSessionPublication{}, err
		}
		p.subs, err = s.prepareSubscriptionKinds(ctx, publication, tx, ready, true, false)
		if err == nil {
			var stored []preparedSubscription
			stored, err = s.prepareHeldStorageSubscriptions(ctx, p.storageWrites, tx.state.HighWater, ready)
			p.subs = append(p.subs, stored...)
		}
	}
	if err != nil {
		return preparedSessionPublication{}, err
	}
	return p, nil
}

// settlePreparedPublication retains a global dependency on the preparation
// baseline. Finalisation currently scans ownership/task/document state, so a
// narrower footprint is not yet safe. The writer independently checks the raw
// baseline inside its gate to exclude stored-but-unpublished changes.
func (s *Session) settlePreparedPublication(p *preparedSessionPublication) (commitReceipt, Snapshot, error) {
	if err := s.failure(); err != nil {
		return commitReceipt{}, Snapshot{}, err
	}
	if p.base.Seq != s.readImage.Seq {
		return commitReceipt{}, Snapshot{}, reject("stale prepared publication baseline")
	}
	writes := p.storageWrites
	if writes == nil {
		writes = p.tx.writes
	}
	return s.settleBatch(p.ctx, Batch{Writes: writes}, p.base.Seq)
}

// publishSessionPublication owns the line and rechecks the first fatal cause.
// Cancellation after admitted storage never abandons the stored publication.
// Receipted adoption must precede retention, task/trace effects and observers.
func (s *Session) publishSessionPublication(p *preparedSessionPublication, receipt commitReceipt, candidate *Snapshot) (*durableEventBatch, error) {
	if err := s.failure(); err != nil {
		return nil, err
	}
	if err := s.adoptReceipt(receipt, candidate); err != nil {
		return nil, err
	}
	for _, id := range p.storedHeld {
		delete(s.heldDocuments, id)
	}
	s.armPersistenceDeadline()
	s.memoryImage = nil
	adopted := s.memoryReadImage()
	if s.taskScheduler != nil {
		s.taskScheduler.adoptIdleImage(adopted, p.tx.writes)
	}
	var telemetryEvents *durableEventBatch
	s.invalidateContextRanges(adopted, p.tx.writes)
	// Settle retention even without a timer callback. Entry-only writes
	// cannot change task membership, so avoid rescanning tasks for them.
	refreshContextScopes := false
	for _, write := range p.tx.writes {
		// Live documents can change held-generation idleness too. Only a
		// strictly entry-only batch can reuse the previous busy witnesses.
		refreshContextScopes = refreshContextScopes || write.Op != "append-entry"
	}
	s.settleContextRanges(refreshContextScopes)
	if s.taskScheduler != nil {
		// Storage already validated and adopted the immutable candidate. The
		// Session owns its sole writer; scheduler bookkeeping reads this private
		// view only. Do not replay/validate the entire history a second time.
		p.tx.taskAdoptState = &adopted
	}
	if s.taskScheduler != nil && s.taskScheduler.h.options.Telemetry != nil {
		// Capture links before terminal placeholder writes release their parents.
		telemetryEvents = s.taskScheduler.h.captureCommitEvents(p.ctx, p.tx, p.base)
		s.taskScheduler.h.noteTaskAncestry(p.ctx, p.tx, p.base)
		for _, write := range p.tx.writes {
			if write.Task != nil {
				if runtime := s.taskScheduler.invocations[write.Task.ID]; runtime != nil {
					runtime.noteTrace(*write.Task)
				}
			}
		}
	}
	if trace := submitTraceFromContext(p.ctx); trace != nil {
		for _, write := range p.tx.writes {
			if write.Submission != nil {
				trace.observe(*write.Submission)
			}
		}
	}
	s.adoptTaskEffects(receipt.seq, p.tx)
	s.enqueueWatchFrames(p.frames)
	s.enqueueSubscriptions(p.subs)
	// Only changed incarnations invalidate migration cache entries. An
	// unrelated commit never turns a warm migration into a cold load.
	for _, write := range p.tx.writes {
		var changed ID
		if write.Document != nil {
			changed = write.Document.ID
		}
		if write.Delta != nil {
			changed = write.Delta.ID
		}
		if changed == 0 {
			continue
		}
		for key, value := range s.definitionCache {
			if key.id == changed {
				delete(s.definitionCache, key)
				s.definitionCacheBytes -= value.bytes
			}
		}
	}
	// Background expiry is optional even if the host declines its timer.
	s.sweepDocumentCache()
	s.scheduleDocumentExpiry()
	return telemetryEvents, nil
}
