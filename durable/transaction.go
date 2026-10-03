package durable

import (
	"context"
	"sync/atomic"
)

// Tx is synchronous, single-owner and callback-scoped. No mutable staged-state
// accessor exists. Reentrant/overlapping methods reject; escaped methods reject
// after settlement. Concurrent mutation DURING copy is unsupported Go misuse.
type Tx struct {
	store  *storeCore
	ctx    context.Context
	limits Limits
	state  Snapshot
	writes []Write
	busy   atomic.Bool
	sealed atomic.Bool
}

func (t *Tx) enter() error {
	if t.sealed.Load() {
		return ErrSealed
	}
	if !t.busy.CompareAndSwap(false, true) {
		return ErrConcurrent
	}
	if t.sealed.Load() {
		t.busy.Store(false)
		return ErrSealed
	}
	return nil
}
func (t *Tx) leave() { t.busy.Store(false) }
func (t *Tx) seal() error {
	t.sealed.Store(true)
	if t.busy.Load() {
		return ErrConcurrent
	}
	return nil
}
func (t *Tx) stage(w Write) error {
	// Every method detaches before returning, independently of final seal.
	p, e := encodeBounded(w, t.limits, t.limits.MaxRecordBytes)
	if e != nil {
		return e
	}
	var owned Write
	if e = decodeStrict(p, t.limits, t.limits.MaxRecordBytes, &owned); e != nil {
		return e
	}
	candidate := append(append([]Write(nil), t.writes...), owned)
	if _, e = encodeBounded(commitRecord{Seq: t.state.Seq + 1, Writes: candidate}, t.limits, t.limits.MaxFramePayloadBytes); e != nil {
		return e
	}
	if _, e = prepareWithReferences(t.state, commitRecord{Seq: t.state.Seq + 1, Writes: candidate}, t.limits, false); e != nil {
		return e
	}
	t.writes = candidate
	return nil
}
func (t *Tx) MintID() (ID, error) {
	if e := t.enter(); e != nil {
		return 0, e
	}
	defer t.leave()
	id, e := t.store.mintID(t.ctx, true)
	if e == nil {
		t.state.HighWater = uint64(id)
	}
	return id, e
}
func (t *Tx) CreateConversation(v Conversation) error {
	if e := t.enter(); e != nil {
		return e
	}
	defer t.leave()
	return t.stage(Write{Op: "create-conversation", Conversation: &v})
}
func (t *Tx) AppendEntry(v Entry) error {
	if e := t.enter(); e != nil {
		return e
	}
	defer t.leave()
	if _, e := copyObject(v.Value, t.limits); e != nil {
		return e
	}
	return t.stage(Write{Op: "append-entry", Entry: &v})
}
func (t *Tx) PutTask(v Task) error {
	if e := t.enter(); e != nil {
		return e
	}
	defer t.leave()
	if _, e := copyObject(v.Checkpoint, t.limits); e != nil {
		return e
	}
	return t.stage(Write{Op: "put-task", Task: &v})
}
func (t *Tx) PutSubmission(v Submission) error {
	if e := t.enter(); e != nil {
		return e
	}
	defer t.leave()
	if _, e := copyObject(v.Value, t.limits); e != nil {
		return e
	}
	return t.stage(Write{Op: "put-submission", Submission: &v})
}
func (t *Tx) current() (Snapshot, error) {
	return prepareWithReferences(t.state, commitRecord{Seq: t.state.Seq + 1, Writes: t.writes}, t.limits, false)
}
func (t *Tx) currentDocument(id ID) (Document, error) {
	var s Snapshot
	var e error
	if len(t.writes) == 0 {
		s = t.state
	} else {
		s, e = t.current()
		if e != nil {
			return Document{}, e
		}
	}
	v, ok := s.Documents[id]
	if !ok || v.Retired {
		return Document{}, reject("document not current")
	}
	return v, nil
}

// DocumentHandle binds exact ID/scope/kind/key/version identity to one Tx.
type DocumentHandle struct {
	tx        *Tx
	id        ID
	scope     string
	owner     ID
	kind, key string
	version   uint64
}

func (t *Tx) CreateDocument(v Document) (*DocumentHandle, error) {
	if e := t.enter(); e != nil {
		return nil, e
	}
	defer t.leave()
	if _, e := copyObject(v.Value, t.limits); e != nil {
		return nil, e
	}
	if e := t.stage(Write{Op: "put-document", Document: &v}); e != nil {
		return nil, e
	}
	return &DocumentHandle{t, v.ID, v.Scope, v.Owner, v.Kind, v.Key, v.Version}, nil
}
func (t *Tx) Document(id ID) (*DocumentHandle, error) {
	if e := t.enter(); e != nil {
		return nil, e
	}
	defer t.leave()
	v, e := t.currentDocument(id)
	if e != nil {
		return nil, e
	}
	return &DocumentHandle{t, v.ID, v.Scope, v.Owner, v.Kind, v.Key, v.Version}, nil
}
func (h *DocumentHandle) record() (Document, error) {
	v, e := h.tx.currentDocument(h.id)
	if e != nil {
		return Document{}, e
	}
	if v.Scope != h.scope || v.Owner != h.owner || v.Kind != h.kind || v.Key != h.key || v.Version != h.version {
		return Document{}, reject("document token mismatch")
	}
	return v, nil
}
func (h *DocumentHandle) Get() (JSON, error) {
	if e := h.tx.enter(); e != nil {
		return nil, e
	}
	defer h.tx.leave()
	v, e := h.record()
	if e != nil {
		return nil, e
	}
	return copyObject(v.Value, h.tx.limits)
}
func (h *DocumentHandle) Set(value JSON) error {
	if e := h.tx.enter(); e != nil {
		return e
	}
	defer h.tx.leave()
	owned, e := copyObject(value, h.tx.limits)
	if e != nil {
		return e
	}
	v, e := h.record()
	if e != nil {
		return e
	}
	v.Value = owned
	return h.tx.stage(Write{Op: "put-document", Document: &v})
}
func (h *DocumentHandle) Update(callback func(JSON) error) error {
	if e := h.tx.enter(); e != nil {
		return e
	}
	defer h.tx.leave()
	if callback == nil {
		return reject("nil Update callback")
	}
	v, e := h.record()
	if e != nil {
		return e
	}
	candidate, e := copyObject(v.Value, h.tx.limits)
	if e != nil {
		return e
	}
	if e = callback(candidate); e != nil {
		return e
	}
	owned, e := copyObject(candidate, h.tx.limits)
	if e != nil {
		return e
	}
	v.Value = owned
	return h.tx.stage(Write{Op: "put-document", Document: &v})
}
func (h *DocumentHandle) Retire() error {
	if e := h.tx.enter(); e != nil {
		return e
	}
	defer h.tx.leave()
	v, e := h.record()
	if e != nil {
		return e
	}
	return h.tx.stage(Write{Op: "retire-document", Document: &v})
}

// Unsupported historical/delta/copy calls reject explicitly; no success stubs.
func (t *Tx) Unsupported(command string) error {
	if e := t.enter(); e != nil {
		return e
	}
	defer t.leave()
	return ErrUnsupported
}
