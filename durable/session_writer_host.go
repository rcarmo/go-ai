package durable

import "context"

// Session-side writer host, ported from the host hooks pinned v1.5.0 Session
// passes to its Writer (src/session/session.ts). The queue itself lives in
// session_writer.go; this file owns only what a Session must mean for each hook.
func (s *Session) newSessionWriterFromSession() *sessionWriter {
	return newSessionWriter(sessionWriterHost{
		commit: s.writerCommit,
		// Native Storage exposes no publication scope, so this is upstream's
		// documented no-scope path: run the publication here, immediately.
		publishing: func(_ context.Context, publish func()) <-chan struct{} {
			// Source unregisters keys and adopts synchronously in this scope.
			// Lock before the closure unregisters, not inside the part callback:
			// otherwise readers can see no gate while memory is still stale.
			_ = s.line.acquire(context.Background())
			defer s.handoffLeave(nil)
			publish()
			return nil
		},
		failure: s.writerFailure,
		fail:    s.writerFail,
		report:  s.writerReport,
		// Upstream defers to a macrotask between publications so each publication
		// gets its own turn. Go has none, and this queue publishes one part at a
		// time on a single flush, so each publication already has its own turn.
		hop: func() {},
	})
}

// writerCommit settles batches off the Session line. Core admission owns the
// state the receipts witness, so candidate capture stays inside the sink.
func (s *Session) writerCommit(ctx context.Context, batches []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
	// Adoption takes the exact image storage holds at that receipt, so an internal
	// read can share its backing rather than copy it. Only a single-part flush can
	// witness that: in a multi-part group the state at a later receipt has already
	// moved past an earlier part, and adoption then rebuilds from the receipt frame.
	single := len(batches) == 1
	if s.flushImages == nil {
		s.flushImages = make(map[uint64]*Snapshot)
	}
	for seq := range s.flushImages {
		delete(s.flushImages, seq)
	}
	return s.core.commitParts(ctx, batches, func(r commitReceipt) error {
		if single {
			image := s.core.state
			s.flushImages[r.seq] = &image
		}
		if receive != nil {
			receive(r)
		}
		return nil
	}, true)
}

// writerFailure returns the first failure CAUSE, not a SessionFailed wrapper: the
// writer wraps it itself when it hands a part SessionFailed.
func (s *Session) writerFailure() error {
	s.failureMu.Lock()
	defer s.failureMu.Unlock()
	return s.failureCause
}

// writerFail records a cause the queue found, first cause wins, and fences
// admission the same way a backend failure does.
func (s *Session) writerFail(cause error, at uint64, own []uint64) {
	if cause == nil {
		return
	}
	// Once the writer settles storage it is the only writer, so a failure it sees
	// must also drive the shutdown a serial settle used to trigger from
	// backendResult. Upstream's fail() closes fire-and-forget too (session.ts:450).
	_ = s.backendResultAt(context.Background(), cause, true, &at, own)
}

func (s *Session) writerReport(err error, _ context.Context) {
	if err == nil {
		return
	}
	reportTaskError(s.timerReport, err)
}
