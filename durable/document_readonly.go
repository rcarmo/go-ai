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

// Storage receipts and memory adoption install one persistent value before
// any observer runs. Capture the old nodes before invalidating raw metadata.
func (s *Session) adoptDefinitionPlans(tx *Tx, image Snapshot, changes []DocumentChange) error {
	previous := map[ID]definitionCacheValue{}
	for _, write := range tx.writes {
		id := documentWriteID(write)
		if id == 0 {
			continue
		}
		for key, cached := range s.definitionCache {
			if cached.record.ID == id {
				previous[id] = cached
				delete(s.definitionCache, key)
				s.definitionCacheBytes -= cached.bytes
			}
		}
	}
	if err := s.installDefinitionPlans(tx, image); err != nil {
		return err
	}
	for _, change := range changes {
		if change.Record.Retired {
			continue
		}
		key := definitionCacheKey{documentAddress(change.Record)}
		next, exists := s.definitionCache[key]
		old, loaded := previous[change.Record.ID]
		if !exists || !loaded || old.valueVersion != change.Version || next.record.ID != change.Record.ID || next.valueVersion != change.Version {
			continue
		}
		if held, ok := s.heldDocuments[change.Record.ID]; ok && held.record.Version == change.Version && held.readonly != nil {
			next.value = held.readonly
			s.definitionCache[key] = next
			continue
		}
		if len(change.Ops) == 0 {
			if equalDeltaJSON(old.value.Copy(), next.value.Copy()) {
				next.value = old.value
			}
		} else {
			node := old.value.immutableNode()
			for _, operation := range change.Ops {
				var err error
				node, err = applyImmutableValue(node, []Operation{operation}, s.limits)
				if err != nil {
					return err
				}
			}
			next.value = readonlyValue(node)
		}
		s.definitionCache[key] = next
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
