package durable

// copyReadonlyDocument is used only at mutable internal transaction boundaries.
// Public reads keep the cached handle instead of converting it back to maps.
func copyReadonlyDocument(value ReadonlyValue, limits Limits) (JSON, error) {
	owned, err := ownJSONValue(value, limits)
	if err != nil {
		return nil, err
	}
	return JSON(owned.(map[string]any)), nil
}
func (s *Session) cachedReadonlyDocument(id ID, version uint64) ReadonlyValue {
	if !s.hasNativeReadImage() {
		return nil
	}
	for _, held := range s.heldDocuments {
		if held.record.ID == id && held.record.Version == version && held.readonly != nil {
			return held.readonly
		}
	}
	for _, cached := range s.definitionCache {
		if cached.record.ID == id && cached.valueVersion == version {
			return cached.value
		}
	}
	return nil
}

// Seal one persistent revision before handing the transaction to storage. Both
// memory and receipt adoption reuse it, including every asynchronous watch.
type readonlyDocumentRevision struct {
	value ReadonlyValue
	bytes int64
}

func (s *Session) prepareReadonlyDocuments(tx *Tx, publication CommitPublication) error {
	tx.readonlyDocuments = map[ID]readonlyDocumentRevision{}
	for _, change := range publication.Documents {
		if change.Record.Retired {
			continue
		}
		before := s.cachedReadonlyDocument(change.Record.ID, change.Version)
		if before == nil {
			if old, ok := tx.state.Documents[change.Record.ID]; ok && old.Version == change.Version {
				before = s.attachedReadonlyDocument(old.ID, change.Version, old.Value)
			}
		}
		value, err := persistentDocumentValue(before, change.Value, change.Ops, s.limits)
		if err != nil {
			return err
		}
		encoded, err := encodeBounded(value, s.limits, s.limits.MaxDocumentBytes)
		if err != nil {
			return err
		}
		tx.readonlyDocuments[change.Record.ID] = readonlyDocumentRevision{value: value, bytes: int64(len(encoded))}
	}
	return nil
}

func (s *Session) adoptDefinitionPlans(tx *Tx, image Snapshot, changes []DocumentChange) error {
	previous := map[definitionCacheKey]definitionCacheValue{}
	for _, write := range tx.writes {
		id := documentWriteID(write)
		if id == 0 {
			continue
		}
		for key, cached := range s.definitionCache {
			if cached.record.ID == id {
				previous[key] = cached
				delete(s.definitionCache, key)
				s.definitionCacheBytes -= cached.bytes
			}
		}
	}
	if err := s.installDefinitionPlans(tx, image); err != nil {
		return err
	}
	for _, change := range changes {
		key := definitionCacheKey{documentAddress(change.Record)}
		next, ok := s.definitionCache[key]
		if !ok && !change.Record.Retired {
			old, loaded := previous[key]
			revision := tx.readonlyDocuments[change.Record.ID]
			if loaded && old.record.ID == change.Record.ID && old.valueVersion == change.Version && revision.value != nil && (s.documentRetentionMs() > 0 || s.documentPinned(change.Record.ID)) && len(s.definitionCache) < s.limits.MaxPage && revision.bytes <= s.limits.MaxRetainedBytes-s.definitionCacheBytes {
				next = old
				next.record = change.Record
				next.record.Value = nil
				next.value = revision.value
				next.bytes = revision.bytes
				next.lastUsed = s.now()
				if s.definitionCache == nil {
					s.definitionCache = map[definitionCacheKey]definitionCacheValue{}
				}
				s.definitionCache[key] = next
				s.definitionCacheBytes += next.bytes
				ok = true
			}
		}
		if !ok || next.record.ID != change.Record.ID || next.valueVersion != change.Version {
			continue
		}
		if node := tx.readonlyDocuments[change.Record.ID].value; node != nil {
			next.value = node
			s.definitionCache[key] = next
		}
	}
	return nil
}

// The value was already owned and validated by transaction preparation.
func persistentDocumentValue(before ReadonlyValue, value JSON, operations []Operation, limits Limits) (ReadonlyValue, error) {
	if before != nil {
		if len(operations) == 0 && equalDeltaJSON(before.Copy(), map[string]any(value)) {
			return before, nil
		}
		if len(operations) > 0 {
			if node, handled, err := immutablePrimitiveObjectSets(before.immutableNode(), operations, limits); handled {
				if err != nil {
					return nil, err
				}
				return readonlyValue(node), nil
			}
			node := before.immutableNode()
			for _, operation := range operations {
				var err error
				node, err = applyImmutableValue(node, []Operation{operation}, limits)
				if err != nil {
					return nil, err
				}
			}
			return readonlyValue(node), nil
		}
	}
	node, err := freezeImmutableValue(map[string]any(value))
	if err != nil {
		return nil, err
	}
	return readonlyValue(node), nil
}

// A hold owns its tracker independently of idle retention. A warm read keeps
// that same node in the bounded idle cache when retention permits, so storing
// the hold does not manufacture another public revision.
func (s *Session) cacheHeldReadonly(held heldDocument) error {
	if s.documentRetentionMs() <= 0 {
		return nil
	}
	key := definitionCacheKey{documentAddress(held.record)}
	if cached, ok := s.definitionCache[key]; ok && cached.record.ID == held.record.ID && cached.valueVersion == held.record.Version {
		cached.value = held.readonly
		cached.lastUsed = s.now()
		s.definitionCache[key] = cached
		s.scheduleDocumentExpiry()
		return nil
	}
	if previous, ok := s.definitionCache[key]; ok {
		delete(s.definitionCache, key)
		s.definitionCacheBytes -= previous.bytes
	}
	encoded, err := encodeBounded(held.readonly, s.limits, s.limits.MaxDocumentBytes)
	if err != nil {
		return err
	}
	if len(s.definitionCache) >= s.limits.MaxPage || int64(len(encoded)) > s.limits.MaxRetainedBytes-s.definitionCacheBytes {
		return nil
	}
	if s.definitionCache == nil {
		s.definitionCache = map[definitionCacheKey]definitionCacheValue{}
	}
	record := held.record
	record.Value = nil
	s.definitionCache[key] = definitionCacheValue{record: record, valueVersion: record.Version, value: held.readonly, bytes: int64(len(encoded)), lastUsed: s.now()}
	s.definitionCacheBytes += int64(len(encoded))
	s.scheduleDocumentExpiry()
	return nil
}

// Attached readers pin the tracker, not just an idle cache entry. Recover that
// authority on a cold cache fill (notably the first attachment at retention0).
// Compare the materialised value as well as the schema version: installation
// may run before those readers have received a new revision of the same schema.
func (s *Session) attachedReadonlyDocument(id ID, version uint64, value JSON) ReadonlyValue {
	s.observerMu.Lock()
	defer s.observerMu.Unlock()
	matches := func(node ReadonlyValue) bool {
		return node != nil && equalDeltaJSON(node.Copy(), map[string]any(value))
	}
	for watch := range s.watches {
		watch.mu.Lock()
		node, valid := watch.observedReadonly, watch.id == id && watch.version == version && !watch.ended
		watch.mu.Unlock()
		if valid && matches(node) {
			return node
		}
	}
	for _, observation := range s.documentObservers {
		observation.mu.Lock()
		node, valid := observation.readonly, observation.id == id && observation.version == version && observation.session != nil
		observation.mu.Unlock()
		if valid && matches(node) {
			return node
		}
	}
	return nil
}
