package durable

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"sync"
	"sync/atomic"
)

var kindPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,127}$`)

// storeCore owns the only mutation line. Every admitted append settles while
// holding it, irrespective of the admission caller's later cancellation.
type storeCore struct {
	line         chan struct{}
	closing      atomic.Bool
	closeOnce    sync.Once
	closeDone    chan struct{}
	closeErr     error
	poisoned     bool
	sessionOwned bool
	ownershipMu  sync.Mutex
	state        Snapshot
	limits       Limits
	ordinal      uint64
	appendFrame  func(byte, uint64, uint64, []byte) error
	finish       func() error
}

func initialState() Snapshot {
	return Snapshot{HighWater: 1, Conversations: map[ID]Conversation{1: {ID: 1}}, Entries: map[ID]Entry{}, Tasks: map[ID]Task{}, Submissions: map[ID]Submission{}, Documents: map[ID]Document{}}
}
func newCore(l Limits) *storeCore {
	c := &storeCore{line: make(chan struct{}, 1), closeDone: make(chan struct{}), state: initialState(), limits: l, ordinal: 1}
	c.line <- struct{}{}
	return c
}
func (c *storeCore) enter(ctx context.Context) error {
	if ctx == nil {
		return reject("nil context")
	}
	if c.closing.Load() {
		return ErrClosed
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.line:
	}
	if c.closing.Load() {
		c.leave()
		return ErrClosed
	}
	if c.poisoned {
		c.leave()
		return ErrPoisoned
	}
	if err := ctx.Err(); err != nil {
		c.leave()
		return err
	}
	return nil
}
func (c *storeCore) leave() { c.line <- struct{}{} }
func (c *storeCore) Limits() (Limits, error) {
	if err := c.enter(context.Background()); err != nil {
		return Limits{}, err
	}
	defer c.leave()
	return c.limits, nil
}
func (c *storeCore) claim() error {
	if err := c.enter(context.Background()); err != nil {
		return err
	}
	defer c.leave()
	c.ownershipMu.Lock()
	defer c.ownershipMu.Unlock()
	if c.closing.Load() {
		return ErrClosed
	}
	if c.sessionOwned {
		return ErrOwned
	}
	c.sessionOwned = true
	return nil
}
func (c *storeCore) Close(ctx context.Context) error { return c.close(ctx, false) }
func (c *storeCore) close(ctx context.Context, owned bool) error {
	if ctx == nil {
		return reject("nil context")
	}
	// Close must work even after poison, while the writer capability stays private.
	// Seal before waiting for admitted writes, on a separate ownership fence.
	c.ownershipMu.Lock()
	if !owned && c.sessionOwned {
		c.ownershipMu.Unlock()
		return ErrOwned
	}
	c.closing.Store(true)
	c.ownershipMu.Unlock()
	c.closeOnce.Do(func() {
		c.closing.Store(true)
		go func() {
			<-c.line
			if c.finish != nil {
				c.closeErr = c.finish()
			}
			close(c.closeDone)
			c.leave()
		}()
	})
	select {
	case <-c.closeDone:
		return c.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (c *storeCore) settle(typ byte, high uint64, payload []byte) error {
	if c.ordinal >= MaxID {
		return reject("frame ordinal exhausted")
	}
	if c.appendFrame != nil {
		if err := c.appendFrame(typ, c.ordinal+1, high, payload); err != nil {
			var rejected *StorageRejected
			if errors.As(err, &rejected) {
				return err
			}
			c.poisoned = true
			return fmt.Errorf("%w: append settlement failed", ErrPoisoned)
		}
	}
	c.ordinal++
	return nil
}
func (c *storeCore) MintID(ctx context.Context) (ID, error) { return c.mintID(ctx, false) }
func (c *storeCore) mintID(ctx context.Context, owned bool) (ID, error) {
	if err := c.enter(ctx); err != nil {
		return 0, err
	}
	defer c.leave()
	if c.sessionOwned && !owned {
		return 0, ErrOwned
	}
	if c.state.HighWater >= MaxID || c.ordinal >= MaxID {
		return 0, reject("allocator exhausted")
	}
	id := c.state.HighWater + 1
	p, err := encodeBounded(reservation{First: id, Last: id}, c.limits, c.limits.MaxFramePayloadBytes)
	if err != nil {
		return 0, err
	}
	if err = c.settle(1, id, p); err != nil {
		return 0, err
	}
	c.state.HighWater = id
	return ID(id), nil
}
func (c *storeCore) Apply(ctx context.Context, b Batch) (uint64, error) {
	return c.apply(ctx, b, false)
}
func (c *storeCore) apply(ctx context.Context, b Batch, owned bool) (uint64, error) {
	if err := c.enter(ctx); err != nil {
		return 0, err
	}
	defer c.leave()
	if c.sessionOwned && !owned {
		return 0, ErrOwned
	}
	if c.state.Seq >= MaxID || c.ordinal >= MaxID {
		return 0, reject("sequence exhausted")
	}
	if err := validateBatchJSON(b, c.limits); err != nil {
		return 0, err
	}
	// Encoding validates/copies caller data before I/O. Decoding the encoded batch
	// gives private immutable ownership even when DTOs contain nested caller maps.
	p, err := encodeBounded(commitRecord{Seq: c.state.Seq + 1, Writes: b.Writes}, c.limits, c.limits.MaxFramePayloadBytes)
	if err != nil {
		return 0, err
	}
	var record commitRecord
	if err = decodeStrict(p, c.limits, c.limits.MaxFramePayloadBytes, &record); err != nil {
		return 0, err
	}
	next, err := prepare(c.state, record, c.limits)
	if err != nil {
		return 0, err
	}
	if err = c.settle(2, c.state.HighWater, p); err != nil {
		return 0, err
	}
	c.state = next
	return next.Seq, nil
}

type reservation struct {
	First uint64 `json:"first"`
	Last  uint64 `json:"last"`
}
type commitRecord struct {
	Seq    uint64  `json:"seq"`
	Writes []Write `json:"writes"`
}

// Public low-level batches have the same strict object ownership policy as Tx;
// accepting a struct through the trusted DTO envelope must not grant document
// input a custom native representation or marshaler authority.
func validateBatchJSON(b Batch, l Limits) error {
	if len(b.Writes) == 0 || len(b.Writes) > l.MaxWrites {
		return reject("invalid write count")
	}
	for _, w := range b.Writes {
		switch w.Op {
		case "create-conversation", "append-entry", "put-task", "put-submission", "put-document", "retire-document", "copy-document", "delta-document":
		default:
			return ErrUnsupported
		}
		if w.Delta != nil {
			if _, err := ownOperations(w.Delta.Ops, l); err != nil {
				return err
			}
		}
		var value JSON
		switch {
		case w.Entry != nil:
			value = w.Entry.Value
		case w.Task != nil:
			value = w.Task.Checkpoint
		case w.Submission != nil:
			value = w.Submission.Value
		case w.Document != nil && w.Op != "retire-document" && w.Op != "copy-document":
			value = w.Document.Value
		default:
			continue
		}
		if _, e := copyObject(value, l); e != nil {
			return e
		}
	}
	return nil
}

// candidateTables is private copy-on-write preparation over an already-owned
// immutable base. Every table is copied; unchanged nested JSON stays read-only.
// New writes have been detached by Apply/Tx before reaching prepare. Updating a
// record replaces its copied DTO; no preparation path mutates a shared value.
// Never return this candidate through a public read or retained memory image.
func candidateTables(s Snapshot) Snapshot {
	n := Snapshot{Seq: s.Seq, HighWater: s.HighWater, Conversations: make(map[ID]Conversation, len(s.Conversations)), Entries: make(map[ID]Entry, len(s.Entries)), Tasks: make(map[ID]Task, len(s.Tasks)), Submissions: make(map[ID]Submission, len(s.Submissions)), Documents: make(map[ID]Document, len(s.Documents))}
	for id, v := range s.Conversations {
		n.Conversations[id] = v
	}
	for id, v := range s.Entries {
		n.Entries[id] = v
	}
	for id, v := range s.Tasks {
		n.Tasks[id] = v
	}
	for id, v := range s.Submissions {
		n.Submissions[id] = v
	}
	for id, v := range s.Documents {
		n.Documents[id] = v
	}
	n.DocumentRevisions = make(map[ID][]DocumentRevision, len(s.DocumentRevisions))
	for id, revisions := range s.DocumentRevisions {
		n.DocumentRevisions[id] = revisions // immutable until replaced, never appended in place
	}
	return n
}

// cloneState is the strict deep-detachment boundary for public reads and images.
func cloneState(s Snapshot, l Limits) (Snapshot, error) {
	n := candidateTables(s)
	for id, v := range s.Entries {
		x, e := copyObject(v.Value, l)
		if e != nil {
			return Snapshot{}, e
		}
		v.Value = x
		n.Entries[id] = v
	}
	for id, v := range s.Tasks {
		x, e := copyObject(v.Checkpoint, l)
		if e != nil {
			return Snapshot{}, e
		}
		v.Checkpoint = x
		n.Tasks[id] = v
	}
	for id, v := range s.Submissions {
		x, e := copyObject(v.Value, l)
		if e != nil {
			return Snapshot{}, e
		}
		v.Value = x
		n.Submissions[id] = v
	}
	for id, v := range s.Documents {
		x, e := copyObject(v.Value, l)
		if e != nil {
			return Snapshot{}, e
		}
		v.Value = x
		n.Documents[id] = v
	}
	for id, revisions := range s.DocumentRevisions {
		owned := make([]DocumentRevision, len(revisions))
		for i, revision := range revisions {
			if revision.Kind == "delta" {
				x, e := ownOperations(revision.Ops, l)
				if e != nil {
					return Snapshot{}, e
				}
				revision.Ops = x
			} else {
				x, e := copyObject(revision.Value, l)
				if e != nil {
					return Snapshot{}, e
				}
				revision.Value = x
			}
			owned[i] = revision
		}
		n.DocumentRevisions[id] = owned
	}
	return n, nil
}
func (c *storeCore) Snapshot(ctx context.Context) (Snapshot, error) {
	if err := c.enter(ctx); err != nil {
		return Snapshot{}, err
	}
	defer c.leave()
	return cloneState(c.state, c.limits)
}

func prepare(s Snapshot, r commitRecord, l Limits) (Snapshot, error) {
	return prepareWithReferences(s, r, l, true)
}
func prepareWithReferences(s Snapshot, r commitRecord, l Limits, final bool) (Snapshot, error) {
	if s.Seq >= MaxID || r.Seq != s.Seq+1 || r.Seq > MaxID {
		return Snapshot{}, reject("invalid commit sequence")
	}
	if len(r.Writes) == 0 || len(r.Writes) > l.MaxWrites {
		return Snapshot{}, reject("invalid write count")
	}
	resolved, err := resolveDocumentCopies(s, r.Writes, l)
	if err != nil {
		return Snapshot{}, err
	}
	r.Writes = resolved
	n := candidateTables(s)
	n.Seq = r.Seq
	used := map[ID]string{}
	for id := range n.Conversations {
		used[id] = "conversation"
	}
	for id := range n.Entries {
		used[id] = "entry"
	}
	for id := range n.Tasks {
		used[id] = "task"
	}
	for id := range n.Submissions {
		used[id] = "submission"
	}
	for id := range n.Documents {
		used[id] = "document"
	}
	retirements := map[ID]Document{}
	deltaContent := map[ID]bool{}
	baseContent := map[ID]bool{}
	for i, w := range r.Writes {
		if _, err = encodeBounded(w, l, l.MaxRecordBytes); err != nil {
			return Snapshot{}, err
		}
		count := 0
		for _, present := range []bool{w.Conversation != nil, w.Entry != nil, w.Task != nil, w.Submission != nil, w.Document != nil, w.Delta != nil} {
			if present {
				count++
			}
		}
		if count != 1 || w.Source != nil {
			return Snapshot{}, reject("write union requires one record")
		}
		var id ID
		var kind string
		var replace bool
		switch w.Op {
		case "create-conversation":
			if w.Conversation == nil {
				return Snapshot{}, reject("write union mismatch")
			}
			id = w.Conversation.ID
			kind = "conversation"
		case "append-entry":
			if w.Entry == nil {
				return Snapshot{}, reject("write union mismatch")
			}
			id = w.Entry.ID
			kind = "entry"
		case "put-task":
			if w.Task == nil {
				return Snapshot{}, reject("write union mismatch")
			}
			id = w.Task.ID
			kind = "task"
			replace = true
		case "put-submission":
			if w.Submission == nil {
				return Snapshot{}, reject("write union mismatch")
			}
			id = w.Submission.ID
			kind = "submission"
			replace = true
		case "delta-document":
			if w.Delta == nil {
				return Snapshot{}, reject("write union mismatch")
			}
			id, kind, replace = w.Delta.ID, "document", true
		case "put-document", "retire-document":
			if w.Document == nil {
				return Snapshot{}, reject("write union mismatch")
			}
			id = w.Document.ID
			kind = "document"
			replace = true
		default:
			return Snapshot{}, fmt.Errorf("%w: command", ErrUnsupported)
		}
		if id < 1 || uint64(id) > MaxID || uint64(id) > n.HighWater {
			return Snapshot{}, reject("unreserved ID")
		}
		old, exists := used[id]
		if exists && (!replace || old != kind) {
			return Snapshot{}, reject("global ID collision or immutable record")
		}
		used[id] = kind
		switch w.Op {
		case "create-conversation":
			v := *w.Conversation
			if v.Parent != 0 && v.ParentAt == 0 {
				return Snapshot{}, fmt.Errorf("%w: fork requires an entry cutoff", ErrUnsupported)
			}
			n.Conversations[id] = v
		case "append-entry":
			v := *w.Entry
			if v.Seq != 0 || v.Position != 0 {
				return Snapshot{}, reject("entry placement is storage-owned")
			}
			v.Seq = r.Seq
			v.Position = uint64(i + 1)
			n.Entries[id] = v
		case "put-task":
			v := *w.Task
			if old, ok := n.Tasks[id]; ok && (old.Conversation != v.Conversation || old.Kind != v.Kind) {
				return Snapshot{}, reject("task identity changed")
			}
			n.Tasks[id] = v
		case "put-submission":
			v := *w.Submission
			if old, ok := n.Submissions[id]; ok && (old.Conversation != v.Conversation || old.RequestID != v.RequestID || old.Type != v.Type) {
				return Snapshot{}, reject("submission identity changed")
			}
			n.Submissions[id] = v
		case "delta-document":
			if deltaContent[id] || baseContent[id] {
				return Snapshot{}, reject("delta has multiple content commands")
			}
			deltaContent[id] = true
			delta := w.Delta
			v, ok := n.Documents[id]
			if !ok || v.Retired || delta.Version != v.Version || len(delta.Ops) == 0 {
				return Snapshot{}, reject("delta requires a live same-version base and nonempty ops")
			}
			value, e := ApplyOperations(v.Value, delta.Ops, l)
			if e != nil {
				return Snapshot{}, e
			}
			object, ok := value.(map[string]any)
			if !ok {
				return Snapshot{}, reject("delta document root must be object")
			}
			v.Value = JSON(object)
			if v.DeltasSinceBase >= MaxID {
				return Snapshot{}, reject("delta count exhausted")
			}
			v.DeltasSinceBase++
			n.Documents[id] = v
			revisions := append([]DocumentRevision(nil), n.DocumentRevisions[id]...)
			if len(revisions) == 0 {
				// Legacy latest-only bases were materialised without a tail table.
				old := s.Documents[id]
				revisions = append(revisions, DocumentRevision{Seq: old.CreatedAt, Version: old.Version, Value: old.Value})
			}
			n.DocumentRevisions[id] = append(revisions, DocumentRevision{Seq: r.Seq, Version: v.Version, Kind: "delta", Ops: delta.Ops})
		case "put-document":
			if deltaContent[id] {
				return Snapshot{}, reject("delta has multiple content commands")
			}
			baseContent[id] = true
			v := *w.Document
			if v.DeltasSinceBase != 0 {
				return Snapshot{}, reject("delta count is storage-owned")
			}
			v.DeltasSinceBase = 0
			if v.Retired {
				return Snapshot{}, reject("use explicit retirement")
			}
			if err := validDocumentPolicy(v); err != nil {
				return Snapshot{}, err
			}
			if old, ok := n.Documents[id]; ok {
				if old.Retired || !sameAddress(old, v) || documentHistory(old) != documentHistory(v) || documentFork(old) != documentFork(v) || (v.CreatedAt != 0 && v.CreatedAt != old.CreatedAt) || v.RetiredAt != 0 {
					return Snapshot{}, reject("document identity changed or retired")
				}
				v.CreatedAt = old.CreatedAt
			} else {
				if v.CreatedAt != 0 || v.RetiredAt != 0 {
					return Snapshot{}, reject("document lifecycle is storage-owned")
				}
				v.CreatedAt = r.Seq
			}
			n.Documents[id] = v
			revision := DocumentRevision{Seq: r.Seq, Version: v.Version, Value: v.Value}
			if documentHistory(v) == "rewindable" {
				revisions := append([]DocumentRevision(nil), n.DocumentRevisions[id]...)
				if len(revisions) > 0 && revisions[len(revisions)-1].Seq == r.Seq {
					revisions = revisions[:len(revisions)-1]
				}
				n.DocumentRevisions[id] = append(revisions, revision)
			} else {
				// A checkpoint discards only a latest-only tail. Keep its newest
				// full base so later delta count/replay is storage-derived.
				if _, retained := n.DocumentRevisions[id]; retained {
					n.DocumentRevisions[id] = []DocumentRevision{revision}
				}
			}
		case "retire-document":
			if _, duplicate := retirements[id]; duplicate {
				return Snapshot{}, reject("duplicate retirement")
			}
			retirements[id] = *w.Document
		}
	}
	// Retirement and content form one lifecycle action, independent of write
	// order. Preserve original entry positions while stamping empty lifetimes.
	for id, retirement := range retirements {
		v, ok := n.Documents[id]
		if !ok || v.Retired || !sameAddress(v, retirement) {
			return Snapshot{}, reject("unknown document incarnation")
		}
		v.Retired, v.RetiredAt = true, r.Seq
		n.Documents[id] = v
		if documentHistory(v) != "rewindable" {
			delete(n.DocumentRevisions, id)
		}
	}
	if err = validateState(n, l, final); err != nil {
		return Snapshot{}, err
	}
	return n, nil
}
func sameAddress(a, b Document) bool {
	return documentAddress(a) == documentAddress(b)
}
func validKind(s string) bool { return kindPattern.MatchString(s) }
func validStatus(s string) bool {
	switch s {
	case "pending", "running", "waiting", "completing", "done", "failed", "aborted":
		return true
	}
	return false
}
func validateState(s Snapshot, l Limits, final bool) error {
	revisionCount := 0
	for _, revisions := range s.DocumentRevisions {
		revisionCount += len(revisions)
	}
	if len(s.Conversations)+len(s.Entries)+len(s.Tasks)+len(s.Submissions)+len(s.Documents)+revisionCount > l.MaxRecords {
		return reject("live record limit")
	}
	existsConv := func(id ID) bool { _, ok := s.Conversations[id]; return ok }
	existsTask := func(id ID) bool { _, ok := s.Tasks[id]; return ok }
	for _, v := range s.Conversations {
		if uint64(v.Owner) > MaxID || (final && v.Owner != 0 && !existsTask(v.Owner)) {
			return reject("conversation owner missing")
		}
	}
	if err := validateAncestry(s, final); err != nil {
		return err
	}
	for _, v := range s.Entries {
		if uint64(v.Conversation) > MaxID || uint64(v.Head) > MaxID || (final && !existsConv(v.Conversation)) || !validKind(v.Kind) || v.Value == nil {
			return reject("invalid entry")
		}
		if v.Head != 0 && (v.Head > v.ID || (final && !entryVisible(s, v.Conversation, v.Head))) {
			return reject("entry head is not visible at marker")
		}
	}
	for _, v := range s.Tasks {
		if uint64(v.Conversation) > MaxID || uint64(v.Owner) > MaxID || (final && !existsConv(v.Conversation)) || !validKind(v.Kind) || !validStatus(v.Status) || v.Checkpoint == nil || v.Owner == v.ID || (final && v.Owner != 0 && !existsTask(v.Owner)) {
			return reject("invalid task")
		}
	}
	// Ownership edges cannot contain cycles, including forward same-batch edges.
	for id := range s.Tasks {
		seen := map[ID]bool{}
		for id != 0 {
			if seen[id] {
				return reject("task ownership cycle")
			}
			seen[id] = true
			id = s.Tasks[id].Owner
		}
	}
	type requestKey struct {
		conversation ID
		request      string
	}
	requests := map[requestKey]ID{}
	for _, v := range s.Submissions {
		if uint64(v.Conversation) > MaxID || (final && !existsConv(v.Conversation)) || (v.Type != "follow-up" && v.Type != "write") || !validStatus(v.Status) || v.Value == nil || len(v.RequestID) > l.MaxRequestIDBytes {
			return reject("invalid submission")
		}
		if v.RequestID != "" {
			key := requestKey{v.Conversation, v.RequestID}
			if _, ok := requests[key]; ok {
				return reject("request ID conflict")
			}
			requests[key] = v.ID
		}
	}
	addresses := map[DocumentAddress]ID{}
	for _, v := range s.Documents {
		if !validKind(v.Kind) || v.Version < 1 || v.Version > MaxID || v.Value == nil || v.CreatedAt == 0 || v.CreatedAt > s.Seq || v.RetiredAt > s.Seq || (v.Retired != (v.RetiredAt != 0)) || (v.RetiredAt != 0 && v.RetiredAt < v.CreatedAt) {
			return reject("invalid document")
		}
		if err := validDocumentPolicy(v); err != nil {
			return err
		}
		switch v.Scope {
		case "session":
			if v.Owner != 0 {
				return reject("session scope owner")
			}
		case "conversation":
			if uint64(v.Owner) > MaxID || (final && !existsConv(v.Owner)) {
				return reject("conversation document owner missing")
			}
		case "task":
			if uint64(v.Owner) > MaxID || (final && !existsTask(v.Owner)) {
				return reject("task document owner missing")
			}
		default:
			return reject("invalid scope")
		}
		if _, err := encodeBounded(v.Value, l, l.MaxDocumentBytes); err != nil {
			return err
		}
		if !v.Retired {
			key := documentAddress(v)
			if _, ok := addresses[key]; ok {
				return reject("duplicate current document address")
			}
			addresses[key] = v.ID
		}
	}
	var retained int64
	add := func(v any) error {
		p, e := encodeBounded(v, l, l.MaxRecordBytes)
		if e != nil {
			return e
		}
		retained += int64(len(p))
		if retained > l.MaxRetainedBytes {
			return reject("retained data limit")
		}
		return nil
	}
	for _, v := range s.Conversations {
		if e := add(v); e != nil {
			return e
		}
	}
	for _, v := range s.Entries {
		if e := add(v); e != nil {
			return e
		}
	}
	for _, v := range s.Tasks {
		if e := add(v); e != nil {
			return e
		}
	}
	for _, v := range s.Submissions {
		if e := add(v); e != nil {
			return e
		}
	}
	for _, v := range s.Documents {
		if e := add(v); e != nil {
			return e
		}
	}
	for id, revisions := range s.DocumentRevisions {
		if _, ok := s.Documents[id]; !ok {
			return reject("revision document missing")
		}
		var previous uint64
		var version uint64
		var count uint64
		var value JSON
		for _, revision := range revisions {
			if revision.Seq == 0 || revision.Seq <= previous || revision.Seq > s.Seq || revision.Version == 0 || revision.Version > MaxID || (revision.Kind != "delta" && revision.Value == nil) {
				return reject("invalid document revision")
			}
			previous = revision.Seq
			if revision.Kind == "delta" {
				if revision.Value != nil || len(revision.Ops) == 0 {
					return reject("invalid delta revision")
				}
				if value == nil || revision.Version != version {
					return reject("delta revision requires a same-version base")
				}
				applied, e := ApplyOperations(value, revision.Ops, l)
				if e != nil {
					return e
				}
				object, ok := applied.(map[string]any)
				if !ok {
					return reject("delta revision root must be object")
				}
				value = JSON(object)
				count++
			} else {
				if revision.Kind != "" && revision.Kind != "base" || len(revision.Ops) != 0 {
					return reject("invalid base revision")
				}
				if _, e := encodeBounded(revision.Value, l, l.MaxDocumentBytes); e != nil {
					return e
				}
				value, version, count = revision.Value, revision.Version, 0
			}
			if e := add(revision); e != nil {
				return e
			}
		}
		if len(revisions) > 0 {
			d := s.Documents[id]
			left, e := ownJSONValue(d.Value, l)
			if e != nil {
				return e
			}
			right, e := ownJSONValue(value, l)
			if e != nil {
				return e
			}
			if version != d.Version || count != d.DeltasSinceBase || !equalDeltaJSON(left, right) {
				return reject("document materialisation/count disagrees with revisions")
			}
		}
	}
	return nil
}
func checkPage(l Limits, n int) error {
	if n < 1 || n > l.MaxPage {
		return reject("page limit outside persisted bounds")
	}
	return nil
}
func ids[V any](m map[ID]V) []ID {
	r := make([]ID, 0, len(m))
	for id := range m {
		r = append(r, id)
	}
	sort.Slice(r, func(i, j int) bool { return r[i] < r[j] })
	return r
}
func (c *storeCore) Conversations(ctx context.Context, q Query) ([]Conversation, error) {
	s, e := c.Snapshot(ctx)
	if e != nil {
		return nil, e
	}
	if e = checkPage(c.limits, q.Limit); e != nil {
		return nil, e
	}
	r := []Conversation{}
	for _, id := range ids(s.Conversations) {
		v := s.Conversations[id]
		if id <= q.After || (q.Owner != 0 && v.Owner != q.Owner) || (q.Conversation != 0 && id != q.Conversation) {
			continue
		}
		r = append(r, v)
		if len(r) == q.Limit {
			break
		}
	}
	return r, nil
}
func (c *storeCore) Entries(ctx context.Context, conversation ID, after EntryCursor, limit int) ([]Entry, error) {
	s, e := c.Snapshot(ctx)
	if e != nil {
		return nil, e
	}
	if e = checkPage(c.limits, limit); e != nil {
		return nil, e
	}
	if _, ok := s.Conversations[conversation]; !ok {
		return nil, reject("unknown conversation")
	}
	if (after.Conversation != 0 && after.Conversation != conversation) || after.Seq > MaxID || after.Position > MaxID {
		return nil, reject("entry cursor mismatch")
	}
	r := []Entry{}
	for _, v := range s.Entries {
		if v.Conversation == conversation && (v.Seq > after.Seq || (v.Seq == after.Seq && v.Position > after.Position)) {
			r = append(r, v)
		}
	}
	sort.Slice(r, func(i, j int) bool {
		if r[i].Seq != r[j].Seq {
			return r[i].Seq < r[j].Seq
		}
		return r[i].Position < r[j].Position
	})
	if len(r) > limit {
		r = r[:limit]
	}
	return r, nil
}
func (c *storeCore) Tasks(ctx context.Context, q Query) ([]Task, error) {
	s, e := c.Snapshot(ctx)
	if e != nil {
		return nil, e
	}
	if e = checkPage(c.limits, q.Limit); e != nil {
		return nil, e
	}
	r := []Task{}
	for _, id := range ids(s.Tasks) {
		v := s.Tasks[id]
		if id <= q.After || (q.Conversation != 0 && v.Conversation != q.Conversation) || (q.Owner != 0 && v.Owner != q.Owner) || (q.Status != "" && v.Status != q.Status) || (q.Kind != "" && v.Kind != q.Kind) {
			continue
		}
		r = append(r, v)
		if len(r) == q.Limit {
			break
		}
	}
	return r, nil
}
func (c *storeCore) Submissions(ctx context.Context, q Query) ([]Submission, error) {
	s, e := c.Snapshot(ctx)
	if e != nil {
		return nil, e
	}
	if e = checkPage(c.limits, q.Limit); e != nil {
		return nil, e
	}
	r := []Submission{}
	for _, id := range ids(s.Submissions) {
		v := s.Submissions[id]
		if id <= q.After || (q.Conversation != 0 && v.Conversation != q.Conversation) || (q.Status != "" && v.Status != q.Status) {
			continue
		}
		r = append(r, v)
		if len(r) == q.Limit {
			break
		}
	}
	return r, nil
}
func (c *storeCore) Documents(ctx context.Context, q Query) ([]Document, error) {
	s, e := c.Snapshot(ctx)
	if e != nil {
		return nil, e
	}
	if e = checkPage(c.limits, q.Limit); e != nil {
		return nil, e
	}
	r := []Document{}
	for _, id := range ids(s.Documents) {
		v := s.Documents[id]
		if id <= q.After || v.Retired || (q.Scope != "" && v.Scope != q.Scope) || (q.Owner != 0 && v.Owner != q.Owner) || (q.Kind != "" && v.Kind != q.Kind) || (q.Key != nil && v.Key != *q.Key) {
			continue
		}
		r = append(r, v)
		if len(r) == q.Limit {
			break
		}
	}
	return r, nil
}
func (c *storeCore) Request(ctx context.Context, conversation ID, request string) (Submission, bool, error) {
	s, e := c.Snapshot(ctx)
	if e != nil {
		return Submission{}, false, e
	}
	if request == "" || len(request) > c.limits.MaxRequestIDBytes {
		return Submission{}, false, reject("invalid request ID")
	}
	for _, v := range s.Submissions {
		if v.Conversation == conversation && v.RequestID == request {
			return v, true, nil
		}
	}
	return Submission{}, false, nil
}
