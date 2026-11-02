package durable

import (
	"context"
	"errors"
	"sync"
)

// DocumentChange describes one adopted incarnation. Retired values are nil;
// copies retain source metadata and hydrate a new watch separately.
type DocumentChange struct {
	Record  Document
	Value   JSON
	Version uint64
	Ops     []Operation
	Source  *DocumentCopySource
}
type CommitPublication struct {
	Seq       uint64
	Tables    []Write
	Documents []DocumentChange
}
type WatchEnd struct {
	Reason string
	Error  error
}

// WatchFrame is a detached value delivery. Retirement retains the preceding
// value with no operations; a later incarnation replaces it at the same address.
type WatchFrame struct {
	Kind       string
	Retired    bool
	Seq        uint64
	Value      JSON
	Ops        []Operation
	Context    context.Context
	ownedValue JSON
	ready      <-chan struct{}
}

// DocumentWatch owns detached public views and serialises its host callback off
// the Session line. A terminal task owner ends it after pending callbacks return.
// Stop/Close never joins a caller-owned in-flight callback.
type DocumentWatch struct {
	mu           sync.Mutex
	session      *Session
	id           ID
	version      uint64
	address      DocumentAddress
	observed     JSON
	current      JSON
	pending      []WatchFrame
	bytes        []int64
	pendingBytes int64
	byteLimit    int64
	listener     func(context.Context, WatchFrame) error
	started      bool
	running      bool
	ended        bool
	ending       bool
	end          WatchEnd
	closed       chan struct{}
}

func (w *DocumentWatch) Value() (JSON, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return copyObjectNullable(w.current, w.session.limits)
}
func (w *DocumentWatch) Closed() <-chan struct{} { return w.closed }
func (w *DocumentWatch) End() (WatchEnd, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.end, w.ended
}

// Start is the value/operations compatibility view of StartFrame. Use StartFrame
// to distinguish retirement from an unchanged value; retirement does not close.
func (w *DocumentWatch) Start(listener func(context.Context, JSON, []Operation) error) error {
	if listener == nil {
		return w.StartFrame(nil)
	}
	return w.StartFrame(func(ctx context.Context, frame WatchFrame) error {
		return listener(ctx, frame.Value, frame.Ops)
	})
}

// StartFrame starts serialized tagged deliveries off the Session line.
func (w *DocumentWatch) StartFrame(listener func(context.Context, WatchFrame) error) error {
	w.mu.Lock()
	if w.ended {
		w.mu.Unlock()
		return ErrClosed
	}
	if w.started {
		w.mu.Unlock()
		return reject("watch already started")
	}
	if listener == nil {
		w.mu.Unlock()
		return reject("nil watch listener")
	}
	w.started = true
	w.listener = listener
	w.mu.Unlock()
	w.kick()
	return nil
}
func (w *DocumentWatch) Stop() WatchEnd {
	w.terminate(WatchEnd{Reason: "stopped"})
	end, _ := w.End()
	return end
}
func (w *DocumentWatch) terminate(end WatchEnd) {
	w.mu.Lock()
	if w.ended {
		w.mu.Unlock()
		return
	}
	w.ended = true
	w.end = end
	w.pending = nil
	w.bytes = nil
	w.pendingBytes = 0
	w.listener = nil
	w.observed = nil
	w.id = 0
	close(w.closed)
	w.mu.Unlock()
	w.session.observerMu.Lock()
	delete(w.session.watches, w)
	w.session.observerMu.Unlock()
	if !w.session.closing.Load() {
		go w.session.taskBookkeeping(func() {
			if !w.session.closing.Load() {
				w.session.sweepDocumentCache()
				w.session.scheduleDocumentExpiry()
			}
		})
	}
}
func (w *DocumentWatch) kick() {
	w.mu.Lock()
	if w.ended || w.running {
		w.mu.Unlock()
		return
	}
	if w.ending && len(w.pending) == 0 {
		w.mu.Unlock()
		w.terminate(WatchEnd{Reason: "ended"})
		return
	}
	if !w.started || len(w.pending) == 0 {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()
	go w.drain()
}
func (w *DocumentWatch) drain() {
	callbackContext := context.Background()
	defer func() {
		if recovered := recover(); recovered != nil {
			err := errors.New("watch listener panicked")
			w.terminate(WatchEnd{Reason: "listener_error", Error: err})
			w.session.reportListener(callbackContext, err)
		}
		w.mu.Lock()
		w.running = false
		w.mu.Unlock()
		w.kick()
	}()
	for {
		w.mu.Lock()
		if w.ended || len(w.pending) == 0 {
			finish := !w.ended && w.ending
			w.mu.Unlock()
			if finish {
				w.terminate(WatchEnd{Reason: "ended"})
			}
			return
		}
		frames := w.pending
		w.pending, w.bytes, w.pendingBytes = nil, nil, 0
		w.mu.Unlock()
		// Only frames still pending at this boundary fold. In-flight callbacks
		// own their call, and later publications belong to the next delivery.
		for _, pending := range frames {
			if pending.ready != nil {
				<-pending.ready
			}
		}
		frame, e := coalesceWatchFrames(frames, w.session.limits, w.byteLimit)
		clear(frames)
		if e != nil {
			w.terminate(WatchEnd{Reason: "budget_exceeded"})
			return
		}
		w.mu.Lock()
		if w.ended {
			w.mu.Unlock()
			return
		}
		w.current = frame.ownedValue
		callback := w.listener
		w.mu.Unlock()
		callbackContext = frame.Context
		frame.ownedValue, frame.ready = nil, nil
		if e := callback(frame.Context, frame); e != nil {
			w.terminate(WatchEnd{Reason: "listener_error", Error: e})
			w.session.reportListener(frame.Context, e)
			return
		}
	}
}
func copyObjectNullable(v JSON, l Limits) (JSON, error) {
	if v == nil {
		return nil, nil
	}
	return copyObject(v, l)
}

// preparedWatchFrame is owned and fallible preparation precedes storage. Adopt
// only enqueues these already detached frames; it never calls host code/copies.
type preparedWatchFrame struct {
	watch     *DocumentWatch
	frame     WatchFrame
	size      int64
	reset     WatchFrame
	resetSize int64
	version   uint64
	id        ID
	ownerEnd  bool
}

func (s *Session) prepareWatchFrames(ctx context.Context, publication CommitPublication, ready <-chan struct{}) ([]preparedWatchFrame, error) {
	s.observerMu.Lock()
	watchers := make([]*DocumentWatch, 0, len(s.watches))
	for w := range s.watches {
		watchers = append(watchers, w)
	}
	s.observerMu.Unlock()
	result := []preparedWatchFrame{}
	for _, w := range watchers {
		w.mu.Lock()
		version, incarnation, previous, ended := w.version, w.id, w.observed, w.ended || w.ending
		w.mu.Unlock()
		if ended {
			continue
		}
		ownerEnd := false
		if w.address.Scope == "task" {
			for _, write := range publication.Tables {
				if write.Task != nil && write.Task.ID == w.address.Owner && terminalStatus(write.Task.Status) {
					ownerEnd = true
				}
			}
		}
		var value JSON
		var operations []Operation
		retired, changed := false, false
		for _, change := range publication.Documents {
			if change.Record.ID != incarnation && documentAddress(change.Record) != w.address {
				continue
			}
			if change.Record.Retired {
				if change.Record.ID != incarnation {
					continue
				}
				incarnation = 0
				value, operations = previous, []Operation{}
				retired, changed = true, true
				continue
			}
			if change.Record.ID != incarnation || version != change.Version {
				operations = []Operation{{"r", mapOrNull(change.Value)}}
			} else {
				if len(change.Ops) == 0 {
					continue
				}
				operations = change.Ops
			}
			incarnation, version = change.Record.ID, change.Version
			value, previous, changed = change.Value, change.Value, true
		}
		if !changed {
			if ownerEnd {
				result = append(result, preparedWatchFrame{watch: w, ownerEnd: true})
			}
			continue
		}
		// One address delivery per publication, including retire+recreate. All
		// copies and byte checks precede settlement; enqueue cannot fail a commit.
		public, e := copyObjectNullable(value, s.limits)
		if e != nil {
			return nil, e
		}
		ops, e := ownOperations(operations, s.limits)
		if e != nil {
			return nil, e
		}
		private, e := copyObjectNullable(value, s.limits)
		if e != nil {
			return nil, e
		}
		resetValue, e := copyObjectNullable(value, s.limits)
		if e != nil {
			return nil, e
		}
		resetPrivate, e := copyObjectNullable(value, s.limits)
		if e != nil {
			return nil, e
		}
		resetOps, e := ownOperations([]Operation{{"r", mapOrNull(value)}}, s.limits)
		if e != nil {
			return nil, e
		}
		frame := WatchFrame{Kind: "value", Retired: retired, Seq: publication.Seq, Value: public, Ops: ops, Context: context.WithoutCancel(ctx), ownedValue: private, ready: ready}
		reset := WatchFrame{Kind: "value", Retired: retired, Seq: publication.Seq, Value: resetValue, Ops: resetOps, Context: context.WithoutCancel(ctx), ownedValue: resetPrivate, ready: ready}
		encoded, e := encodeBounded(JSON{"value": public, "ops": ops}, s.limits, s.limits.MaxFramePayloadBytes)
		if e != nil {
			return nil, e
		}
		resetEncoded, e := encodeBounded(JSON{"value": resetValue, "ops": resetOps}, s.limits, s.limits.MaxFramePayloadBytes)
		if e != nil {
			return nil, e
		}
		result = append(result, preparedWatchFrame{w, frame, int64(len(encoded)), reset, int64(len(resetEncoded)), version, incarnation, ownerEnd})
	}
	return result, nil
}
func mapOrNull(v JSON) any {
	if v == nil {
		return nil
	}
	return map[string]any(v)
}
func (s *Session) enqueueWatchFrames(frames []preparedWatchFrame) {
	for _, prepared := range frames {
		w := prepared.watch
		w.mu.Lock()
		if w.ended || w.ending {
			w.mu.Unlock()
			continue
		}
		if prepared.frame.Kind != "" {
			if prepared.resetSize > w.byteLimit {
				w.mu.Unlock()
				w.terminate(WatchEnd{Reason: "budget_exceeded"})
				continue
			}
			w.version, w.id = prepared.version, prepared.id
			w.observed = prepared.frame.ownedValue
			if w.id == 0 {
				w.observed = nil
			}
			frame, size := prepared.frame, prepared.size
			if size > w.byteLimit-w.pendingBytes {
				for _, pending := range w.pending {
					prepared.reset.Retired = prepared.reset.Retired || pending.Retired
				}
				w.pending = nil
				w.bytes = nil
				w.pendingBytes = 0
				frame, size = prepared.reset, prepared.resetSize
			}
			w.pending = append(w.pending, frame)
			w.bytes = append(w.bytes, size)
			w.pendingBytes += size
		}
		if prepared.ownerEnd {
			// Stay registered for Close/cancellation until final callbacks finish,
			// but release the incarnation and refuse all later publications now.
			w.ending, w.id, w.observed = true, 0, nil
		}
		w.mu.Unlock()
	}
}
func (s *Session) kickWatches() {
	s.observerMu.Lock()
	watchers := make([]*DocumentWatch, 0, len(s.watches))
	for w := range s.watches {
		watchers = append(watchers, w)
	}
	subs := make([]*CommitSubscription, 0, len(s.subscriptions))
	for p := range s.subscriptions {
		subs = append(subs, p)
	}
	s.observerMu.Unlock()
	for _, w := range watchers {
		w.kick()
	}
	for _, p := range subs {
		p.kick()
	}
}
func (s *Session) closeWatches() {
	s.observerMu.Lock()
	watchers := make([]*DocumentWatch, 0, len(s.watches))
	for w := range s.watches {
		watchers = append(watchers, w)
	}
	subs := make([]*CommitSubscription, 0, len(s.subscriptions))
	for p := range s.subscriptions {
		subs = append(subs, p)
	}
	s.observerMu.Unlock()
	end := WatchEnd{Reason: "session_closed"}
	if err := s.failure(); err != nil {
		end = WatchEnd{Reason: "session_failed", Error: err}
	}
	for _, w := range watchers {
		w.terminate(end)
	}
	for _, p := range subs {
		p.stop(end)
	}
}

// WatchDefinition atomically hydrates an existing document and follows its address.
// An absent address is not created. Retirement keeps the watch open for recreation,
// except when its task owner is terminal: final delivery then ends the watch.
// Registration budget is a native process bound: at most min(MaxPage,64)
// watchers, each with MaxRetainedBytes/capacity encoded pending bytes. A root
// exceeding that quota rejects acquisition or ends a later watch explicitly.
func (s *Session) WatchDefinition(ctx context.Context, def *DocumentDefinition, owner ID, key *string) (*DocumentWatch, bool, error) {
	if e := s.enter(ctx); e != nil {
		return nil, false, e
	}
	defer s.leave()
	record, value, ok, e := s.watchDefinitionValue(ctx, def, owner, key)
	if e != nil || !ok {
		return nil, ok, e
	}
	capacity := s.limits.MaxPage
	if capacity > 64 {
		capacity = 64
	}
	quota := s.limits.MaxRetainedBytes / int64(capacity)
	encoded, e := encodeBounded(JSON{"value": value, "ops": []Operation{{"r", mapOrNull(value)}}}, s.limits, s.limits.MaxFramePayloadBytes)
	if e != nil {
		return nil, false, e
	}
	if int64(len(encoded)) > quota {
		return nil, false, reject("watch baseline exceeds process quota")
	}
	if e = ctx.Err(); e != nil {
		return nil, false, e
	}
	w := &DocumentWatch{session: s, id: record.ID, version: def.options.Version, address: documentAddress(record), observed: value, current: value, byteLimit: quota, closed: make(chan struct{})}
	s.observerMu.Lock()
	if s.closing.Load() {
		s.observerMu.Unlock()
		return nil, false, ErrClosed
	}
	if s.observerProcessCountLocked() >= capacity {
		s.observerMu.Unlock()
		return nil, false, reject("watch process count limit")
	}
	if quota > s.limits.MaxRetainedBytes-s.observerReservedBytesLocked() {
		s.observerMu.Unlock()
		return nil, false, reject("watch baseline exceeds process quota")
	}
	if s.watches == nil {
		s.watches = map[*DocumentWatch]bool{}
	}
	s.watches[w] = true
	s.observerMu.Unlock()
	if ctx.Done() != nil {
		go func() {
			select {
			case <-ctx.Done():
				w.terminate(WatchEnd{Reason: "cancelled"})
			case <-w.closed:
			}
		}()
	}
	return w, true, nil
}

func (t *Tx) preparePublication(ctx context.Context) (CommitPublication, error) {
	publication := CommitPublication{Seq: t.state.Seq + 1}
	if len(t.writes) == 0 {
		return publication, nil
	}
	state, e := t.prepareCandidate(t.writes, true)
	if e != nil {
		return CommitPublication{}, e
	}
	t.publicationState = &state
	changed := map[ID]bool{}
	for _, write := range t.writes {
		if write.Document != nil {
			changed[write.Document.ID] = true
		}
		if write.Delta != nil {
			changed[write.Delta.ID] = true
		}
		if write.Document == nil && write.Delta == nil {
			// Publish the prepared adopted table projection, including storage's
			// entry placement. Original staged writes still have Seq/Position0.
			projected := write
			switch write.Op {
			case "create-conversation":
				v := state.Conversations[write.Conversation.ID]
				projected.Conversation = &v
			case "append-entry":
				v := state.Entries[write.Entry.ID]
				projected.Entry = &v
			case "put-task":
				v := state.Tasks[write.Task.ID]
				projected.Task = &v
			case "put-submission":
				v := state.Submissions[write.Submission.ID]
				projected.Submission = &v
			}
			bytes, e := encodeBounded(projected, t.limits, t.limits.MaxRecordBytes)
			if e != nil {
				return CommitPublication{}, e
			}
			var owned Write
			if e = decodeStrict(bytes, t.limits, t.limits.MaxRecordBytes, &owned); e != nil {
				return CommitPublication{}, e
			}
			publication.Tables = append(publication.Tables, owned)
		}
	}
	for _, id := range ids(changed) {
		record := state.Documents[id]
		change := DocumentChange{Record: record, Version: record.Version}
		change.Record.Value = nil
		if !record.Retired {
			change.Value, e = copyObject(record.Value, t.limits)
			if e != nil {
				return CommitPublication{}, e
			}
		}
		if plan := t.documentPlans[id]; plan != nil && !record.Retired {
			change.Ops, e = ownOperations(plan.ops, t.limits)
			if e != nil {
				return CommitPublication{}, e
			}
		}
		if _, exists := t.state.Documents[id]; !exists {
			change.Ops = []Operation{}
		}
		for _, write := range t.writes {
			if write.Op == "copy-document" && write.Document.ID == id {
				source := *write.Source
				change.Source = &source
			}
		}
		publication.Documents = append(publication.Documents, change)
	}
	return publication, nil
}

// PublicationFrame is an exact adopted batch or an explicit resnapshot after
// bounded backlog overflow. Snapshots are never raw pre-adoption state.
type PublicationFrame struct {
	Publication  CommitPublication
	Snapshot     *Snapshot
	Context      context.Context
	ready        <-chan struct{}
	graphRefresh bool                          // process-local metadata only; never a storage publication
	conversation *conversationPublicationFrame // private bounded view projection
}
type CommitSubscription struct {
	mu                      sync.Mutex
	session                 *Session
	pending                 []PublicationFrame
	sizes                   []int64
	bytes                   int64
	quota                   int64
	callback                func(context.Context, PublicationFrame) error
	started, running, ended bool
	graph                   bool // opts this existing quota-bound subscription into graph metadata wakes
	end                     WatchEnd
	closed                  chan struct{}
	conversation            *ConversationWatch // immutable after atomic registration
}

func (p *CommitSubscription) Start(fn func(context.Context, PublicationFrame) error) error {
	p.mu.Lock()
	if p.ended {
		p.mu.Unlock()
		return ErrClosed
	}
	if p.started || fn == nil {
		p.mu.Unlock()
		return reject("invalid publication listener start")
	}
	p.started = true
	p.callback = fn
	p.mu.Unlock()
	p.kick()
	return nil
}
func (p *CommitSubscription) Closed() <-chan struct{} { return p.closed }
func (p *CommitSubscription) End() (WatchEnd, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.end, p.ended
}
func (p *CommitSubscription) Stop() WatchEnd {
	p.stop(WatchEnd{Reason: "stopped"})
	e, _ := p.End()
	return e
}
func (p *CommitSubscription) stop(end WatchEnd) {
	p.mu.Lock()
	if p.ended {
		p.mu.Unlock()
		return
	}
	p.ended = true
	p.end = end
	p.pending = nil
	p.sizes = nil
	p.bytes = 0
	p.callback = nil
	close(p.closed)
	p.mu.Unlock()
	p.session.observerMu.Lock()
	delete(p.session.subscriptions, p)
	p.session.observerMu.Unlock()
}
func (p *CommitSubscription) kick() {
	p.mu.Lock()
	if p.ended || !p.started || p.running || len(p.pending) == 0 {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.mu.Unlock()
	go p.drain()
}
func (p *CommitSubscription) drain() {
	callbackContext := context.Background()
	defer func() {
		if recover() != nil {
			err := errors.New("publication listener panicked")
			p.stop(WatchEnd{Reason: "listener_error", Error: err})
			p.session.reportListener(callbackContext, err)
		}
		p.mu.Lock()
		p.running = false
		p.mu.Unlock()
		p.kick()
	}()
	for {
		p.mu.Lock()
		if p.ended || len(p.pending) == 0 {
			p.mu.Unlock()
			return
		}
		count := 1
		if p.conversation != nil && p.pending[0].conversation != nil && p.pending[0].conversation.err == nil {
			for count < len(p.pending) && p.pending[count].conversation != nil && p.pending[count].conversation.err == nil {
				count++
			}
		}
		frames := p.pending[:count:count]
		for _, size := range p.sizes[:count] {
			p.bytes -= size
		}
		p.pending, p.sizes = p.pending[count:], p.sizes[count:]
		if len(p.pending) == 0 {
			p.pending, p.sizes = nil, nil
		}
		fn := p.callback
		p.mu.Unlock()
		// Detach only pending calls, never the active callback. Every selected
		// publication must release its receipt/Session-line readiness fence.
		for _, pending := range frames {
			if pending.ready != nil {
				<-pending.ready
			}
		}
		p.mu.Lock()
		ended := p.ended
		p.mu.Unlock()
		if ended {
			clear(frames)
			return
		}
		frame := frames[0]
		var foldErr error
		if count > 1 {
			w := p.conversation
			w.mu.Lock()
			before := w.current
			w.mu.Unlock()
			frame, foldErr = coalesceConversationFrames(frames, before, w.limits, p.quota)
		}
		clear(frames) // release all consumed projections, including folded-away values
		if foldErr != nil {
			p.stop(WatchEnd{Reason: "budget_exceeded"})
			return
		}
		p.mu.Lock()
		ended = p.ended
		p.mu.Unlock()
		if ended {
			return
		}
		callbackContext = frame.Context
		if e := fn(frame.Context, frame); e != nil {
			p.stop(WatchEnd{Reason: "listener_error", Error: e})
			p.session.reportListener(frame.Context, e)
			return
		}
	}
}

// SubscribeCommits returns a detached baseline plus a subscription registered on
// the same line. Host callbacks run asynchronously and may initiate later commits.
func (s *Session) SubscribeCommits(ctx context.Context) (Snapshot, *CommitSubscription, error) {
	return s.subscribeCommits(ctx, nil)
}

// attach is internal projection setup, under the same line as registration.
func (s *Session) subscribeCommits(ctx context.Context, attach func(Snapshot, *CommitSubscription) error) (Snapshot, *CommitSubscription, error) {
	if e := s.enter(ctx); e != nil {
		return Snapshot{}, nil, e
	}
	defer s.leave()
	snapshot, e := s.guardedStorage().Snapshot(ctx)
	if e != nil {
		return Snapshot{}, nil, e
	}
	capacity := s.observerCapacity()
	quota := s.limits.MaxRetainedBytes / int64(capacity)
	// A subscription must be able to represent its worst-case root resnapshot.
	baselineBytes, e := snapshotObservationBytes(snapshot, s.limits)
	if e != nil {
		return Snapshot{}, nil, e
	}
	// Large baselines reserve more of the shared budget, not a fixed slot.
	if baselineBytes > quota {
		quota = baselineBytes
	}
	p := &CommitSubscription{session: s, quota: quota, closed: make(chan struct{})}
	if attach != nil {
		if err := attach(snapshot, p); err != nil {
			return Snapshot{}, nil, err
		}
	}
	s.observerMu.Lock()
	if s.closing.Load() {
		s.observerMu.Unlock()
		return Snapshot{}, nil, ErrClosed
	}
	if s.observerProcessCountLocked() >= capacity {
		s.observerMu.Unlock()
		return Snapshot{}, nil, reject("observer process count limit")
	}
	if quota > s.limits.MaxRetainedBytes-s.observerReservedBytesLocked() {
		s.observerMu.Unlock()
		return Snapshot{}, nil, reject("publication baseline exceeds process quota")
	}
	if s.subscriptions == nil {
		s.subscriptions = map[*CommitSubscription]bool{}
	}
	s.subscriptions[p] = true
	s.observerMu.Unlock()
	if ctx.Done() != nil {
		go func() {
			select {
			case <-ctx.Done():
				p.stop(WatchEnd{Reason: "cancelled"})
			case <-p.closed:
			}
		}()
	}
	return snapshot, p, nil
}

// Caller holds observerMu; reservations stay fixed until observer removal.
func (s *Session) observerReservedBytesLocked() int64 {
	var total int64
	for w := range s.watches {
		total += w.byteLimit
	}
	for p := range s.subscriptions {
		total += p.quota
	}
	return total
}

func (s *Session) observerCapacity() int {
	capacity := s.limits.MaxPage
	if capacity > 64 {
		capacity = 64
	}
	return capacity
}
func intQuota(n int64) int {
	if n > int64(^uint(0)>>1) {
		return int(^uint(0) >> 1)
	}
	return int(n)
}

type preparedSubscription struct {
	sub               *CommitSubscription
	frame             PublicationFrame
	reset             PublicationFrame
	size, resetSize   int64
	overflow          bool
	view              *ConversationView // installed only after receipted adoption
	incarnations      map[string]conversationIncarnation
	headOnly          bool // a bounded empty diff still advances the private witness
	disableProjection bool // a published wrapper fallback invalidates this head
}

func (s *Session) prepareSubscriptions(ctx context.Context, publication CommitPublication, tx *Tx, ready <-chan struct{}) ([]preparedSubscription, error) {
	return s.prepareSubscriptionKinds(ctx, publication, tx, ready, true, true)
}

func (s *Session) prepareSubscriptionKinds(ctx context.Context, publication CommitPublication, tx *Tx, ready <-chan struct{}, memory, stored bool) ([]preparedSubscription, error) {
	s.observerMu.Lock()
	list := make([]*CommitSubscription, 0, len(s.subscriptions))
	for p := range s.subscriptions {
		if (p.conversation != nil && memory) || (p.conversation == nil && stored) {
			list = append(list, p)
		}
	}
	s.observerMu.Unlock()
	if len(list) == 0 {
		return nil, nil
	}
	// Publication already validated this exact sealed candidate. Subscribers
	// still receive independently detached frames and snapshots; storage repeats
	// validation at its admission boundary. Reuse avoids a full replay per commit.
	if tx.publicationState == nil {
		return nil, reject("publication candidate missing")
	}
	state := *tx.publicationState
	result := []preparedSubscription{}
	for _, p := range list {
		if p.conversation != nil {
			enabled, failed := p.conversation.projectionStatus()
			if failed {
				continue
			}
			if enabled && s.hasNativeReadImage() {
				if item := p.conversation.preparePublication(ctx, publication, ready); item != nil {
					result = append(result, *item)
				}
				continue
			}
		}
		// Each subscription owns its independent batch and fallback snapshot, copied
		// before storage settlement. The adopted path performs no fallible work.
		encoded, e := encodeBounded(publication, s.limits, s.limits.MaxRetainedBytesAsInt())
		if e != nil {
			return nil, e
		}
		var copy CommitPublication
		if e = decodeStrict(encoded, s.limits, s.limits.MaxRetainedBytesAsInt(), &copy); e != nil {
			return nil, e
		}
		snapshot, e := cloneState(state, s.limits)
		if e != nil {
			return nil, e
		}
		resetBytes, e := snapshotObservationBytes(snapshot, s.limits)
		if e != nil {
			return nil, e
		}
		item := preparedSubscription{sub: p, frame: PublicationFrame{Publication: copy, Context: context.WithoutCancel(ctx), ready: ready}, reset: PublicationFrame{Snapshot: &snapshot, Context: context.WithoutCancel(ctx), ready: ready}, size: int64(len(encoded)), resetSize: resetBytes, overflow: false, disableProjection: p.conversation != nil}
		if w := p.conversation; w != nil {
			next, reset, size, err := w.prepareAuthoritativeReset(ctx, publication, snapshot, ready)
			if err != nil {
				// Projection failures affect only this watch, after receipt/readiness.
				frame := PublicationFrame{Context: context.WithoutCancel(ctx), ready: ready, conversation: &conversationPublicationFrame{err: err}}
				item.frame, item.reset, item.size, item.resetSize = frame, frame, 1, 1
			} else {
				item.view, item.reset, item.resetSize = &next, reset, size
			}
		}
		result = append(result, item)
	}
	return result, nil
}
func (l Limits) MaxRetainedBytesAsInt() int { return intQuota(l.MaxRetainedBytes) }
func (s *Session) enqueueSubscriptions(prepared []preparedSubscription) {
	for _, item := range prepared {
		p := item.sub
		p.mu.Lock()
		if p.ended {
			p.mu.Unlock()
			continue
		}
		if p.conversation != nil {
			p.conversation.adoptPublication(item)
		}
		if item.headOnly {
			p.mu.Unlock()
			continue
		}
		frame, size := item.frame, item.size
		// JSON conversation watches fold operations off-line. The 100-value
		// threshold belongs only to raw/native-value subscription delivery.
		foldable := frame.conversation != nil && frame.conversation.err == nil
		if (!foldable && len(p.pending) >= 100) || size > p.quota-p.bytes {
			p.pending = nil
			p.sizes = nil
			p.bytes = 0
			frame, size = item.reset, item.resetSize
		}
		if size > p.quota {
			p.mu.Unlock()
			p.stop(WatchEnd{Reason: "budget_exceeded"})
			continue
		}
		p.pending = append(p.pending, frame)
		p.sizes = append(p.sizes, size)
		p.bytes += size
		p.mu.Unlock()
	}
}

// DocumentState is a disposable committed read handle bound to one incarnation.
// Value reads on the Session line; retirement returns nil and recreation is not
// followed. No callback/replica transport is required for synchronous reads.
type DocumentState struct {
	mu         sync.Mutex
	session    *Session
	id         ID
	definition *DocumentDefinition
	version    uint64
	disposed   bool
}

func (s *Session) DocumentState(ctx context.Context, def *DocumentDefinition, owner ID, key *string) (*DocumentState, bool, error) {
	if e := s.enter(ctx); e != nil {
		return nil, false, e
	}
	defer s.leave()
	record, value, ok, e := s.watchDefinitionValue(ctx, def, owner, key)
	if e != nil || !ok {
		return nil, ok, e
	}
	_ = value
	return &DocumentState{session: s, id: record.ID, definition: def, version: def.options.Version}, true, nil
}
func (d *DocumentState) Dispose() { d.mu.Lock(); d.disposed = true; d.definition = nil; d.mu.Unlock() }
func (d *DocumentState) Value(ctx context.Context) (JSON, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.disposed {
		return nil, ErrClosed
	}
	if e := d.session.enter(ctx); e != nil {
		return nil, e
	}
	defer d.session.leave()
	snapshot, e := d.session.guardedStorage().Snapshot(ctx)
	if e != nil {
		return nil, e
	}
	record, ok := snapshot.Documents[d.id]
	if !ok || record.Retired {
		return nil, nil
	}
	if record.Version < d.version {
		return d.session.definitionValue(d.definition, record)
	}
	return copyObject(record.Value, d.session.limits)
}

// Snapshot keys are native numeric indices, never generic caller JSON maps.
// Size-account individual owned DTO records rather than granting numeric maps
// new marshal authority or serialising the entire unbounded image at once.
func snapshotObservationBytes(snapshot Snapshot, l Limits) (int64, error) {
	var total int64
	add := func(v any) error {
		p, e := encodeBounded(v, l, l.MaxRecordBytes)
		if e != nil {
			return e
		}
		total += int64(len(p))
		if total > l.MaxRetainedBytes {
			return reject("publication snapshot byte limit")
		}
		return nil
	}
	for _, v := range snapshot.Conversations {
		if e := add(v); e != nil {
			return 0, e
		}
	}
	for _, v := range snapshot.Entries {
		if e := add(v); e != nil {
			return 0, e
		}
	}
	for _, v := range snapshot.Tasks {
		if e := add(v); e != nil {
			return 0, e
		}
	}
	for _, v := range snapshot.Submissions {
		if e := add(v); e != nil {
			return 0, e
		}
	}
	for _, v := range snapshot.Documents {
		if e := add(v); e != nil {
			return 0, e
		}
	}
	for _, rs := range snapshot.DocumentRevisions {
		for _, v := range rs {
			if e := add(v); e != nil {
				return 0, e
			}
		}
	}
	return total, nil
}
