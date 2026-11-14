package durable

import (
	"context"
	"sync/atomic"
)

// Tx is synchronous, single-owner and callback-scoped. No mutable staged-state
// accessor exists. Reentrant/overlapping methods reject; escaped methods reject
// after settlement. Concurrent mutation DURING copy is unsupported Go misuse.
type Tx struct {
	store             *storeCore
	session           *Session
	identity          *Tx // Session-owned object identity; copying/wrapping does not confer ownership
	submissions       map[ID]SubmissionRecord
	submissionOrder   []ID
	submissionChanges []transactionSubmissionChange
	ctx               context.Context
	limits            Limits
	state             Snapshot
	writes            []Write
	hasTableWrite     bool      // pinned Transaction.#hasTableWrite; failed table writes count too
	stagedState       *Snapshot // private immutable prepare result; exact write-slice identity
	stagedWrites      []Write
	publicationState  *Snapshot // sealed final candidate; reused only before storage admission
	creationError     error     // creation callback failure poisons only this transaction
	// Bound only by Harness task commits or invocation admission. Callers never
	// supply executable authority by copying a raw task record.
	taskConversation   ID
	progressTask       ID // process-local invocation whose state can reset restarts
	byTask             ID
	taskStops          []taskStop
	taskAdoptState     *Snapshot   // immutable confirmed storage view, private to adoption
	taskRollback       func(error) // trusted bounded bookkeeping BEFORE Session.leave
	taskTerminals      map[ID]bool // private builtin withdrawal cleanup; no legacy rewrite
	taskTraceRoots     map[ID]bool // normalized root directives, applied only after adoption
	externalTaskCommit bool        // public host transaction, not a scheduler/adapter pass
	// Immediate parents of successfully staged high-level forks. This fence is
	// broader than copy source IDs: new current-policy parent docs also reject.
	forkParents       map[ID]bool
	documentPlans     map[ID]*transactionDocumentPlan
	readonlyDocuments map[ID]readonlyDocumentRevision
	readDocuments     map[ID]bool // actual document access, separate from preparation scans
	memoryOnly        bool        // sealed eligible deferred-document publication
	keyedReads        bool        // Session methods touch dependencies before reading the base
	busy              atomic.Bool
	sealed            atomic.Bool
}

func (t *Tx) enter() error { return t.enterKeys(ALL_KEYS) }

func (t *Tx) enterKeys(keys ...string) error { return t.enterOperation("", keys...) }
func (t *Tx) enterTableRead(method string, keys ...string) error {
	return t.enterOperation(method, keys...)
}
func (t *Tx) enterOperation(method string, keys ...string) error {
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
	if method != "" && t.hasTableWrite {
		t.busy.Store(false)
		return &ReadAfterWrite{Method: method}
	}
	if err := t.touchKeys(keys...); err != nil {
		t.busy.Store(false)
		return err
	}
	return nil
}

// touchKeys is the Transaction host's gated read boundary. Staged changes stay
// private; after a dependency publishes, rebase their validation on the receipted
// image. A disjoint read does not drain unrelated admitted writes.
func (t *Tx) touchKeys(keys ...string) error {
	if !t.keyedReads || t.session == nil {
		return nil
	}
	for _, key := range keys {
		if err := t.session.touch(t.ctx, key); err != nil {
			return err
		}
	}
	image := t.session.memoryReadImage()
	if image.Seq != t.state.Seq {
		image.HighWater = max(image.HighWater, t.state.HighWater)
		t.state = image
		t.stagedState, t.stagedWrites, t.publicationState = nil, nil, nil
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
	switch w.Op {
	case "create-conversation", "append-entry", "put-task", "put-submission":
		t.hasTableWrite = true
	}
	// Retirement names an incarnation; its payload is already durable. Do not
	// duplicate chunk/checkpoint data into an aggregate terminal commit.
	if w.Op == "retire-document" && w.Document != nil {
		address := *w.Document
		address.Value = nil
		w.Document = &address
	}
	// Every method detaches before returning, independently of final seal.
	// Trusted DTO encoding cannot grant nested task JSON marshaler authority.
	if w.Entry != nil && t.byTask != 0 {
		entry := *w.Entry
		if entry.ByTask != 0 && entry.ByTask != t.byTask {
			return reject("runtime entry task attribution")
		}
		entry.ByTask = t.byTask
		w.Entry = &entry
	}
	if w.Task != nil {
		stamped := *w.Task
		prior := t.state.Tasks[stamped.ID]
		for _, write := range t.writes {
			if write.Task != nil && write.Task.ID == stamped.ID {
				prior = *write.Task
			}
		}
		if t.session != nil {
			stamped = stampTaskTimes(stamped, prior, t.session.now)
		}
		owned, e := copyTask(stamped, t.limits)
		if e != nil {
			return e
		}
		if owned.ID == t.progressTask && owned.Restarts != 0 && taskMadeProgress(prior, owned) {
			owned.Restarts = 0
		}
		w.Task = &owned
	}
	p, e := encodeBounded(w, t.limits, t.limits.MaxRecordBytes)
	if e != nil {
		return e
	}
	var owned Write
	if e = decodeStrict(p, t.limits, t.limits.MaxRecordBytes, &owned); e != nil {
		return e
	}
	candidate := append(append([]Write(nil), t.writes...), owned)
	for parent := range t.forkParents {
		if e = t.rejectCurrentForkWrites(candidate, parent); e != nil {
			return e
		}
	}
	if _, e = encodeBounded(commitRecord{Seq: t.state.Seq + 1, Writes: candidate}, t.limits, t.limits.MaxFramePayloadBytes); e != nil {
		return e
	}
	prepared, err := t.prepareCandidate(candidate, false)
	if err != nil {
		return err
	}
	t.writes = candidate
	t.stagedState = &prepared
	t.stagedWrites = candidate
	if owned.Op == "put-document" {
		d := *owned.Document
		plan := t.documentPlan(d.ID)
		base, exists := t.state.Documents[d.ID]
		if !exists || base.Version != d.Version {
			plan.requiredBase = true
		}
		for _, prior := range candidate {
			if prior.Op == "copy-document" && prior.Document.ID == d.ID {
				plan.requiredBase = true
			}
		}
		// stage's direct base API carries whole-root intent. Set/Update/explicit
		// operations can replace this private intent after staging succeeds.
		plan.ops = []Operation{{"r", map[string]any(d.Value)}}
	}
	return nil
}

// rejectCurrentForkWrites uses the persisted/staged document identity for
// retirement commands, whose token need not repeat the policy. All current
// parent content and retirements reject, even new or same-commit empty lifetimes.
func (t *Tx) rejectCurrentForkWrites(writes []Write, parent ID) error {
	for _, w := range writes {
		if w.Document == nil && w.Delta == nil {
			continue
		}
		var d Document
		if w.Delta != nil {
			d = t.state.Documents[w.Delta.ID]
		} else {
			d = *w.Document
		}
		if w.Op == "retire-document" {
			if stored, ok := t.state.Documents[d.ID]; ok {
				d = stored
			} else {
				for _, content := range writes {
					if content.Document != nil && content.Document.ID == d.ID && (content.Op == "put-document" || content.Op == "copy-document") {
						d = *content.Document
						break
					}
				}
			}
		}
		if d.Scope == "conversation" && d.Owner == parent && documentFork(d) == "current" {
			return reject("cannot fork while changing current-policy parent documents")
		}
	}
	return nil
}

func (t *Tx) MintID() (ID, error) {
	if e := t.enterKeys(); e != nil {
		return 0, e
	}
	defer t.leave()
	id, e := t.store.mintID(t.ctx, true)
	if t.session != nil {
		e = t.session.backendResult(t.ctx, e, true)
	}
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
	t.hasTableWrite = true
	before := t.writes
	if err := t.stage(Write{Op: "create-conversation", Conversation: &v}); err != nil {
		return err
	}
	if t.session != nil && t.session.taskScheduler != nil {
		// Nested Tx methods must enter without releasing the Session line.
		t.leave()
		err := t.session.taskScheduler.h.initializeCreatedConversation(t, v)
		if enterErr := t.enter(); enterErr != nil {
			return enterErr
		}
		if err != nil {
			t.writes = before
			t.creationError = err
			return err
		}
	}
	return nil
}

// requireConversation mirrors Transaction.#requireConversation: an own creation
// is visible immediately; otherwise the conversation's keyed committed record
// must already exist. Other reference checks remain Storage's responsibility.
func (t *Tx) requireConversation(id ID) error {
	for _, write := range t.writes {
		if write.Op == "create-conversation" && write.Conversation.ID == id {
			return nil
		}
	}
	if _, ok := t.state.Conversations[id]; !ok {
		return reject("conversation does not exist")
	}
	return nil
}

func (t *Tx) appendEntryRecord(v Entry) error {
	if e := t.enterKeys(conversationKey(v.Conversation)); e != nil {
		return e
	}
	defer t.leave()
	t.hasTableWrite = true
	if err := t.requireConversation(v.Conversation); err != nil {
		return err
	}
	owned, e := copyEntry(v, t.limits)
	if e != nil {
		return e
	}
	return t.stage(Write{Op: "append-entry", Entry: &owned})
}
func (t *Tx) PutTask(v Task) error {
	if e := t.enter(); e != nil {
		return e
	}
	defer t.leave()
	t.hasTableWrite = true
	current := t.state.Tasks[v.ID]
	if t.byTask != 0 && (v.Kind == "pi.tool" || v.Kind == "pi.generation" || current.Kind == "pi.tool" || current.Kind == "pi.generation") {
		return reject("runtime raw builtin task replacement")
	}
	if v.Execution != nil {
		return reject("execution task replacement requires invocation authority")
	}
	if current, exists := t.state.Tasks[v.ID]; exists && current.Execution != nil {
		return reject("execution task replacement requires invocation authority")
	}
	owned, e := copyTask(v, t.limits)
	if e != nil {
		return e
	}
	return t.stage(Write{Op: "put-task", Task: &owned})
}
func (t *Tx) PutSubmission(v Submission) error {
	if e := t.enter(); e != nil {
		return e
	}
	defer t.leave()
	t.hasTableWrite = true
	if _, e := copyObject(v.Value, t.limits); e != nil {
		return e
	}
	return t.stage(Write{Op: "put-submission", Submission: &v})
}
func (t *Tx) current() (Snapshot, error) {
	if len(t.writes) == 0 {
		return t.state, nil
	}
	if t.stagedState != nil && len(t.stagedWrites) == len(t.writes) && &t.stagedWrites[0] == &t.writes[0] {
		state := *t.stagedState
		state.HighWater = t.state.HighWater
		return state, nil
	}
	return t.prepareCandidate(t.writes, false)
}

// A transaction validates held values against their real durable history by
// folding them into its provisional next candidate. It never forges a revision
// at an already stored sequence or changes the Session's receipted image.
func (t *Tx) prepareCandidate(writes []Write, references bool) (Snapshot, error) {
	state := t.state
	if t.session != nil && len(t.session.heldDocuments) > 0 {
		selected := make(map[ID]bool, len(t.session.heldDocuments))
		for id := range t.session.heldDocuments {
			selected[id] = true
		}
		var err error
		writes, _, err = t.session.foldSelectedHeld(writes, selected)
		if err != nil {
			return Snapshot{}, err
		}
		state = t.session.readImage
		state.HighWater = t.state.HighWater
	}
	return prepareWithReferences(state, commitRecord{Seq: t.state.Seq + 1, Writes: writes}, t.limits, references)
}

func (t *Tx) currentDocument(id ID) (Document, error) {
	if t.session != nil && len(t.session.heldDocuments) > 0 {
		if t.readDocuments == nil {
			t.readDocuments = map[ID]bool{}
		}
		t.readDocuments[id] = true
	}
	var s Snapshot
	var e error
	changed := false
	for _, write := range t.writes {
		if write.Document != nil && write.Document.ID == id || write.Delta != nil && write.Delta.ID == id {
			changed = true
			break
		}
	}
	if !changed {
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
	// Document acquisition requires an existing or already-staged scope, as
	// Transaction's requireScope does. Do not defer missing owners to storage.
	state, err := t.current()
	if err != nil {
		return nil, err
	}
	if v.Scope == "conversation" {
		if _, ok := state.Conversations[v.Owner]; !ok {
			return nil, reject("document conversation missing")
		}
	}
	if v.Scope == "task" {
		owner, known := state.Tasks[v.Owner]
		if !known {
			return nil, reject("document task owner missing")
		}
		if _, exists := state.Documents[v.ID]; !exists && owner.Execution != nil && terminalStatus(owner.Status) {
			return nil, reject("native task document owner is terminal")
		}
	}
	if _, e := copyObject(v.Value, t.limits); e != nil {
		return nil, e
	}
	if e := t.stage(Write{Op: "put-document", Document: &v}); e != nil {
		return nil, e
	}
	return &DocumentHandle{t, v.ID, v.Scope, v.Owner, v.Kind, v.Key, v.Version}, nil
}
func (t *Tx) Document(id ID) (*DocumentHandle, error) {
	if e := t.enterKeys(incarnationKey(id)); e != nil {
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
	if e := h.tx.enterKeys(incarnationKey(h.id)); e != nil {
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
	if e := h.tx.enterKeys(incarnationKey(h.id)); e != nil {
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
	before, e := ownJSONValue(v.Value, h.tx.limits)
	if e != nil {
		return e
	}
	after, e := ownJSONValue(owned, h.tx.limits)
	if e != nil {
		return e
	}
	if equalDeltaJSON(before, after) {
		return nil
	}
	v.Value = owned
	v.DeltasSinceBase = 0
	if e = h.tx.stage(Write{Op: "put-document", Document: &v}); e != nil {
		return e
	}
	return nil
}
func (h *DocumentHandle) Update(callback func(JSON) error) error {
	if e := h.tx.enterKeys(incarnationKey(h.id)); e != nil {
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
	ops, e := diffOperations(v.Value, owned, h.tx.limits)
	if e != nil || len(ops) == 0 {
		return e
	}
	prior := append([]Operation{}, h.tx.documentPlan(v.ID).ops...)
	combined, e := ownOperations(append(prior, ops...), h.tx.limits)
	if e != nil {
		return e
	}
	v.Value = owned
	v.DeltasSinceBase = 0
	if e = h.tx.stage(Write{Op: "put-document", Document: &v}); e != nil {
		return e
	}
	h.tx.documentPlan(v.ID).ops = combined
	return nil
}

// ApplyOperations retains explicit structural intent, even for a nonempty
// batch whose final value equals the base. Failed batches stage nothing.
func (h *DocumentHandle) ApplyOperations(operations []Operation) error {
	if e := h.tx.enterKeys(incarnationKey(h.id)); e != nil {
		return e
	}
	defer h.tx.leave()
	v, e := h.record()
	if e != nil {
		return e
	}
	ops, e := ownOperations(operations, h.tx.limits)
	if e != nil || len(ops) == 0 {
		return e
	}
	value, e := ApplyOperations(v.Value, ops, h.tx.limits)
	if e != nil {
		return e
	}
	object, ok := value.(map[string]any)
	if !ok {
		return reject("delta document root must be object")
	}
	prior := append([]Operation{}, h.tx.documentPlan(v.ID).ops...)
	combined := append(prior, ops...)
	if _, e = ownOperations(combined, h.tx.limits); e != nil {
		return e
	}
	v.Value = JSON(object)
	v.DeltasSinceBase = 0
	if e = h.tx.stage(Write{Op: "put-document", Document: &v}); e != nil {
		return e
	}
	h.tx.documentPlan(v.ID).ops = combined
	h.tx.documentPlan(v.ID).explicit = true
	return nil
}
func (h *DocumentHandle) Retire() error {
	if e := h.tx.enterKeys(incarnationKey(h.id)); e != nil {
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

// Final content selection is private: public methods are already sealed when
// the Session invokes it. No callback can reopen a transaction's authority.
type transactionDocumentPlan struct {
	definition   *DocumentDefinition
	requiredBase bool
	ops          []Operation
	explicit     bool
}

func (t *Tx) documentPlan(id ID) *transactionDocumentPlan {
	if t.documentPlans == nil {
		t.documentPlans = map[ID]*transactionDocumentPlan{}
	}
	if p := t.documentPlans[id]; p != nil {
		return p
	}
	p := &transactionDocumentPlan{}
	t.documentPlans[id] = p
	return p
}

// finalizeTaskWrites operates after public Tx seal, before document predicates.
// It uses private stage only; no public capability is reopened.
func (t *Tx) finalizeTaskWrites() error {
	needsCandidate := len(t.taskTerminals) > 0
	for _, write := range t.writes {
		if write.Task != nil && write.Task.Execution != nil && (write.Task.Execution.Native != nil || terminalStatus(write.Task.Status)) ||
			write.Op == "create-conversation" && write.Conversation.Owner != 0 {
			needsCandidate = true
		}
		if write.Op == "put-task" && write.Task.Owner != 0 {
			if _, existing := t.state.Tasks[write.Task.ID]; !existing {
				needsCandidate = true
			}
		}
	}
	if !needsCandidate {
		return nil
	}
	candidate, err := t.current()
	if err != nil {
		return err
	}
	// Canonical Storage is a complete-record store. Validate runtime graph
	// restrictions here, on the final local candidate before writer admission,
	// so an invalid Session task never relies on a raw-storage refusal.
	for _, write := range t.writes {
		if write.Task == nil || write.Task.Execution == nil || write.Task.Execution.Native == nil {
			continue
		}
		task := candidate.Tasks[write.Task.ID]
		if err := validateTaskReferences(candidate, task, t.limits); err != nil {
			return err
		}
		if _, err := taskOwnerChain(candidate, task); err != nil {
			return err
		}
	}
	terminals := map[ID]bool{}
	for id := range t.taskTerminals {
		if task, ok := candidate.Tasks[id]; !ok || !terminalStatus(task.Status) {
			return reject("builtin terminal cleanup identity")
		}
		terminals[id] = true
	}
	for _, write := range t.writes {
		if write.Task != nil && write.Task.Execution != nil && terminalStatus(write.Task.Status) {
			terminals[write.Task.ID] = true
		}
		var owner ID
		if write.Op == "create-conversation" {
			owner = write.Conversation.Owner
		}
		if write.Op == "put-task" {
			if _, existing := t.state.Tasks[write.Task.ID]; !existing {
				owner = write.Task.Owner
			}
		}
		if owner != 0 {
			parent, exists := candidate.Tasks[owner]
			if !exists || !taskCanOwnNewWork(parent) {
				return reject("new work final owner is missing, decided or abort-marked")
			}
		}
	}
	retiring := map[ID]bool{}
	for _, write := range t.writes {
		if write.Op == "retire-document" {
			retiring[write.Document.ID] = true
		}
	}
	for _, id := range ids(candidate.Documents) {
		document := candidate.Documents[id]
		if document.Scope == "task" && terminals[document.Owner] && !document.Retired && !retiring[id] {
			if err := t.stage(Write{Op: "retire-document", Document: &document}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *Tx) finalizeDocuments() error {
	if len(t.writes) == 0 {
		return nil
	}
	// Prepare local document values before host predicates. Cross-record
	// references belong to Storage's ordered batch admission: an earlier
	// admitted, unpublished part can legitimately supply them.
	candidate, err := t.prepareCandidate(t.writes, false)
	if err != nil {
		return err
	}
	// Native executable envelopes have private authority invariants. Keep
	// these local capability checks separate from Storage's graph references;
	// forged nested results/journals must fail the callback, not the Session.
	for _, task := range candidate.Tasks {
		if task.Kind == nestedToolKind {
			if err := validateNestedReferences(candidate, task, t.limits); err != nil {
				return err
			}
		}
	}
	for _, doc := range candidate.Documents {
		if doc.Kind == codemodeRunKind || doc.Kind == codemodeJournalKind {
			if err := validateCodemodeDocuments(candidate, doc, t.limits); err != nil {
				return err
			}
		}
	}
	last := map[ID]int{}
	for i, w := range t.writes {
		if w.Op == "put-document" {
			last[w.Document.ID] = i
		}
	}
	selected := make([]Write, 0, len(t.writes))
	type selection struct {
		index int
		final Document
		plan  *transactionDocumentPlan
	}
	predicates := []selection{}
	for i, w := range t.writes {
		if w.Op != "put-document" {
			selected = append(selected, w)
			continue
		}
		id := w.Document.ID
		if last[id] != i {
			continue
		}
		plan := t.documentPlan(id)
		final := *w.Document
		final.Value = candidate.Documents[id].Value
		if _, exists := t.state.Documents[id]; !exists {
			final.CreatedAt = 0
		}
		final.Retired, final.RetiredAt = false, 0 // retirement remains a separate command
		if plan.requiredBase {
			final.DeltasSinceBase = 0
			selected = append(selected, Write{Op: "put-document", Document: &final})
			continue
		}
		if len(plan.ops) == 0 {
			continue
		}
		if !plan.explicit {
			before, e := ownJSONValue(t.state.Documents[id].Value, t.limits)
			if e != nil {
				return e
			}
			after, e := ownJSONValue(final.Value, t.limits)
			if e != nil {
				return e
			}
			if equalDeltaJSON(before, after) {
				continue
			}
		}
		ops, err := ownOperations(plan.ops, t.limits)
		if err != nil {
			return err
		}
		replayed, e := ApplyOperations(t.state.Documents[id].Value, ops, t.limits)
		if e != nil {
			return e
		}
		preparedValue, e := ownJSONValue(final.Value, t.limits)
		if e != nil {
			return e
		}
		if !equalDeltaJSON(replayed, preparedValue) {
			return reject("prepared operations disagree with final document")
		}
		selected = append(selected, Write{Op: "delta-document", Delta: &DocumentDelta{ID: id, Version: final.Version, Ops: ops}})
		if plan.definition != nil && plan.definition.options.CheckpointWhen != nil {
			predicates = append(predicates, selection{len(selected) - 1, final, plan})
		}
	}
	// Reference/value/operation validation is complete. Retained-tail budgets
	// apply to the actual predicate selection, so a checkpoint can reset a tail
	// that could not admit one further retained delta.
	for _, pending := range predicates {
		value, e := copyObject(pending.final.Value, t.limits)
		if e != nil {
			return e
		}
		callbackOps, e := ownOperations(selected[pending.index].Delta.Ops, t.limits)
		if e != nil {
			return e
		}
		checkpoint, e := pending.plan.definition.options.CheckpointWhen(value, callbackOps, CheckpointInfo{t.state.Documents[pending.final.ID].DeltasSinceBase})
		if e != nil {
			return e
		}
		if checkpoint {
			final := pending.final
			final.DeltasSinceBase = 0
			selected[pending.index] = Write{Op: "put-document", Document: &final}
		}
	}
	if len(selected) > 0 {
		for parent := range t.forkParents {
			if err = t.rejectCurrentForkWrites(selected, parent); err != nil {
				return err
			}
		}
		if _, err = encodeBounded(commitRecord{Seq: t.state.Seq + 1, Writes: selected}, t.limits, t.limits.MaxFramePayloadBytes); err != nil {
			return err
		}
		// When selection did not change the provisional writes, the candidate
		// above already validated them. Storage still validates the final batch.
		if len(predicates) > 0 {
			if _, err = t.prepareCandidate(selected, true); err != nil {
				return err
			}
		}
	}
	t.writes = selected
	return nil
}
