package durable

import (
	"context"
	"errors"
	"sync"
)

// ViewObserver receives synchronous adopted conversation revisions. Callbacks
// must not block or call Session APIs. An error detaches only this observer.
type ViewObserver struct {
	Advance func(context.Context, ConversationView, []Operation) error
	End     func(WatchEnd) error
}

// ViewObservation owns its attachment and the readonly initial revision.
// Detach is idempotent and suppresses subsequent advances and end callbacks.
type ViewObservation struct {
	Value    ConversationView
	mu       sync.Mutex
	watch    *ConversationWatch
	observer ViewObserver
}

func (o *ViewObservation) release() (*ConversationWatch, ViewObserver) {
	o.mu.Lock()
	defer o.mu.Unlock()
	watch, observer := o.watch, o.observer
	o.watch = nil
	o.observer = ViewObserver{}
	return watch, observer
}
func (o *ViewObservation) Detach() {
	if o == nil {
		return
	}
	if watch, _ := o.release(); watch != nil {
		watch.sub.Stop()
	}
}
func (o *ViewObservation) end(end WatchEnd) {
	watch, observer := o.release()
	if watch == nil {
		return
	}
	if end.Reason == "session_closed" || end.Reason == "session_failed" {
		watch.sub.session.failureMu.Lock()
		cause := watch.sub.session.failureCause
		watch.sub.session.failureMu.Unlock()
		if cause == nil {
			cause = errors.New("session closed")
		}
		end = WatchEnd{Reason: "failed", Error: cause}
	}
	if err := callSessionCommitObserver(func() error { return observer.End(end) }); err != nil {
		watch.sub.session.reportListener(context.Background(), err)
	}
}
func (o *ViewObservation) advance(frame PublicationFrame) {
	o.mu.Lock()
	watch, observer := o.watch, o.observer
	o.mu.Unlock()
	if watch == nil {
		return
	}
	projection := frame.conversation
	if projection == nil {
		return
	}
	err := projection.err
	if err == nil {
		watch.mu.Lock()
		watch.current = projection.value
		watch.readonlyCurrent = projection.readonly
		watch.mu.Unlock()
		err = callSessionCommitObserver(func() error { return observer.Advance(frame.Context, projection.readonly, projection.ops) })
	}
	if err != nil {
		watch.sub.session.reportListener(frame.Context, err)
		watch.sub.stop(WatchEnd{Reason: "listener_error", Error: err})
	}
}

// ObserveConversation atomically attaches a synchronous view observer. Context
// cancellation governs acquisition only. Value retains the initial revision.
func (h *Harness) ObserveConversation(ctx context.Context, id ID, observer ViewObserver, window ...ConversationWindow) (*ViewObservation, error) {
	if h.closing.Load() {
		return nil, ErrClosed
	}
	if err := h.session.failure(); err != nil {
		return nil, err
	}
	if observer.Advance == nil || observer.End == nil {
		return nil, reject("incomplete conversation observer")
	}
	observation := &ViewObservation{observer: observer}
	conversation := &ConversationHandle{h: h, id: id}
	if _, err := conversation.attachWatch(ctx, observation, window...); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		observation.Detach()
		return nil, err
	}
	return observation, nil
}

// Mount ordering survives the first observer's detachment. Source advances
// conversation mounts in insertion order, then each mount's observer Set.
type conversationViewMount struct {
	conversation ID
	order        uint64
	owners       int
}

func (s *Session) registerConversationMountLocked(p *CommitSubscription) {
	w := p.conversation
	if w == nil {
		return
	}
	if s.conversationMounts == nil {
		s.conversationMounts = map[ID]*conversationViewMount{}
	}
	id := w.head.Conversation.ID
	mount := s.conversationMounts[id]
	if mount == nil {
		mount = &conversationViewMount{conversation: id, order: p.order}
		s.conversationMounts[id] = mount
	}
	mount.owners++
	w.mount = mount
}
func (s *Session) releaseConversationMountLocked(p *CommitSubscription) {
	w := p.conversation
	if w == nil || w.mount == nil {
		return
	}
	mount := w.mount
	mount.owners--
	if mount.owners == 0 {
		delete(s.conversationMounts, mount.conversation)
	}
}
func subscriptionLess(a, b *CommitSubscription) bool {
	order := func(p *CommitSubscription) uint64 {
		if p.conversation != nil && p.conversation.mount != nil {
			return p.conversation.mount.order
		}
		return p.order
	}
	left, right := order(a), order(b)
	if left != right {
		return left < right
	}
	return a.order < b.order
}
