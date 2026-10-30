// Package responsestream contains index routing shared by Responses transports.
package responsestream

// Slots routes explicit output indices without falling back to an unrelated
// active item. Missing indices retain the legacy single-current-item behavior.
// Private parser state stays outside provider content blocks and snapshots.
type Slots[T any] struct {
	indexed map[int]*T
	last    *T
}

func (s *Slots[T]) Add(index *int, value *T) {
	if index != nil {
		if s.indexed == nil {
			s.indexed = make(map[int]*T)
		}
		s.indexed[*index] = value
	}
	s.last = value
}
func (s *Slots[T]) Get(index *int) *T {
	if index == nil {
		return s.last
	}
	return s.indexed[*index]
}
func (s *Slots[T]) Delete(index *int) {
	if index != nil {
		value := s.indexed[*index]
		delete(s.indexed, *index)
		if value == s.last {
			s.last = nil
		}
		return
	}
	if s.last != nil {
		for key, value := range s.indexed {
			if value == s.last {
				delete(s.indexed, key)
			}
		}
	}
	s.last = nil
}
