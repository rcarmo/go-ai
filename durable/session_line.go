package durable

import "context"

// Line job identity and read gates ported from pinned v1.5.0 session.ts.
// jobs mirrors #tail and remains held across touch(). line protects Go memory;
// only that lock is suspended so an off-line writer can publish the dependency.

// ALL_KEYS mirrors upstream's `*`: a read that cannot name the keys it touches
// (a whole-state snapshot) waits for every admitted part to publish. Native
// transaction state is a whole-state snapshot, so this is the barrier its
// commits and full-state reads use.
const ALL_KEYS = "*"

// jobIndex numbers jobs in admission order. The writer records part indices so a
// failure can name the jobs it covers, as upstream's admission index does.
func (s *Session) jobIndex() uint64 {
	s.nextJobIndex++
	return s.nextJobIndex
}

// touch waits for the part that writes `key` to publish, then returns holding
// the line. The caller must hold the line on entry and must treat everything it
// captured before the call as possibly stale: the line was released.
func (s *Session) touch(ctx context.Context, key string) error {
	gate := s.gateFor(key)
	if gate == nil {
		return s.failure()
	}
	// #tail does not advance while the job awaits touch(). Only the memory
	// lock is released, so the writer can publish; later jobs remain queued.
	s.currentJobHit = true
	if s.jobOutcome != nil {
		s.jobOutcome.hit = true
	}
	jobHeld := s.jobHeld
	outcome := s.jobOutcome
	s.jobOutcome = nil
	s.jobHeld = false
	s.line.release()
	var err error
	select {
	case <-ctx.Done():
		err = contextError(ctx)
	case <-gate:
	}
	// Every exit owns the memory line; callers always release it, including
	// failure/cancellation. Never release someone else's token on an error.
	_ = s.line.acquire(context.Background())
	s.jobHeld = jobHeld
	s.jobOutcome = outcome
	if failure := s.failure(); failure != nil {
		return failure
	}
	if err != nil {
		return err
	}
	// This job was admitted before Close sealed new admission. Upstream's
	// touch continuation asserts health, not usability; Close cannot replace
	// the admitted job's own cancellation with a fresh ErrClosed outcome.
	return contextError(ctx)
}

// touchAll is the ALL_KEYS barrier: it waits for everything admitted so far.
func (s *Session) touchAll(ctx context.Context) error {
	return s.touch(ctx, ALL_KEYS)
}

func (s *Session) gateFor(key string) <-chan struct{} {
	if s.writer == nil {
		return nil
	}
	if key == ALL_KEYS {
		return s.writer.drained()
	}
	return s.writer.gate(key)
}
