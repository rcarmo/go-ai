package durable

import "context"

// A prepared publication owns detached observer work for one sealed transaction.
// Preparation runs on the admission line. Storage and publication belong to
// the writer; publication acquires only the memory lock, never the admission slot.
type preparedSessionPublication struct {
	ready           <-chan struct{}
	writerOwned     bool
	listenerFailure error
	ctx             context.Context
	tx              *Tx
	base            Snapshot
	frames          []preparedWatchFrame
	subs            []preparedSubscription
	stored          CommitPublication // actual durable writes, including selected holds
	observed        CommitPublication // this transaction's OWN changes, never folded holds
	storageWrites   []Write
	storedHeld      []ID
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
	p := preparedSessionPublication{ready: ready, ctx: ctx, tx: tx, base: base, frames: frames, stored: publication, observed: publication}
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
			var stored preparedStoredPublication
			stored, err = s.prepareHeldStoragePublication(ctx, p.storageWrites, tx.state.HighWater, ready)
			p.stored = stored.publication
			p.subs = append(p.subs, stored.subscriptions...)
		}
	}
	if err != nil {
		return preparedSessionPublication{}, err
	}
	return p, nil
}

// admittedPublication carries what one commit's publication produced. It is read
// only after the part's outcome closes, so the publication's write and the channel
// close give the commit its happens-before edge.
type admittedPublication struct {
	seq       uint64
	telemetry *durableEventBatch
}

// admitPreparedPublication is the admission handoff, ported from pinned v1.5.0
// src/session/session.ts: once preparation is done, nothing but Storage can fail,
// so the commit hands its sealed batch and write keys to the writer and the line
// goes on. Storage, then publication, run from the writer, and the commit's outcome
// is awaited OFF the line.
func (s *Session) admitPreparedPublication(p *preparedSessionPublication, index uint64, pub *admittedPublication, rollback func(error)) (*sessionWriterPart, error) {
	if err := s.failure(); err != nil {
		return nil, err
	}
	// Only the prepared baseline is compared. Held changes stored independently while
	// a callback runs can put the store image ahead of the read image, and native
	// rebases on the transaction's own state for that (see commitPersistence); the
	// publication's baseline is what must not have moved.
	if p.base.Seq != s.readImage.Seq {
		return nil, reject("stale prepared publication baseline")
	}
	keys, err := p.tx.writeKeys()
	if err != nil {
		return nil, err
	}
	writes := p.storageWrites
	if writes == nil {
		writes = p.tx.writes
	}
	p.writerOwned = true
	batch := Batch{Writes: writes}
	return s.writer.admit(sessionWriterSpec{
		index: index,
		batch: &batch,
		keys:  keys,
		ctx:   p.ctx,
		discard: func(failure error) {
			// Roll back provisional authority before the writer opens its gate.
			// This is memory bookkeeping, not a new #tail job: a reader may be
			// holding that admission slot while waiting for this very part.
			_ = s.line.acquire(context.Background())
			defer s.handoffLeave(nil)
			rollback(failure)
		},
		publish: func(r *commitReceipt) *writerPublishFailure {
			// The host publication scope holds memory across key unregistration
			// and this adoption. It bypasses fresh admission's Close fence because
			// an already-stored part must still be published.
			if err := s.failure(); err != nil {
				return &writerPublishFailure{adoption: true, err: err}
			}
			candidate := s.flushImages[r.seq]
			telemetry, err := s.publishSessionPublication(p, *r, candidate)
			if err != nil {
				return &writerPublishFailure{adoption: true, err: err}
			}
			pub.seq, pub.telemetry = r.seq, telemetry
			if p.listenerFailure != nil {
				return &writerPublishFailure{err: p.listenerFailure}
			}
			return nil
		},
	}), nil
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
	// Transaction.adopt(seq) assigns sequence-dependent metadata only once the
	// receipt exists. Several disjoint parts can prepare against the same image.
	if p.stored.Seq != receipt.seq {
		stampPublication(&p.observed, adopted, receipt.seq)
		stampPublication(&p.stored, adopted, receipt.seq)
		for i := range p.frames {
			p.frames[i].frame.Seq = receipt.seq
			p.frames[i].reset.Seq = receipt.seq
		}
		// Whole-snapshot fallback frames must contain the exact receipted prefix,
		// including earlier disjoint parts, never the old preparation image.
		p.tx.publicationState = &adopted
		var err error
		if p.storageWrites == nil {
			p.subs, err = s.prepareSubscriptions(p.ctx, p.observed, p.tx, p.ready)
		} else {
			p.subs, err = s.prepareSubscriptionKinds(p.ctx, p.observed, p.tx, p.ready, true, false)
			if err == nil {
				stored, e := s.prepareSubscriptionKinds(p.ctx, p.stored, p.tx, p.ready, false, true)
				p.subs = append(p.subs, stored...)
				err = e
			}
		}
		if err != nil {
			return nil, err
		}
	}
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
	// Source runs copied own listeners before the queued host callbacks. Do
	// not latch an own error until hosts see the already-adopted publication.
	listenerFailure := s.publishLocalCommit(p.ctx, "stored", p.observed)
	s.publishStoredCommit(p.ctx, p.stored)
	if listenerFailure != nil && p.writerOwned {
		p.listenerFailure = listenerFailure
	}
	if listenerFailure != nil && !p.writerOwned {
		_ = s.backendResult(p.ctx, listenerFailure, true)
	}
	return telemetryEvents, nil
}

// stampPublication preserves the detached payloads prepared before admission and
// fills only metadata assigned by storage, as Transaction.adopt(seq) does.
func stampPublication(p *CommitPublication, state Snapshot, seq uint64) {
	p.Seq = seq
	for i := range p.Tables {
		if entry := p.Tables[i].Entry; entry != nil {
			record := state.Entries[entry.ID]
			entry.Seq, entry.Position = record.Seq, record.Position
		}
	}
	for i := range p.Documents {
		record := state.Documents[p.Documents[i].Record.ID]
		p.Documents[i].Record.CreatedAt = record.CreatedAt
		p.Documents[i].Record.RetiredAt = record.RetiredAt
	}
}
