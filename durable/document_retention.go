package durable

import "time"

func (s *Session) documentPinned(id ID) bool {
	s.observerMu.Lock()
	defer s.observerMu.Unlock()
	for watch := range s.watches {
		watch.mu.Lock()
		live := watch.id == id && !watch.ended
		watch.mu.Unlock()
		if live {
			return true
		}
	}
	return false
}
func (s *Session) sweepDocumentCache() {
	if len(s.definitionCache) == 0 {
		return
	}
	retention := s.documentRetentionMs()
	now := s.now()
	for key, value := range s.definitionCache {
		if s.documentPinned(key.id) {
			value.lastUsed = now
			s.definitionCache[key] = value
			continue
		}
		if retention <= 0 || now >= value.lastUsed && uint64(now)-uint64(value.lastUsed) >= uint64(retention) {
			delete(s.definitionCache, key)
			s.definitionCacheBytes -= value.bytes
		}
	}
}
func (s *Session) scheduleDocumentExpiry() {
	if s.documentExpiry != nil {
		s.documentExpiry.Stop()
		s.documentExpiry = nil
	}
	if len(s.definitionCache) == 0 || s.closing.Load() {
		return
	}
	retention := s.documentRetentionMs()
	now := s.now()
	var delay time.Duration
	for key, value := range s.definitionCache {
		if s.documentPinned(key.id) {
			continue
		}
		elapsed := uint64(0)
		if now >= value.lastUsed {
			elapsed = uint64(now) - uint64(value.lastUsed)
		}
		left := int64(0)
		if elapsed < uint64(max(retention, int64(0))) {
			left = retention - int64(elapsed)
		}
		candidate := retentionDuration(left)
		if candidate <= 0 {
			candidate = time.Millisecond
		}
		if delay == 0 || candidate < delay {
			delay = candidate
		}
	}
	if delay > 0 {
		s.documentExpiry = time.AfterFunc(delay, func() {
			s.taskBookkeeping(func() {
				if !s.closing.Load() {
					s.sweepDocumentCache()
					s.scheduleDocumentExpiry()
				}
			})
		})
	}
}
