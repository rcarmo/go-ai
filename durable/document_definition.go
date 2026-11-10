package durable

import "context"

// DefinitionOptions describes host-owned code for one persisted document kind.
// Callbacks are process-local; returned values are strictly copied before use.
// Family seeds are used only for the first acquisition of an absent member.
type DefinitionOptions struct {
	Kind    string
	Version uint64
	Scope   string
	History string
	Fork    string
	Family  bool
	Initial func(JSON) (JSON, error)
	Migrate func(JSON, uint64) (JSON, error)
	// CheckpointWhen sees detached final value/ops and the already stored count.
	// Required: use an explicit false predicate to retain deltas indefinitely.
	// Create/copy/required migration bases bypass this ordinary-change predicate.
	CheckpointWhen func(JSON, []Operation, CheckpointInfo) (bool, error)
}

type CheckpointInfo struct{ DeltasSinceBase uint64 }

// DocumentDefinition is immutable after construction. No mutable options or
// registry pointer escapes; callback code remains a host responsibility.
type DocumentDefinition struct{ options DefinitionOptions }

func DefineDocument(options DefinitionOptions) (*DocumentDefinition, error) {
	if !validKind(options.Kind) || options.Version == 0 || options.Version > MaxID || options.Initial == nil || options.CheckpointWhen == nil {
		return nil, reject("invalid document definition")
	}
	if options.Scope != "session" && options.Scope != "conversation" && options.Scope != "task" {
		return nil, reject("invalid definition scope")
	}
	if err := validDocumentPolicy(Document{Scope: options.Scope, History: options.History, Fork: options.Fork}); err != nil {
		return nil, err
	}
	return &DocumentDefinition{options}, nil
}

func definitionAddress(def *DocumentDefinition, owner ID, key *string) (DocumentAddress, error) {
	if def == nil {
		return DocumentAddress{}, reject("nil document definition")
	}
	o := def.options
	if err := validateDocumentScope(DocumentScope{o.Scope, owner}); err != nil {
		return DocumentAddress{}, err
	}
	if o.Family != (key != nil) {
		return DocumentAddress{}, reject("definition family/key mismatch")
	}
	address := DocumentAddress{Scope: o.Scope, Owner: owner, Kind: o.Kind, Family: o.Family}
	if key != nil {
		address.Key = *key
	}
	return address, nil
}
func checkDefinition(def *DocumentDefinition, d Document) error {
	o := def.options
	if d.Kind != o.Kind || d.Scope != o.Scope || documentFamily(d) != o.Family || documentHistory(d) != documentHistory(Document{Scope: o.Scope, History: o.History}) || documentFork(d) != documentFork(Document{Fork: o.Fork}) {
		return reject("document definition identity/policy mismatch")
	}
	if d.Version > o.Version {
		return reject("stored document has newer version")
	}
	if d.Version < o.Version && o.Migrate == nil {
		return reject("document requires migration")
	}
	return nil
}
func findDocumentState(s Snapshot, address DocumentAddress, at DocumentPoint) (Document, bool) {
	for _, id := range ids(s.Documents) {
		d := s.Documents[id]
		if documentAddress(d) == address && aliveDocument(d, at) {
			return d, true
		}
	}
	return Document{}, false
}

type definitionCacheKey struct {
	address DocumentAddress
}
type definitionCacheValue struct {
	record       Document // stored incarnation metadata; Value is kept separately
	valueVersion uint64
	value        ReadonlyValue
	bytes        int64
	lastUsed     int64
}

// Native definitionValue runs at most once per version/incarnation until that
// document changes or the bounded cache unloads. Adapters own their Snapshot
// projection; neither a token/ID nor an unchanged sequence witnesses its value.
// Stored versions remain authoritative: an older token may still read its stored
// shape after a newer token migrated only in process memory.
func (s *Session) definitionValue(def *DocumentDefinition, d Document) (JSON, error) {
	if err := checkDefinition(def, d); err != nil {
		return nil, err
	}
	cacheable := s.hasNativeReadImage()
	if cacheable {
		s.sweepDocumentCache()
	}
	key := definitionCacheKey{documentAddress(d)}
	if cached, ok := s.definitionCache[key]; cacheable && ok && cached.record.ID == d.ID && cached.valueVersion == def.options.Version {
		cached.lastUsed = s.now()
		s.definitionCache[key] = cached
		s.scheduleDocumentExpiry()
		return copyReadonlyDocument(cached.value, s.limits)
	}
	// A cache has one materialized version per address, not one value per
	// definition-token identity. Reloading a different version replaces it.
	if cached, ok := s.definitionCache[key]; cacheable && ok {
		delete(s.definitionCache, key)
		s.definitionCacheBytes -= cached.bytes
	}
	value, err := copyObject(d.Value, s.limits)
	if err != nil {
		return nil, err
	}
	if d.Version < def.options.Version {
		value, err = def.options.Migrate(value, d.Version)
		if err != nil {
			return nil, err
		}
		value, err = copyObject(value, s.limits)
		if err != nil {
			return nil, err
		}
	}
	encoded, err := encodeBounded(value, s.limits, s.limits.MaxDocumentBytes)
	if err != nil {
		return nil, err
	}
	// Cache is an optimisation, never an authority or unbounded history store.
	// A migrated value can be valid JSON yet larger than the cache's entire
	// byte allowance. Return it detached without retaining it.
	if !cacheable || int64(len(encoded)) > s.limits.MaxRetainedBytes {
		return copyObject(value, s.limits)
	}
	if len(s.definitionCache) >= s.limits.MaxPage || s.definitionCacheBytes+int64(len(encoded)) > s.limits.MaxRetainedBytes {
		s.definitionCache = nil
		s.definitionCacheBytes = 0
	}
	if s.definitionCache == nil {
		s.definitionCache = map[definitionCacheKey]definitionCacheValue{}
	}
	record := d
	record.Value = nil
	frozenValue := s.cachedReadonlyDocument(d.ID, def.options.Version)
	if frozenValue == nil {
		frozen, err := freezeImmutableValue(map[string]any(value))
		if err != nil {
			return nil, err
		}
		frozenValue = readonlyValue(frozen)
	}
	s.definitionCache[key] = definitionCacheValue{record: record, valueVersion: def.options.Version, value: frozenValue, bytes: int64(len(encoded)), lastUsed: s.now()}
	s.definitionCacheBytes += int64(len(encoded))
	s.sweepDocumentCache()
	s.scheduleDocumentExpiry()
	return copyObject(value, s.limits)
}

// UnloadDocuments stores pending held changes before dropping process-local
// migrated values. With no holds it remains a write-free cache operation.
func (s *Session) UnloadDocuments(ctx context.Context) error {
	if err := s.enter(ctx); err != nil {
		return err
	}
	defer s.leave()
	// Upstream gates every read: a job must not observe state an admitted
	// write has not published.
	if err := s.touchAll(ctx); err != nil {
		return err
	}
	// Poisoned storage must not appear usable even for a cache operation.
	if _, err := s.guardedStorage().Limits(); err != nil {
		return err
	}
	if err := s.storeHeldDocuments(context.Background(), nil); err != nil {
		return s.backendResult(ctx, err, true)
	}
	s.definitionCache = nil
	s.definitionCacheBytes = 0
	if s.documentExpiry != nil {
		s.documentExpiry.Stop()
		s.documentExpiry = nil
	}
	return nil
}

// watchDefinitionValue resolves a non-creating watch baseline on the Session
// line. It shares snapshot migration/cache policy and never persists a base.
func (s *Session) watchDefinitionValue(ctx context.Context, def *DocumentDefinition, owner ID, key *string) (Document, JSON, bool, error) {
	address, e := definitionAddress(def, owner, key)
	if e != nil {
		return Document{}, nil, false, e
	}
	state, e := s.guardedStorage().Snapshot(ctx)
	if e != nil {
		return Document{}, nil, false, e
	}
	record, ok := findDocumentState(state, address, CurrentDocumentPoint())
	if !ok {
		return Document{}, nil, false, nil
	}
	record, e = s.reloadHeldDefinition(ctx, def, record)
	if e != nil {
		return Document{}, nil, false, e
	}
	value, e := s.definitionValue(def, record)
	return record, value, e == nil, e
}

// touchDefinition ports #loadDocument and GatedStorage's address/incarnation
// dependencies. Historical visibility first touches the requested conversation and
// entry, then uses the entry's actual ancestor as the document owner.
func (s *Session) touchDefinition(ctx context.Context, def *DocumentDefinition, owner ID, key *string, at ID, historical bool) error {
	address, err := definitionAddress(def, owner, key)
	if err != nil {
		return err
	}
	if !historical {
		return s.touch(ctx, documentKey(address))
	}
	if address.Scope != "conversation" || documentHistory(Document{Scope: def.options.Scope, History: def.options.History}) != "rewindable" {
		return reject("historical definition requires rewindable conversation scope")
	}
	if err := s.touch(ctx, conversationKey(owner)); err != nil {
		return err
	}
	if err := s.touch(ctx, entryKey(at)); err != nil {
		return err
	}
	state, err := s.taskSnapshot(ctx)
	if err != nil {
		return err
	}
	if at == 0 || uint64(at) > MaxID || !entryVisible(state, owner, at) {
		return reject("historical entry is not visible")
	}
	entry := state.Entries[at]
	address.Owner = entry.Conversation
	if err := s.touch(ctx, documentKey(address)); err != nil {
		return err
	}
	// The address gate may have published a different incarnation, so resolve
	// from the current publication image rather than the pre-wait snapshot.
	state, err = s.taskSnapshot(ctx)
	if err != nil {
		return err
	}
	if record, ok := findDocumentState(state, address, DocumentPoint{Seq: entry.Seq}); ok {
		return s.touch(ctx, incarnationKey(record.ID))
	}
	return nil
}

// SnapshotDefinition returns the shared readonly typed-policy value without initialising
// or persisting migration. Callback credentials/code are never serialized.
func (s *Session) SnapshotDefinition(ctx context.Context, def *DocumentDefinition, owner ID, key *string) (value ReadonlyValue, found bool, err error) {
	if value, found, cached, err := s.cachedDefinitionSnapshot(ctx, def, owner, key); cached || err != nil {
		return value, found, err
	}
	if err := s.enter(ctx); err != nil {
		return nil, false, err
	}
	outcome := s.jobOutcome
	defer func() {
		s.leave()
		err = s.deliveredError(outcome, err)
		if err != nil {
			value = nil
			found = false
		}
	}()
	if err := s.touchDefinition(ctx, def, owner, key, 0, false); err != nil {
		return nil, false, err
	}
	return s.snapshotDefinition(ctx, def, owner, key)
}

// snapshotDefinition runs on the Session line. Invocation callers check their
// lifetime on that same line before migration callbacks or cache admission.
func (s *Session) snapshotDefinition(ctx context.Context, def *DocumentDefinition, owner ID, key *string) (ReadonlyValue, bool, error) {
	address, err := definitionAddress(def, owner, key)
	if err != nil {
		return nil, false, err
	}
	state, err := s.guardedStorage().Snapshot(ctx)
	if err != nil {
		return nil, false, err
	}
	d, ok := findDocumentState(state, address, CurrentDocumentPoint())
	if !ok {
		return nil, false, nil
	}
	d, err = s.reloadHeldDefinition(ctx, def, d)
	if err != nil {
		return nil, false, err
	}
	value, err := s.definitionValue(def, d)
	if err != nil {
		return nil, false, err
	}
	if cached := s.cachedReadonlyDocument(d.ID, def.options.Version); cached != nil {
		return cached, true, nil
	}
	frozen, err := NewImmutableValue(value, s.limits)
	if err != nil {
		return nil, false, err
	}
	return readonlyValue(frozen), true, nil
}

// SnapshotDefinitionAsOf resolves the visible cutoff's owning ancestor and
// original commit sequence, independently of the descendant's copied content.
func (s *Session) SnapshotDefinitionAsOf(ctx context.Context, def *DocumentDefinition, conversation ID, key *string, at ID) (value ReadonlyValue, found bool, err error) {
	if err := s.enter(ctx); err != nil {
		return nil, false, err
	}
	outcome := s.jobOutcome
	defer func() {
		s.leave()
		err = s.deliveredError(outcome, err)
		if err != nil {
			value = nil
			found = false
		}
	}()
	if err := s.touchDefinition(ctx, def, conversation, key, at, true); err != nil {
		return nil, false, err
	}
	return s.snapshotDefinitionAsOf(ctx, def, conversation, key, at)
}

// snapshotDefinitionAsOf shares historical materialisation between public
// Session reads and invocation-fenced reads; caller holds the Session line.
func (s *Session) snapshotDefinitionAsOf(ctx context.Context, def *DocumentDefinition, conversation ID, key *string, at ID) (ReadonlyValue, bool, error) {
	address, err := definitionAddress(def, conversation, key)
	if err != nil {
		return nil, false, err
	}
	if address.Scope != "conversation" || documentHistory(Document{Scope: def.options.Scope, History: def.options.History}) != "rewindable" {
		return nil, false, reject("historical definition requires rewindable conversation scope")
	}
	state, err := s.guardedStorage().Snapshot(ctx)
	if err != nil {
		return nil, false, err
	}
	if at == 0 || uint64(at) > MaxID || !entryVisible(state, conversation, at) {
		return nil, false, reject("historical entry is not visible")
	}
	entry := state.Entries[at]
	address.Owner = entry.Conversation
	point := DocumentPoint{Seq: entry.Seq}
	record, ok := findDocumentState(state, address, point)
	if !ok {
		return nil, false, nil
	}
	d, ok, err := materializeDocument(state, record.ID, point, s.limits)
	if err != nil || !ok {
		return nil, ok, err
	}
	if err = checkDefinition(def, d); err != nil {
		return nil, false, err
	}
	value, err := copyObject(d.Value, s.limits)
	if err != nil {
		return nil, false, err
	}
	if d.Version < def.options.Version {
		value, err = def.options.Migrate(value, d.Version)
		if err != nil {
			return nil, false, err
		}
	}
	value, err = copyObject(value, s.limits)
	if err != nil {
		return nil, false, err
	}
	if _, err = encodeBounded(value, s.limits, s.limits.MaxDocumentBytes); err != nil {
		return nil, false, err
	}
	frozen, err := NewImmutableValue(value, s.limits)
	if err != nil {
		return nil, false, err
	}
	return readonlyValue(frozen), true, nil
}

// AcquireDocument acquires a singleton or family member on this transaction.
// Initializers run once for absent addresses; successful typed acquisition of an
// older stored version stages a required full base even without a value edit.
func (t *Tx) AcquireDocument(def *DocumentDefinition, owner ID, key *string, seed JSON) (*DocumentHandle, error) {
	if err := t.enterKeys(); err != nil {
		return nil, err
	}
	defer t.leave()
	return t.acquireDefinition(def, owner, key, seed)
}
func (t *Tx) acquireDefinition(def *DocumentDefinition, owner ID, key *string, seed JSON) (*DocumentHandle, error) {
	address, err := definitionAddress(def, owner, key)
	if err != nil {
		return nil, err
	}
	if err := t.touchKeys(documentKey(address)); err != nil {
		return nil, err
	}
	if address.Scope == "conversation" {
		if err := t.touchKeys(conversationKey(owner)); err != nil {
			return nil, err
		}
	}
	if address.Scope == "task" {
		if err := t.touchKeys(taskKey(owner)); err != nil {
			return nil, err
		}
	}
	state := t.state
	if len(t.writes) > 0 {
		state, err = t.current()
		if err != nil {
			return nil, err
		}
	}
	if address.Scope == "conversation" {
		if _, ok := state.Conversations[owner]; !ok {
			return nil, reject("definition conversation missing")
		}
	}
	if address.Scope == "task" {
		task, ok := state.Tasks[owner]
		if !ok || terminalStatus(task.Status) {
			return nil, reject("definition task owner is not live")
		}
	}
	d, found := findDocumentState(state, address, CurrentDocumentPoint())
	if found {
		if t.session != nil {
			before := t.session.readImage.Seq
			if _, err = t.session.reloadHeldDefinition(t.ctx, def, d); err != nil {
				return nil, err
			}
			if before != t.session.readImage.Seq {
				// A real prefix was stored inside this callback. Refresh the
				// immutable base/history, not the callback or its staged intent.
				image := t.session.memoryReadImage()
				image.HighWater = max(image.HighWater, t.state.HighWater)
				t.state = image
				t.stagedState, t.stagedWrites, t.publicationState = nil, nil, nil
				state, err = t.current()
				if err != nil {
					return nil, err
				}
				d = state.Documents[d.ID]
			}
		}
		if err = checkDefinition(def, d); err != nil {
			return nil, err
		}
		if d.Version < def.options.Version {
			var value JSON
			staged := false
			for _, w := range t.writes {
				if w.Document != nil && w.Document.ID == d.ID {
					staged = true
					break
				}
			}
			if t.session != nil && !staged {
				value, err = t.session.definitionValue(def, d)
			} else {
				value, err = copyObject(d.Value, t.limits)
				if err == nil {
					value, err = def.options.Migrate(value, d.Version)
				}
			}
			if err != nil {
				return nil, err
			}
			value, err = copyObject(value, t.limits)
			if err != nil {
				return nil, err
			}
			d.Version, d.Value, d.DeltasSinceBase = def.options.Version, value, 0
			if err = t.stage(Write{Op: "put-document", Document: &d}); err != nil {
				return nil, err
			}
			t.documentPlan(d.ID).ops = []Operation{}
		}
		t.documentPlan(d.ID).definition = def
		return &DocumentHandle{t, d.ID, d.Scope, d.Owner, d.Kind, d.Key, d.Version}, nil
	}
	var ownedSeed JSON
	if seed != nil {
		ownedSeed, err = copyObject(seed, t.limits)
		if err != nil {
			return nil, err
		}
	}
	value, err := def.options.Initial(ownedSeed)
	if err != nil {
		return nil, err
	}
	value, err = copyObject(value, t.limits)
	if err != nil {
		return nil, err
	}
	id, err := t.store.mintID(t.ctx, true)
	if err != nil {
		return nil, err
	}
	t.state.HighWater = uint64(id)
	d = Document{ID: id, Scope: address.Scope, Owner: owner, Kind: address.Kind, Key: address.Key, Family: address.Family, History: def.options.History, Fork: def.options.Fork, Version: def.options.Version, Value: value}
	if err = t.stage(Write{Op: "put-document", Document: &d}); err != nil {
		return nil, err
	}
	t.documentPlan(d.ID).definition = def
	return &DocumentHandle{t, d.ID, d.Scope, d.Owner, d.Kind, d.Key, d.Version}, nil
}

// RetireDefinition is a no-op for an absent address. Typed access validates the
// policy/version first and persists a required migration base before retirement.
func (t *Tx) RetireDefinition(def *DocumentDefinition, owner ID, key *string) error {
	if err := t.enter(); err != nil {
		return err
	}
	defer t.leave()
	address, err := definitionAddress(def, owner, key)
	if err != nil {
		return err
	}
	state := t.state
	if len(t.writes) > 0 {
		state, err = t.current()
	}
	if err != nil {
		return err
	}
	if _, ok := findDocumentState(state, address, CurrentDocumentPoint()); !ok {
		return nil
	}
	h, err := t.acquireDefinition(def, owner, key, nil)
	if err != nil {
		return err
	}
	d, err := t.currentDocument(h.id)
	if err != nil {
		return err
	}
	return t.stage(Write{Op: "retire-document", Document: &d})
}

// cachedDefinitionSnapshot is Session.snapshot's no-enqueue branch. It observes
// the last adopted tracker value, even while that address has an unpublished
// writer, and does not send the caller's cancelled context through Storage.
func (s *Session) cachedDefinitionSnapshot(ctx context.Context, def *DocumentDefinition, owner ID, key *string) (ReadonlyValue, bool, bool, error) {
	if !s.hasNativeReadImage() {
		return nil, false, false, nil
	}
	if ctx == nil {
		return nil, false, false, reject("nil context")
	}
	_ = s.line.acquire(context.Background())
	flush := false
	defer func() {
		if flush {
			s.leave()
		} else {
			s.handoffLeave(nil)
		}
	}()
	if err := s.failure(); err != nil {
		return nil, false, false, err
	}
	if s.closing.Load() {
		return nil, false, false, ErrClosed
	}
	address, err := definitionAddress(def, owner, key)
	if err != nil {
		return nil, false, false, err
	}
	for _, held := range s.heldDocuments {
		if documentAddress(held.record) == address && held.record.Version == def.options.Version && held.readonly != nil {
			if err := checkDefinition(def, held.record); err != nil {
				return nil, false, true, err
			}
			if err := s.cacheHeldReadonly(held); err != nil {
				return nil, false, true, err
			}
			flush = true
			return held.readonly, true, true, nil
		}
	}
	cacheKey := definitionCacheKey{address}
	cached, ok := s.definitionCache[cacheKey]
	if !ok || cached.valueVersion != def.options.Version {
		return nil, false, false, nil
	}
	if s.documentRetentionMs() <= 0 && !s.documentPinned(cached.record.ID) {
		return nil, false, false, nil
	}
	if _, err := s.core.sessionReadAdmission(context.Background()); err != nil {
		return nil, false, false, s.backendResult(ctx, err, false)
	}
	if err := checkDefinition(def, cached.record); err != nil {
		return nil, false, false, err
	}
	cached.lastUsed = s.now()
	s.definitionCache[cacheKey] = cached
	s.scheduleDocumentExpiry()
	flush = true
	return cached.value, true, true, nil
}

// installDefinitionPlans is receipt/memory-time tracker installation for typed
// document changes. Migration/initialisation already ran during preparation.
func (s *Session) installDefinitionPlans(tx *Tx, image Snapshot) error {
	if !s.hasNativeReadImage() {
		return nil
	}
	for id, plan := range tx.documentPlans {
		def := plan.definition
		if def == nil {
			continue
		}
		record, ok := image.Documents[id]
		if !ok || record.Retired || record.Version != def.options.Version {
			continue
		}
		if _, err := s.definitionValue(def, record); err != nil {
			return err
		}
	}
	return nil
}
