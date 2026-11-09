package durable

import "context"

// Session-side writer host, ported from the host hooks pinned v1.5.0 Session
// passes to its Writer (src/session/session.ts). The queue itself lives in
// session_writer.go; this file owns only what a Session must mean for each hook.
//
// The queue is installed but no ordinary commit admits to it yet: the admission
// handoff, the keyed read barriers and the close/failure boundary are one
// change, and installing admission before its readers is not safe.

func (s *Session) newSessionWriterFromSession() *sessionWriter {
	return newSessionWriter(sessionWriterHost{
		commit: s.writerCommit,
		// Native Storage exposes no publication scope, so this is upstream's
		// documented no-scope path: run the publication here, immediately.
		publishing: func(_ context.Context, publish func()) <-chan struct{} {
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
	return s.core.commitParts(ctx, batches, func(r commitReceipt) error {
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
func (s *Session) writerFail(cause error, _ uint64, _ []uint64) {
	if cause == nil {
		return
	}
	s.failureMu.Lock()
	if s.failureCause == nil {
		s.failureValue = asFailure(cause)
		s.failureCause = cause
	}
	s.failureMu.Unlock()
	s.closeWatches()
}

func (s *Session) writerReport(err error, _ context.Context) {
	if err == nil {
		return
	}
	reportTaskError(s.timerReport, err)
}
