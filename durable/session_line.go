package durable

import "context"

// Line job identity and read gates, ported from pinned v1.5.0 src/session/session.ts.
//
// Upstream's line is a promise chain: a job that awaits a gate suspends and the
// line is free for the next job, so `touch(key)` can be awaited from inside a
// job. The native line is a mutex, so the same await would deadlock against a
// publication that needs that line. `touch` therefore RELEASES the line across
// the wait and re-acquires it, re-asserting health afterwards: the two designs
// give a waiting reader the same observable behaviour, and the line is never
// held across a gate.

// ALL_KEYS mirrors upstream's `*`: a read that cannot name the keys it touches
// (a whole-state snapshot) waits for every admitted part to publish. Native
// transaction state is a whole-state snapshot, so this is the barrier its
// commits and full-state reads use.
const ALL_KEYS = "*"

// touch waits for the part that writes `key` to publish, then returns holding
// the line. The caller must hold the line on entry and must treat everything it
// captured before the call as possibly stale: the line was released.
func (s *Session) touch(ctx context.Context, key string) error {
	gate := s.gateFor(key)
	if gate == nil {
		return s.failure()
	}
	s.currentJobHit = true
	s.leave()
	select {
	case <-ctx.Done():
		if err := s.reacquire(ctx); err != nil {
			return err
		}
		return ctx.Err()
	case <-gate:
	}
	if err := s.reacquire(ctx); err != nil {
		return err
	}
	// A gate can complete while the Session fails; the caller must not read
	// behind a failure that arrived while it was off the line.
	return s.failure()
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

// reacquire takes the line back after an off-line wait, honouring cancellation
// and the failure/close fences the ordinary admission applies.
func (s *Session) reacquire(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.line:
	}
	if err := s.failure(); err != nil {
		s.leave()
		return err
	}
	if s.closing.Load() {
		s.leave()
		return ErrClosed
	}
	if e := ctx.Err(); e != nil {
		s.leave()
		return e
	}
	return nil
}
