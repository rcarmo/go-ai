package durable

// CommitPersistence maps the source immediate/deferred policy. A nil WithinMs
// uses the current host persistence window; zero and +Inf are explicit values.
// Windows are process-local options and are never persisted as task state.
type CommitPersistence struct {
	Kind     string   `json:"kind"`
	WithinMs *float64 `json:"withinMs,omitempty"`
}

// CommitOptions supplies the optional policy on each public commit method.
// Nil Persistence and any kind other than deferred select immediate storage.
type CommitOptions struct {
	Persistence *CommitPersistence `json:"persistence,omitempty"`
}

// Resolve at the public call boundary, before admission, as source #deferral
// does. Copy the Number now so later caller/settings mutations cannot change
// an already queued commit. JavaScript ignores additional optional arguments.
func (s *Session) commitPolicy(options []CommitOptions) (bool, []float64, error) {
	if err := s.failure(); err != nil {
		return false, nil, err
	}
	if s.closing.Load() {
		return false, nil, ErrClosed
	}
	if len(options) == 0 || options[0].Persistence == nil || options[0].Persistence.Kind != "deferred" {
		return false, nil, nil
	}
	var requested []float64
	if window := options[0].Persistence.WithinMs; window != nil {
		requested = []float64{*window}
	}
	window, err := s.persistenceWindow(requested)
	if err != nil {
		return false, nil, err
	}
	return true, []float64{window}, nil
}
