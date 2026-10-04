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
type WatchFrame struct {
	Seq        uint64
	Value      JSON
	Ops        []Operation
	Context    context.Context
	ownedValue JSON
	ready      <-chan struct{}
}

// DocumentWatch owns detached public views and serialises its host callback off
// the Session line. Stop/Close never joins a caller-owned in-flight callback.
type DocumentWatch struct {
	mu           sync.Mutex
	session      *Session
	id           ID
	version      uint64
	initial      JSON
	current      JSON
	pending      []WatchFrame
	bytes        []int64
	pendingBytes int64
	byteLimit    int64
	listener     func(context.Context, JSON, []Operation) error
	started      bool
	running      bool
	ended        bool
	end          WatchEnd
	closed       chan struct{}
	retired      bool
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
func (w *DocumentWatch) Start(listener func(context.Context, JSON, []Operation) error) error {
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
	close(w.closed)
	w.mu.Unlock()
	w.session.observerMu.Lock()
	delete(w.session.watches, w)
	w.session.observerMu.Unlock()
}
func (w *DocumentWatch) kick() {
	w.mu.Lock()
	if w.ended || !w.started || w.running || len(w.pending) == 0 {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()
	go w.drain()
}
func (w *DocumentWatch) drain() {
	defer func() {
		if recovered := recover(); recovered != nil {
			w.terminate(WatchEnd{Reason: "listener_error", Error: errors.New("watch listener panicked")})
		}
		w.mu.Lock()
		w.running = false
		w.mu.Unlock()
		w.kick()
	}()
	for {
		w.mu.Lock()
		if w.ended || len(w.pending) == 0 {
			w.mu.Unlock()
			return
		}
		frame := w.pending[0]
		size := w.bytes[0]
		w.pending = w.pending[1:]
		w.bytes = w.bytes[1:]
		w.pendingBytes -= size
		// Own a separate Value copy before notifying the listener: public callback
		// mutation cannot alter Value(), another observer or any storage revision.
		w.current = frame.ownedValue
		callback := w.listener
		w.mu.Unlock()
		if frame.ready != nil {
			<-frame.ready
		}
		w.mu.Lock()
		ended := w.ended
		w.mu.Unlock()
		if ended {
			return
		}
		if e := callback(frame.Context, frame.Value, frame.Ops); e != nil {
			w.terminate(WatchEnd{Reason: "listener_error", Error: e})
			return
		}
		if frame.Value == nil {
			w.terminate(WatchEnd{Reason: "retired"})
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
		version, ended := w.version, w.ended
		w.mu.Unlock()
		if ended {
			continue
		}
		for _, change := range publication.Documents {
			if change.Record.ID != w.id {
				continue
			}
			if change.Source != nil {
				continue
			}
			value, e := copyObjectNullable(change.Value, s.limits)
			if e != nil {
				return nil, e
			}
			operations := change.Ops
			if value == nil || version != change.Version {
				operations = []Operation{{"r", mapOrNull(value)}}
			}
			if len(operations) == 0 {
				continue
			}
			ops, e := ownOperations(operations, s.limits)
			if e != nil {
				return nil, e
			}
			resetValue, e := copyObjectNullable(change.Value, s.limits)
			if e != nil {
				return nil, e
			}
			resetOps, e := ownOperations([]Operation{{"r", mapOrNull(resetValue)}}, s.limits)
			if e != nil {
				return nil, e
			}
			private, e := copyObjectNullable(change.Value, s.limits)
			if e != nil {
				return nil, e
			}
			resetPrivate, e := copyObjectNullable(change.Value, s.limits)
			if e != nil {
				return nil, e
			}
			frame := WatchFrame{Seq: publication.Seq, Value: value, Ops: ops, Context: context.WithoutCancel(ctx), ownedValue: private, ready: ready}
			reset := WatchFrame{Seq: publication.Seq, Value: resetValue, Ops: resetOps, Context: context.WithoutCancel(ctx), ownedValue: resetPrivate, ready: ready}
			encoded, e := encodeBounded(JSON{"value": value, "ops": ops}, s.limits, s.limits.MaxFramePayloadBytes)
			if e != nil {
				return nil, e
			}
			resetEncoded, e := encodeBounded(JSON{"value": resetValue, "ops": resetOps}, s.limits, s.limits.MaxFramePayloadBytes)
			if e != nil {
				return nil, e
			}
			result = append(result, preparedWatchFrame{w, frame, int64(len(encoded)), reset, int64(len(resetEncoded)), change.Version})
		}
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
		if w.ended || w.retired {
			w.mu.Unlock()
			continue
		}
		if prepared.resetSize > w.byteLimit {
			w.mu.Unlock()
			w.terminate(WatchEnd{Reason: "budget_exceeded"})
			continue
		}
		w.version = prepared.version
		if prepared.frame.Value == nil {
			w.retired = true
		}
		frame, size := prepared.frame, prepared.size
		if len(w.pending) >= 100 || size > w.byteLimit-w.pendingBytes {
			w.pending = nil
			w.bytes = nil
			w.pendingBytes = 0
			frame, size = prepared.reset, prepared.resetSize
		}
		w.pending = append(w.pending, frame)
		w.bytes = append(w.bytes, size)
		w.pendingBytes += size
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
	for _, w := range watchers {
		w.terminate(WatchEnd{Reason: "session_closed"})
	}
	for _, p := range subs {
		p.stop(WatchEnd{Reason: "session_closed"})
	}
}

// WatchDefinition atomically hydrates an existing incarnation and subscribes.
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
	w := &DocumentWatch{session: s, id: record.ID, version: def.options.Version, initial: value, current: value, byteLimit: quota, closed: make(chan struct{})}
	s.observerMu.Lock()
	if s.closing.Load() {
		s.observerMu.Unlock()
		return nil, false, ErrClosed
	}
	if len(s.watches)+len(s.subscriptions) >= capacity {
		s.observerMu.Unlock()
		return nil, false, reject("watch process count limit")
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
	state, e := prepare(t.state, commitRecord{Seq: publication.Seq, Writes: t.writes}, t.limits)
	if e != nil {
		return CommitPublication{}, e
	}
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
	Publication CommitPublication
	Snapshot    *Snapshot
	Context     context.Context
	ready       <-chan struct{}
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
	end                     WatchEnd
	closed                  chan struct{}
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
	defer func() {
		if recover() != nil {
			p.stop(WatchEnd{Reason: "listener_error", Error: errors.New("publication listener panicked")})
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
		frame := p.pending[0]
		p.bytes -= p.sizes[0]
		p.pending = p.pending[1:]
		p.sizes = p.sizes[1:]
		fn := p.callback
		p.mu.Unlock()
		<-frame.ready
		p.mu.Lock()
		ended := p.ended
		p.mu.Unlock()
		if ended {
			return
		}
		if e := fn(frame.Context, frame); e != nil {
			p.stop(WatchEnd{Reason: "listener_error", Error: e})
			return
		}
	}
}

// SubscribeCommits returns a detached baseline plus a subscription registered on
// the same line. Host callbacks run asynchronously and may initiate later commits.
func (s *Session) SubscribeCommits(ctx context.Context) (Snapshot, *CommitSubscription, error) {
	if e := s.enter(ctx); e != nil {
		return Snapshot{}, nil, e
	}
	defer s.leave()
	snapshot, e := s.store.Snapshot(ctx)
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
	if baselineBytes > quota {
		return Snapshot{}, nil, reject("publication baseline exceeds process quota")
	}
	p := &CommitSubscription{session: s, quota: quota, closed: make(chan struct{})}
	s.observerMu.Lock()
	if s.closing.Load() {
		s.observerMu.Unlock()
		return Snapshot{}, nil, ErrClosed
	}
	if len(s.watches)+len(s.subscriptions) >= capacity {
		s.observerMu.Unlock()
		return Snapshot{}, nil, reject("observer process count limit")
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
	sub             *CommitSubscription
	frame           PublicationFrame
	reset           PublicationFrame
	size, resetSize int64
	overflow        bool
}

func (s *Session) prepareSubscriptions(ctx context.Context, publication CommitPublication, tx *Tx, ready <-chan struct{}) ([]preparedSubscription, error) {
	s.observerMu.Lock()
	list := make([]*CommitSubscription, 0, len(s.subscriptions))
	for p := range s.subscriptions {
		list = append(list, p)
	}
	s.observerMu.Unlock()
	if len(list) == 0 {
		return nil, nil
	}
	state, e := prepare(tx.state, commitRecord{Seq: publication.Seq, Writes: tx.writes}, s.limits)
	if e != nil {
		return nil, e
	}
	result := []preparedSubscription{}
	for _, p := range list {
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
		result = append(result, preparedSubscription{p, PublicationFrame{Publication: copy, Context: context.WithoutCancel(ctx), ready: ready}, PublicationFrame{Snapshot: &snapshot, Context: context.WithoutCancel(ctx), ready: ready}, int64(len(encoded)), resetBytes, false})
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
		frame, size := item.frame, item.size
		if len(p.pending) >= 100 || size > p.quota-p.bytes {
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
	snapshot, e := d.session.store.Snapshot(ctx)
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
