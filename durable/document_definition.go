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
	// Create/copy/required migration bases bypass this ordinary-change predicate.
	CheckpointWhen func(JSON, []Operation, CheckpointInfo) (bool, error)
}

type CheckpointInfo struct{ DeltasSinceBase uint64 }

// DocumentDefinition is immutable after construction. No mutable options or
// registry pointer escapes; callback code remains a host responsibility.
type DocumentDefinition struct{ options DefinitionOptions }

func DefineDocument(options DefinitionOptions) (*DocumentDefinition, error) {
	if !validKind(options.Kind) || options.Version == 0 || options.Version > MaxID || options.Initial == nil {
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
	token *DocumentDefinition
	id    ID
}
type definitionCacheValue struct {
	value JSON
	bytes int64
}

// definitionValue runs at most once per token/incarnation until that document
// changes or the bounded cache unloads. Unrelated commits do not rerun migration. Stored versions remain authoritative: an older token may still read
// its stored shape after a newer token migrated only in process memory.
func (s *Session) definitionValue(def *DocumentDefinition, d Document) (JSON, error) {
	if err := checkDefinition(def, d); err != nil {
		return nil, err
	}
	key := definitionCacheKey{def, d.ID}
	if cached, ok := s.definitionCache[key]; ok {
		return copyObject(cached.value, s.limits)
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
	if int64(len(encoded)) > s.limits.MaxRetainedBytes {
		return copyObject(value, s.limits)
	}
	if len(s.definitionCache) >= s.limits.MaxPage || s.definitionCacheBytes+int64(len(encoded)) > s.limits.MaxRetainedBytes {
		s.definitionCache = nil
		s.definitionCacheBytes = 0
	}
	if s.definitionCache == nil {
		s.definitionCache = map[definitionCacheKey]definitionCacheValue{}
	}
	s.definitionCache[key] = definitionCacheValue{value, int64(len(encoded))}
	s.definitionCacheBytes += int64(len(encoded))
	return copyObject(value, s.limits)
}

// UnloadDocuments drops only process-local migrated values. It writes nothing.
func (s *Session) UnloadDocuments(ctx context.Context) error {
	if err := s.enter(ctx); err != nil {
		return err
	}
	defer s.leave()
	// Poisoned storage must not appear usable even for a cache operation.
	if _, err := s.store.Limits(); err != nil {
		return err
	}
	s.definitionCache = nil
	s.definitionCacheBytes = 0
	return nil
}

// SnapshotDefinition returns a detached typed-policy value without initialising
// or persisting migration. Callback credentials/code are never serialized.
func (s *Session) SnapshotDefinition(ctx context.Context, def *DocumentDefinition, owner ID, key *string) (JSON, bool, error) {
	if err := s.enter(ctx); err != nil {
		return nil, false, err
	}
	defer s.leave()
	address, err := definitionAddress(def, owner, key)
	if err != nil {
		return nil, false, err
	}
	state, err := s.store.Snapshot(ctx)
	if err != nil {
		return nil, false, err
	}
	d, ok := findDocumentState(state, address, CurrentDocumentPoint())
	if !ok {
		return nil, false, nil
	}
	value, err := s.definitionValue(def, d)
	return value, err == nil, err
}

// SnapshotDefinitionAsOf resolves the visible cutoff's owning ancestor and
// original commit sequence, independently of the descendant's copied content.
func (s *Session) SnapshotDefinitionAsOf(ctx context.Context, def *DocumentDefinition, conversation ID, key *string, at ID) (JSON, bool, error) {
	if err := s.enter(ctx); err != nil {
		return nil, false, err
	}
	defer s.leave()
	address, err := definitionAddress(def, conversation, key)
	if err != nil {
		return nil, false, err
	}
	if address.Scope != "conversation" || documentHistory(Document{Scope: def.options.Scope, History: def.options.History}) != "rewindable" {
		return nil, false, reject("historical definition requires rewindable conversation scope")
	}
	state, err := s.store.Snapshot(ctx)
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
	return value, true, nil
}

// AcquireDocument acquires a singleton or family member on this transaction.
// Initializers run once for absent addresses; successful typed acquisition of an
// older stored version stages a required full base even without a value edit.
func (t *Tx) AcquireDocument(def *DocumentDefinition, owner ID, key *string, seed JSON) (*DocumentHandle, error) {
	if err := t.enter(); err != nil {
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
