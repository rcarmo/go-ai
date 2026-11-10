package durable

import "context"

// A native read owns an immutable published image and a copy of the retained
// range header. Payload arrays/maps are immutable; idle/sequence/current fields
// remain private to each header. retained is only an identity witness on line.
type preparedContextRead struct {
	state              Snapshot
	conversation, at   ID
	previous, retained *contextRange
	revision           uint64
	limits             Limits
}

// captureContextRead runs on the Session line after native read admission.
func (s *Session) captureContextRead(state Snapshot, conversation, at ID) preparedContextRead {
	s.expireContextRanges()
	retained := s.contextRanges[conversation]
	var previous *contextRange
	if retained != nil {
		copy := *retained
		copy.idleSince = nil // retention is decided again on installation
		previous = &copy
	}
	return preparedContextRead{state: state, conversation: conversation, at: at, previous: previous, retained: retained, revision: s.contextRevision, limits: s.limits}
}

// installContextRead consumes the derived range. A stale result can still be
// returned for its captured bounds, but cannot skip later publications by
// becoming a current cache. Rejection requires no retry or writer admission.
func (s *Session) installContextRead(ctx context.Context, read preparedContextRead, derived *contextRange) error {
	if err := s.enter(ctx); err != nil {
		return err
	}
	defer s.leave()
	// Upstream gates every read: a job must not observe state an admitted
	// write has not published.
	if err := s.touchAll(ctx); err != nil {
		return err
	}
	// Native health can change while derivation is off line, independently of
	// Session publication (for example an uncertain raw write or storage Close).
	if _, err := s.core.sessionReadAdmission(ctx); err != nil {
		return s.backendResult(ctx, err, false)
	}
	valid := s.hasNativeReadImage() && s.readImage.Seq == read.state.Seq && s.contextRevision == read.revision && s.contextRanges[read.conversation] == read.retained
	s.expireContextRanges()
	if !valid || derived == nil || s.contextRanges[read.conversation] != read.retained {
		return nil
	}
	if read.retained != nil {
		derived.idleSince = read.retained.idleSince
	}
	if s.contextRanges == nil {
		s.contextRanges = map[ID]*contextRange{}
	}
	s.contextRanges[read.conversation] = derived
	s.expireContextRanges()
	return nil
}

func (s *Session) completeContextRead(ctx context.Context, read preparedContextRead) (ContextView, error) {
	if err := ctx.Err(); err != nil {
		return ContextView{}, err
	}
	view, derived, err := deriveContextRange(read.state, read.conversation, read.at, read.previous, read.limits)
	if err != nil {
		return ContextView{}, err
	}
	owned, err := detachContextView(view, read.limits)
	if err != nil {
		return ContextView{}, err
	}
	if err := ctx.Err(); err != nil {
		return ContextView{}, err
	}
	if err := s.installContextRead(ctx, read, derived); err != nil {
		return ContextView{}, err
	}
	return owned, nil
}

// capture is a private projection used by ConversationHandle.Context to capture
// its agent document from the same admitted image as the transcript bounds.
func (s *Session) contextView(ctx context.Context, conversation, at ID, capture func(Snapshot) error) (ContextView, error) {
	if err := s.enter(ctx); err != nil {
		return ContextView{}, err
	}
	state, native, err := s.taskSnapshotView(ctx)
	if err == nil && capture != nil {
		err = capture(state)
	}
	if err != nil {
		s.leave()
		return ContextView{}, err
	}
	if !native {
		// A wrapper owns its Snapshot projection, even at an unchanged sequence.
		// Keep its existing serial read boundary and never retain native witnesses.
		defer s.leave()
		view, _, err := deriveContextRange(state, conversation, at, nil, s.limits)
		if err != nil {
			return ContextView{}, err
		}
		return detachContextView(view, s.limits)
	}
	read := s.captureContextRead(state, conversation, at)
	s.leave()
	return s.completeContextRead(ctx, read)
}
