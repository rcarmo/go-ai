package durable

import (
	"context"
)

// DocumentScope is exact: session requires Owner0; conversation/task require a
// nonzero owner. It never treats zero as a wildcard.
type DocumentScope struct {
	Scope string `json:"scope"`
	Owner ID     `json:"owner,omitempty"`
}

type DocumentAddress struct {
	Scope  string `json:"scope"`
	Owner  ID     `json:"owner,omitempty"`
	Kind   string `json:"kind"`
	Key    string `json:"key,omitempty"`
	Family bool   `json:"family,omitempty"`
}

type DocumentQuery struct {
	Scope string        `json:"scope"`
	Owner ID            `json:"owner,omitempty"`
	At    DocumentPoint `json:"at"`
	Kind  string        `json:"kind,omitempty"`
}

// DocumentHistoryStorage adds exact point reads and lifecycle membership to
// Storage's legacy current-record queries.
type DocumentHistoryStorage interface {
	DocumentAt(context.Context, ID, DocumentPoint) (Document, bool, error)
	FindDocument(context.Context, DocumentAddress, DocumentPoint) (Document, bool, error)
	ScanDocuments(context.Context, DocumentQuery, int, Cursor) (Page[Document], error)
}

func documentHistory(d Document) string {
	if d.Scope != "conversation" || d.History == "" {
		return "latest"
	}
	return d.History
}
func documentFork(d Document) string {
	if d.Fork == "" {
		return "initial"
	}
	return d.Fork
}
func documentFamily(d Document) bool { return d.Family || d.Key != "" }
func documentAddress(d Document) DocumentAddress {
	return DocumentAddress{d.Scope, d.Owner, d.Kind, d.Key, documentFamily(d)}
}
func validDocumentPolicy(d Document) error {
	if d.Scope != "conversation" {
		if d.History != "" || d.Fork != "" {
			return reject("history/fork policy requires conversation scope")
		}
		return nil
	}
	h, f := documentHistory(d), documentFork(d)
	if h != "latest" && h != "rewindable" {
		return reject("invalid document history policy")
	}
	if f != "initial" && f != "current" && f != "asOf" {
		return reject("invalid document fork policy")
	}
	if f == "asOf" && h != "rewindable" {
		return reject("asOf policy requires rewindable history")
	}
	return nil
}
func validateDocumentPoint(at DocumentPoint) error {
	if at.Seq > MaxID || (at.Current && at.Seq != 0) {
		return storageRequest("invalid document point")
	}
	return nil
}
func aliveDocument(d Document, at DocumentPoint) bool {
	if at.Current {
		return !d.Retired
	}
	return d.CreatedAt <= at.Seq && (d.RetiredAt == 0 || at.Seq < d.RetiredAt)
}

// materializeDocument reads only adopted private data and never consults a
// definition/registry/migration. The caller detaches before publishing the value.
func materializeDocument(s Snapshot, id ID, at DocumentPoint, l Limits) (Document, bool, error) {
	if err := validateDocumentPoint(at); err != nil {
		return Document{}, false, err
	}
	d, ok := s.Documents[id]
	if !ok {
		return Document{}, false, nil
	}
	if !at.Current && documentHistory(d) != "rewindable" {
		return Document{}, false, storageRequest("document does not retain historical content")
	}
	if !aliveDocument(d, at) {
		return Document{}, false, nil
	}
	revisions := s.DocumentRevisions[id]
	if len(revisions) == 0 && at.Current {
		return d, true, nil
	} // legacy latest base
	end := len(revisions)
	if !at.Current {
		for end > 0 && revisions[end-1].Seq > at.Seq {
			end--
		}
	}
	base := end - 1
	for base >= 0 && revisions[base].Kind == "delta" {
		base--
	}
	if base < 0 {
		return Document{}, false, reject("document is missing a required base")
	}
	r := revisions[base]
	d.Version, d.Value, d.DeltasSinceBase = r.Version, r.Value, 0
	for i := base + 1; i < end; i++ {
		tail := revisions[i]
		if tail.Kind != "delta" || tail.Version != d.Version {
			return Document{}, false, reject("delta crosses version boundary without base")
		}
		value, err := ApplyOperations(d.Value, tail.Ops, l)
		if err != nil {
			return Document{}, false, err
		}
		object, ok := value.(map[string]any)
		if !ok {
			return Document{}, false, reject("delta document root must be object")
		}
		d.Value = JSON(object)
		d.DeltasSinceBase++
	}
	return d, true, nil
}

func validateDocumentScope(scope DocumentScope) error {
	if uint64(scope.Owner) > MaxID {
		return reject("invalid document owner")
	}
	switch scope.Scope {
	case "session":
		if scope.Owner != 0 {
			return reject("session scope owner")
		}
	case "conversation", "task":
		if scope.Owner == 0 {
			return reject("scope owner required")
		}
	default:
		return reject("invalid document scope")
	}
	return nil
}

func (c *storeCore) DocumentAt(ctx context.Context, id ID, at DocumentPoint) (Document, bool, error) {
	if err := c.enter(ctx); err != nil {
		return Document{}, false, err
	}
	defer c.leave()
	if id == 0 || uint64(id) > MaxID {
		return Document{}, false, storageRequest("invalid document ID")
	}
	d, ok, err := materializeDocument(c.state, id, at, c.limits)
	if err != nil || !ok {
		return d, ok, err
	}
	d.Value, err = copyObject(d.Value, c.limits)
	return d, err == nil, err
}

func (c *storeCore) FindDocument(ctx context.Context, address DocumentAddress, at DocumentPoint) (Document, bool, error) {
	if err := c.enter(ctx); err != nil {
		return Document{}, false, err
	}
	defer c.leave()
	if err := requestValidation(validateDocumentScope(DocumentScope{address.Scope, address.Owner})); err != nil {
		return Document{}, false, err
	}
	if err := requestValidation(validateDocumentPoint(at)); err != nil {
		return Document{}, false, err
	}
	if !validKind(address.Kind) || (!address.Family && address.Key != "") {
		return Document{}, false, storageRequest("invalid document address")
	}
	for _, id := range ids(c.state.Documents) {
		d := c.state.Documents[id]
		if documentAddress(d) == address && aliveDocument(d, at) {
			// Membership works for current-only documents at historical points;
			// content is returned only by DocumentAt, which enforces its policy.
			d.Value = nil
			return d, true, nil
		}
	}
	return Document{}, false, nil
}

func (c *storeCore) ScanDocuments(ctx context.Context, q DocumentQuery, limit int, cursor Cursor) (Page[Document], error) {
	if err := c.enter(ctx); err != nil {
		return Page[Document]{}, err
	}
	defer c.leave()
	if err := requestValidation(validateDocumentScope(DocumentScope{q.Scope, q.Owner})); err != nil {
		return Page[Document]{}, err
	}
	if err := validateDocumentPoint(q.At); err != nil {
		return Page[Document]{}, err
	}
	if q.Kind != "" && !validKind(q.Kind) {
		return Page[Document]{}, storageRequest("invalid document kind")
	}
	if err := checkPage(c.limits, limit); err != nil {
		return Page[Document]{}, err
	}
	after, hasAfter, err := decodeDocumentCursor(cursor)
	if err != nil {
		return Page[Document]{}, err
	}
	page := Page[Document]{Items: []Document{}}
	for _, id := range ids(c.state.Documents) {
		d := c.state.Documents[id]
		if (hasAfter && int64(id) <= after) || d.Scope != q.Scope || d.Owner != q.Owner || (q.Kind != "" && d.Kind != q.Kind) || !aliveDocument(d, q.At) {
			continue
		}
		if len(page.Items) == limit {
			page.Next = Cursor{"after": float64(page.Items[len(page.Items)-1].ID)}
			return page, nil
		}
		d.Value = nil
		page.Items = append(page.Items, d)
	}
	return page, nil
}

// Document scans are ascending only; like the original they read only the
// cursor's "after" member.
func decodeDocumentCursor(cursor Cursor) (int64, bool, error) {
	return cursorAfter(cursor)
}

func resolveDocumentCopies(s Snapshot, writes []Write, l Limits, validateSources bool) ([]Write, error) {
	changed := map[ID]bool{}
	for _, w := range writes {
		if w.Document != nil {
			changed[w.Document.ID] = true
		}
		if w.Delta != nil {
			changed[w.Delta.ID] = true
		}
	}
	resolved := append([]Write(nil), writes...)
	copyTargets := map[ID]bool{}
	for i, w := range resolved {
		if w.Op != "copy-document" {
			if w.Source != nil {
				return nil, reject("copy source on non-copy command")
			}
			continue
		}
		if w.Document == nil || w.Source == nil || w.Conversation != nil || w.Entry != nil || w.Task != nil || w.Submission != nil || w.Delta != nil {
			return nil, reject("invalid document copy union")
		}
		if validateSources && changed[w.Source.ID] {
			return nil, reject("fork source document is changed in copy batch")
		}
		source, ok, err := materializeDocument(s, w.Source.ID, w.Source.At, l)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, reject("document copy source missing")
		}
		target := *w.Document
		if _, exists := s.Documents[target.ID]; exists || copyTargets[target.ID] {
			return nil, reject("copy target incarnation already exists")
		}
		for _, prior := range writes[:i] {
			if prior.Document != nil && prior.Document.ID == target.ID && prior.Op != "retire-document" {
				return nil, reject("copy target already staged")
			}
		}
		copyTargets[target.ID] = true
		if target.Scope != "conversation" || source.Scope != "conversation" || target.Kind != source.Kind || target.Key != source.Key || documentFamily(target) != documentFamily(source) || documentHistory(target) != documentHistory(source) || documentFork(target) != documentFork(source) || target.Value != nil || target.Version != 0 || target.CreatedAt != 0 || target.RetiredAt != 0 || target.Retired || target.DeltasSinceBase != 0 {
			return nil, reject("document copy source metadata mismatch")
		}
		// Source content is already privately owned immutable JSON. Preparation
		// shares it read-only; public/Tx reads detach before returning. Do not
		// copy every source into a potentially oversized intermediate batch.
		target.Value, target.Version = source.Value, source.Version
		resolved[i] = Write{Op: "put-document", Document: &target}
	}
	// Copy commands have a small wire shape but their materialized bases must
	// obey the same complete atomic-batch size limit as explicit base writes.
	if _, err := encodeBounded(commitRecord{Seq: s.Seq + 1, Writes: resolved}, l, l.MaxFramePayloadBytes); err != nil {
		return nil, err
	}
	return resolved, nil
}

// CopyDocument stages a definition-free base copy from committed state. It does
// not run a migration, validator or initializer; the stored version is preserved.
func (t *Tx) copyDocumentRecord(target Document, source DocumentCopySource) (*DocumentHandle, error) {
	if err := t.enter(); err != nil {
		return nil, err
	}
	defer t.leave()
	if err := t.stage(Write{Op: "copy-document", Document: &target, Source: &source}); err != nil {
		return nil, err
	}
	value, err := t.currentDocument(target.ID)
	if err != nil {
		return nil, err
	}
	return &DocumentHandle{t, value.ID, value.Scope, value.Owner, value.Kind, value.Key, value.Version}, nil
}

// forkConversationRecord stages the already selected child and every persisted conversation-document
// copy together. Sources are selected from adopted pre-batch state. A caller may
// retire/recreate child documents afterwards but cannot mutate a selected source.
// Initial-policy documents are acquired lazily by their host definitions.
func (t *Tx) forkConversationRecord(parent, at ID, owner ID) (Conversation, error) {
	if err := t.enter(); err != nil {
		return Conversation{}, err
	}
	defer t.leave()
	if !entryVisible(t.state, parent, at) {
		return Conversation{}, reject("fork entry is not visible from parent")
	}
	entry := t.state.Entries[at]
	id, err := t.store.mintID(t.ctx, true)
	if err != nil {
		return Conversation{}, err
	}
	t.state.HighWater = uint64(id)
	child := Conversation{ID: id, Owner: owner, Parent: parent, ParentAt: at}
	return t.stageForkConversation(child, entry)
}

func (t *Tx) stageForkConversation(child Conversation, entry Entry) (Conversation, error) {
	parent := child.Parent
	var err error
	// Retain staging rollback if source selection/assembly fails. Reservations
	// remain spent just like all other aborted transaction ID allocations.
	before := t.writes
	complete := false
	defer func() {
		if !complete {
			t.writes = before
		}
	}()
	if err = t.stage(Write{Op: "create-conversation", Conversation: &child}); err != nil {
		return Conversation{}, err
	}
	selected := map[DocumentAddress]bool{}
	for _, policy := range []string{"asOf", "current"} {
		owner := parent
		point := CurrentDocumentPoint()
		if policy == "asOf" {
			owner, point = entry.Conversation, DocumentPoint{Seq: entry.Seq}
		}
		for _, sourceID := range ids(t.state.Documents) {
			d := t.state.Documents[sourceID]
			if d.Scope != "conversation" || d.Owner != owner || documentFork(d) != policy || !aliveDocument(d, point) {
				continue
			}
			target := d
			target.Owner = child.ID
			address := documentAddress(target)
			if selected[address] {
				return Conversation{}, reject("fork selects multiple source documents for address")
			}
			selected[address] = true
			target.ID, err = t.store.mintID(t.ctx, true)
			if err != nil {
				return Conversation{}, err
			}
			t.state.HighWater = uint64(target.ID)
			target.Value, target.Version, target.CreatedAt, target.RetiredAt, target.Retired, target.DeltasSinceBase = nil, 0, 0, 0, false, 0
			copySource := DocumentCopySource{sourceID, point}
			if err = t.stage(Write{Op: "copy-document", Document: &target, Source: &copySource}); err != nil {
				return Conversation{}, err
			}
		}
	}
	if t.forkParents == nil {
		t.forkParents = map[ID]bool{}
	}
	t.forkParents[parent] = true
	if t.session != nil && t.session.taskScheduler != nil {
		t.leave()
		err := t.session.taskScheduler.h.initializeCreatedConversation(t, child)
		if enterErr := t.enter(); enterErr != nil {
			return Conversation{}, enterErr
		}
		if err != nil {
			return Conversation{}, err
		}
	}
	complete = true
	return child, nil
}

// Fork creates an ownerless child without scheduling work. Agent state is copied
// as of the cutoff; live state, inbox and the child's own usage start empty.
func (c *ConversationHandle) Fork(ctx context.Context, at ID) (*ConversationHandle, error) {
	return c.fork(ctx, at, nil, nil)
}
func (c *ConversationHandle) ForkWithInit(ctx context.Context, at ID, change *AgentChange, init ConversationInit) (*ConversationHandle, error) {
	return c.fork(ctx, at, change, init)
}
func (c *ConversationHandle) fork(ctx context.Context, at ID, change *AgentChange, init ConversationInit) (*ConversationHandle, error) {
	h := c.h
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing.Load() {
		return nil, ErrClosed
	}
	var child Conversation
	_, err := h.session.Commit(ctx, func(tx *Tx) (any, error) {
		if err := tx.touchKeys(ALL_KEYS); err != nil {
			return nil, err
		}
		if !entryVisible(tx.state, c.id, at) {
			return nil, reject("fork entry is not visible from parent")
		}
		entry := tx.state.Entries[at]
		// Old M1 agent metadata has latest-only semantics. Refuse unprovable
		// historical agent selection instead of substituting current settings.
		for _, d := range tx.state.Documents {
			if d.Scope == "conversation" && d.Owner == entry.Conversation && d.Kind == "pi.agent" && documentHistory(d) != "rewindable" {
				return nil, reject("historical agent requires retained rewindable history")
			}
		}
		var err error
		child, err = tx.forkConversationRecord(c.id, at, 0)
		if err != nil {
			return nil, err
		}
		if err := initializeBuiltins(tx, child.ID); err != nil {
			return nil, err
		}
		if change != nil {
			state, err := h.agent(*change)
			if err != nil {
				return nil, err
			}
			value, err := dtoObject(state, tx.limits)
			if err != nil {
				return nil, err
			}
			candidate, err := tx.current()
			if err != nil {
				return nil, err
			}
			doc, ok := agentDocument(candidate, child.ID)
			if !ok {
				return nil, reject("fork agent unavailable")
			}
			handle, err := tx.documentRecord(doc.ID)
			if err != nil {
				return nil, err
			}
			if err := handle.Set(value); err != nil {
				return nil, err
			}
		}
		if init != nil {
			return nil, callConversationInit(init, tx, child.ID)
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	return &ConversationHandle{h, child.ID}, nil
}
