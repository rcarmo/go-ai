package durable

import (
	"context"
	"math"
	"time"
)

// heldDocument owns a bounded fold for one existing latest/initial document.
// Its content is visible in memory; readImage and its history remain receipted.
type heldDocument struct {
	record   Document
	ops      []Operation
	base     bool
	deadline float64
}

// commitDeferred preserves the internal infinite-window entry. Explicit/default
// windows use commitPersistence; Harness wiring awaits the lifecycle gates.
func (s *Session) commitDeferred(ctx context.Context, callback func(*Tx) error) (uint64, error) {
	return s.commitPersistence(ctx, callback, true, 0, true, math.Inf(1))
}

func (s *Session) memoryReadImage() Snapshot {
	if len(s.heldDocuments) == 0 {
		return s.readImage
	}
	if s.memoryImage == nil {
		image := s.readImage
		image.Documents = make(map[ID]Document, len(s.readImage.Documents))
		for id, document := range s.readImage.Documents {
			image.Documents[id] = document
		}
		for id, held := range s.heldDocuments {
			image.Documents[id] = held.record
		}
		s.memoryImage = &image
	}
	return *s.memoryImage
}

func documentWriteID(write Write) ID {
	if write.Document != nil {
		return write.Document.ID
	}
	if write.Delta != nil {
		return write.Delta.ID
	}
	return 0
}

func (s *Session) canHoldDocuments(tx *Tx) bool {
	for _, write := range tx.writes {
		if write.Op != "put-document" && write.Op != "delta-document" {
			return false
		}
		id := documentWriteID(write)
		before, exists := s.readImage.Documents[id]
		after := tx.state.Documents[id]
		if !exists || before.Retired || before.Scope != "conversation" || documentHistory(before) != "latest" || documentFork(before) != "initial" {
			return false
		}
		if write.Document != nil {
			after = *write.Document
		}
		if write.Delta != nil && write.Delta.Version != before.Version {
			return false
		}
		if after.Version != before.Version || after.Scope != "conversation" || documentHistory(after) != "latest" || documentFork(after) != "initial" {
			return false
		}
	}
	return len(tx.writes) > 0
}

// Fold operations without weakening the native operation/value budgets. An
// overflowing operation tail becomes an already validated whole-value base.
func (s *Session) extendHeld(held heldDocument, writes []Write) (heldDocument, error) {
	for _, write := range writes {
		if documentWriteID(write) != held.record.ID {
			continue
		}
		switch write.Op {
		case "put-document":
			held.record, held.base, held.ops = *write.Document, true, nil
		case "delta-document":
			value, err := ApplyOperations(held.record.Value, write.Delta.Ops, s.limits)
			if err != nil {
				return heldDocument{}, err
			}
			object, ok := value.(map[string]any)
			if !ok {
				return heldDocument{}, reject("held document root must be object")
			}
			held.record.Value = JSON(object)
			if !held.base {
				combined, overflow, err := foldWatchOperations([][]Operation{held.ops, write.Delta.Ops}, s.limits)
				if err != nil {
					return heldDocument{}, err
				}
				owned, err := ownOperations(combined, s.limits)
				if overflow || err != nil {
					held.base, held.ops = true, nil
				} else {
					held.ops = owned
				}
			}
		}
	}
	return held, nil
}

func heldWrite(held heldDocument) Write {
	if held.base {
		document := held.record
		document.DeltasSinceBase = 0
		return Write{Op: "put-document", Document: &document}
	}
	return Write{Op: "delta-document", Delta: &DocumentDelta{ID: held.record.ID, Version: held.record.Version, Ops: held.ops}}
}

func (s *Session) holdPreparedDocuments(p *preparedSessionPublication, withinMs float64) error {
	deadline := float64(s.now()) + withinMs
	next := make(map[ID]heldDocument, len(s.heldDocuments)+len(p.tx.writes))
	for id, held := range s.heldDocuments {
		next[id] = held
	}
	changed := map[ID]bool{}
	for _, write := range p.tx.writes {
		changed[documentWriteID(write)] = true
	}
	for id := range changed {
		held, exists := next[id]
		if !exists {
			held.record = p.tx.state.Documents[id]
			held.deadline = math.Inf(1)
		}
		if deadline < held.deadline {
			held.deadline = deadline
		}
		var err error
		held, err = s.extendHeld(held, p.tx.writes)
		if err != nil {
			return err
		}
		// The count excludes uncommitted changes, even after many memory commits.
		held.record.DeltasSinceBase = s.readImage.Documents[id].DeltasSinceBase
		next[id] = held
	}
	writes := make([]Write, 0, len(next))
	retained := make([]struct {
		Record Document
		Ops    []Operation
	}, 0, len(next))
	for _, id := range ids(next) {
		held := next[id]
		writes = append(writes, heldWrite(held))
		retained = append(retained, struct {
			Record Document
			Ops    []Operation
		}{held.record, held.ops})
	}
	if _, err := encodeBounded(Batch{Writes: writes}, s.limits, s.limits.MaxFramePayloadBytes); err != nil {
		return err
	}
	if _, err := encodeBounded(struct{ Held any }{retained}, s.limits, s.limits.MaxRetainedBytesAsInt()); err != nil {
		return err
	}
	if err := p.ctx.Err(); err != nil {
		return err
	}
	if err := s.failure(); err != nil {
		return err
	}
	s.heldDocuments, s.memoryImage = next, nil
	s.armPersistenceDeadline()
	// An equal durable sequence cannot witness a cached/off-line memory read.
	s.contextRevision++
	s.invalidateContextRanges(s.memoryReadImage(), p.tx.writes)
	for key, cached := range s.definitionCache {
		if changed[key.id] {
			delete(s.definitionCache, key)
			s.definitionCacheBytes -= cached.bytes
		}
	}
	s.enqueueWatchFrames(p.frames)
	s.enqueueSubscriptions(p.subs)
	if listenerFailure := s.publishLocalCommit(p.ctx, "memory", p.observed); listenerFailure != nil {
		// Memory adoption stands; writer listener failures resolve this call
		// before closing/dropping the failed Session's unpersisted holds.
		_ = s.backendResult(p.ctx, listenerFailure, true)
	}
	return nil
}

// Select only held documents actually accessed by a writing transaction. A
// write-free transaction returned before publication preparation and stays held.
func (s *Session) writesWithHeld(tx *Tx) ([]Write, []ID, error) {
	selected := map[ID]bool{}
	for id := range tx.readDocuments {
		if _, ok := s.heldDocuments[id]; ok {
			selected[id] = true
		}
	}
	for id := range tx.documentPlans {
		if _, ok := s.heldDocuments[id]; ok {
			selected[id] = true
		}
	}
	for _, write := range tx.writes {
		if id := documentWriteID(write); id != 0 {
			if _, ok := s.heldDocuments[id]; ok {
				selected[id] = true
			}
		}
		if write.Source != nil && write.Source.At.Current {
			if _, ok := s.heldDocuments[write.Source.ID]; ok {
				selected[write.Source.ID] = true
			}
		}
	}
	return s.foldSelectedHeld(tx.writes, selected)
}

func (s *Session) foldSelectedHeld(own []Write, selected map[ID]bool) ([]Write, []ID, error) {
	writes := make([]Write, 0, len(own)+len(selected))
	stored := ids(selected)
	for _, id := range stored {
		held, err := s.extendHeld(s.heldDocuments[id], own)
		if err != nil {
			return nil, nil, err
		}
		writes = append(writes, heldWrite(held))
	}
	for _, write := range own {
		if id := documentWriteID(write); selected[id] && (write.Op == "put-document" || write.Op == "delta-document") {
			continue
		}
		writes = append(writes, write)
	}
	return writes, stored, nil
}

type preparedStoredPublication struct {
	publication   CommitPublication
	subscriptions []preparedSubscription
}

func (s *Session) prepareHeldStoragePublication(ctx context.Context, writes []Write, highWater uint64, ready <-chan struct{}) (preparedStoredPublication, error) {
	state := s.readImage
	// Reservations are durable independently of document commits. Include IDs
	// minted in this callback without mutating the prior receipted image.
	state.HighWater = highWater
	tx := &Tx{state: state, limits: s.limits, writes: writes}
	for _, write := range writes {
		if write.Delta != nil {
			plan := tx.documentPlan(write.Delta.ID)
			plan.ops = append(plan.ops, write.Delta.Ops...)
		}
	}
	publication, err := tx.preparePublication(ctx)
	if err != nil {
		return preparedStoredPublication{}, err
	}
	subscriptions, err := s.prepareSubscriptionKinds(ctx, publication, tx, ready, false, true)
	return preparedStoredPublication{publication: publication, subscriptions: subscriptions}, err
}

func (s *Session) persistenceWindow(requested []float64) (float64, error) {
	if len(requested) > 1 {
		return 0, reject("invalid persistence option count")
	}
	if len(requested) == 1 {
		if !(requested[0] >= 0) {
			return 0, reject("CommitPersistence.withinMs must be a number of milliseconds, 0 or more")
		}
		return requested[0], nil
	}
	if s.persistWithinMs != nil {
		if within := s.persistWithinMs(); within >= 0 {
			return within, nil
		}
	}
	return 1000, nil
}

// A fired foreground timer stays selected until its serialized job runs. The
// callback only queues work; the next admission drains it before its own work.
func (s *Session) armPersistenceDeadline() {
	if s.closing.Load() {
		return
	}
	at := math.Inf(1)
	for _, held := range s.heldDocuments {
		if held.deadline < at {
			at = held.deadline
		}
	}
	if armed := s.persistenceTimer; armed != nil {
		if armed.fired.Load() || armed.at == at {
			return
		}
		armed.Stop()
		s.persistenceTimer = nil
	}
	if math.IsInf(at, 1) {
		return
	}
	left := math.Max(0, at-float64(s.now()))
	delay := time.Duration(math.MaxInt64)
	if left < float64(math.MaxInt64/int64(time.Millisecond)) {
		delay = time.Duration(left * float64(time.Millisecond))
	}
	var timer *sessionTimer
	timer = s.newSessionTimer(persistenceDeadlineTimer, delay, func() {
		if !timer.fired.CompareAndSwap(false, true) {
			return
		}
		go func() {
			if s.enter(context.Background()) == nil {
				s.leave()
			}
		}()
	})
	timer.at = at
	s.persistenceTimer = timer
}

func (s *Session) flushFiredPersistence() error {
	timer := s.persistenceTimer
	if timer == nil || !timer.fired.Load() {
		return nil
	}
	s.persistenceTimer = nil
	timer.Stop()
	if len(s.heldDocuments) == 0 {
		return nil
	}
	ctx := context.Background()
	if err := s.storeHeldDocuments(ctx, ids(s.heldDocuments)); err != nil {
		return s.backendResult(ctx, err, true)
	}
	return nil
}

// The Session line owns selection, preparation, settlement and adoption. Only
// stored observers receive this publication: memory already contains the value.
func (s *Session) storeHeldDocuments(ctx context.Context, selected []ID) error {
	if len(selected) == 0 {
		return nil
	}
	if !s.hasNativeReadImage() || s.taskScheduler != nil {
		return ErrUnsupported
	}
	writes := make([]Write, 0, len(selected))
	for _, id := range selected {
		writes = append(writes, heldWrite(s.heldDocuments[id]))
	}
	// This independent maintenance job owns the line. Once admitted, caller
	// cancellation cannot abandon its storage/receipt; retain caller trace data.
	storageContext := context.WithoutCancel(ctx)
	highWater, err := s.core.sessionReadAdmission(storageContext)
	if err != nil {
		return err
	}
	ready := s.persistenceReady
	if ready == nil {
		ready = make(chan struct{})
	}
	stored, err := s.prepareHeldStoragePublication(ctx, writes, highWater, ready)
	if err != nil {
		return err
	}
	receipt, candidate, err := s.settleBatch(storageContext, Batch{Writes: writes}, s.readImage.Seq)
	if err != nil {
		return err
	}
	if err := s.failure(); err != nil {
		return err
	}
	if err := s.adoptReceipt(receipt, &candidate); err != nil {
		return err
	}
	for _, id := range selected {
		delete(s.heldDocuments, id)
	}
	s.memoryImage = nil
	s.armPersistenceDeadline()
	s.enqueueSubscriptions(stored.subscriptions)
	s.persistenceReady = ready
	s.publishStoredCommit(ctx, stored.publication)
	return nil
}

// A different-version token reloads from storage, never from an unstored hold.
// Run before migration/check callbacks; failure there cannot undo this barrier.
func (s *Session) reloadHeldDefinition(ctx context.Context, def *DocumentDefinition, record Document) (Document, error) {
	held, ok := s.heldDocuments[record.ID]
	if !ok || held.record.Version == def.options.Version {
		return record, nil
	}
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	if err := s.storeHeldDocuments(ctx, []ID{record.ID}); err != nil {
		return Document{}, s.backendResult(ctx, err, true)
	}
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	return s.readImage.Documents[record.ID], nil
}

// Close has already excluded new admissions and holds the line. A failed
// Session drops memory-only state; a healthy Close stores all holds in one batch.
func (s *Session) flushHeldOnClose() error {
	if len(s.heldDocuments) == 0 {
		return nil
	}
	writes := make([]Write, 0, len(s.heldDocuments))
	for _, id := range ids(s.heldDocuments) {
		writes = append(writes, heldWrite(s.heldDocuments[id]))
	}
	ctx := context.Background()
	highWater, err := s.core.sessionReadAdmission(ctx)
	if err != nil {
		return err
	}
	stored, err := s.prepareHeldStoragePublication(ctx, writes, highWater, nil)
	if err != nil {
		return err
	}
	receipt, candidate, err := s.settleBatch(ctx, Batch{Writes: writes}, s.readImage.Seq)
	if err != nil {
		return err
	}
	if err := s.failure(); err != nil {
		return err
	}
	if err := s.adoptReceipt(receipt, &candidate); err != nil {
		return err
	}
	s.publishStoredCommit(ctx, stored.publication)
	return nil
}
