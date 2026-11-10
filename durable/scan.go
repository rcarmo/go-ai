package durable

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
)

// ScanOrder controls ID traversal. An omitted order uses the scan's default;
// with a cursor, it uses the cursor's order instead.
type ScanOrder string

const (
	Ascending  ScanOrder = "ascending"
	Descending ScanOrder = "descending"
)

type ConversationQuery struct {
	OwnerConversation ID        `json:"ownerConversation,omitempty"`
	OwnerTask         ID        `json:"ownerTask,omitempty"`
	Order             ScanOrder `json:"order,omitempty"`
}
type TaskQuery struct {
	Conversation   ID        `json:"conversation,omitempty"`
	Kind           string    `json:"kind,omitempty"`
	Status         string    `json:"status,omitempty"`
	AbortRequested *bool     `json:"abortRequested,omitempty"`
	Background     *bool     `json:"background,omitempty"`
	Order          ScanOrder `json:"order,omitempty"`
}
type SubmissionQuery struct {
	Conversation ID        `json:"conversation,omitempty"`
	Status       string    `json:"status,omitempty"`
	Order        ScanOrder `json:"order,omitempty"`
}

// OrderedStorage is a narrow view of the required 1.1.0 storage scan contract.
type OrderedStorage interface {
	ScanConversations(context.Context, ConversationQuery, int, Cursor) (Page[Conversation], error)
	ScanTasks(context.Context, TaskQuery, int, Cursor) (Page[Task], error)
	ScanSubmissions(context.Context, SubmissionQuery, int, Cursor) (Page[Submission], error)
}

// Cursor is the original pi-durable continuation: a plain JSON object whose
// "after" member is the last returned ID and whose optional "order" member is
// the scan direction. Filters are not bound to it. A nil Cursor starts a scan.
type Cursor map[string]any

// nextCursor is the continuation after a page whose last item has id.
func nextCursor(id ID, order ScanOrder) Cursor {
	return Cursor{"after": float64(id), "order": string(order)}
}

// cursorAfter reads the cursor's "after" member, which must be a safe integer.
func cursorAfter(cursor Cursor) (int64, bool, error) {
	value, ok := cursor["after"]
	if cursor == nil || !ok {
		return 0, false, nil
	}
	const safe = 1<<53 - 1
	var after int64
	switch v := value.(type) {
	case float64:
		if v != math.Trunc(v) || v > safe || v < -safe {
			return 0, false, storageRequest("invalid storage cursor")
		}
		after = int64(v)
	case json.Number:
		n, err := strconv.ParseInt(string(v), 10, 64)
		if err != nil {
			f, ferr := v.Float64()
			if ferr != nil || f != math.Trunc(f) || f > safe || f < -safe {
				return 0, false, storageRequest("invalid storage cursor")
			}
			n = int64(f)
		}
		after = n
	case int:
		after = int64(v)
	case int64:
		after = v
	case uint64:
		if v > safe {
			return 0, false, storageRequest("invalid storage cursor")
		}
		after = int64(v)
	case ID:
		if uint64(v) > safe {
			return 0, false, storageRequest("invalid storage cursor")
		}
		after = int64(v)
	default:
		return 0, false, storageRequest("invalid storage cursor")
	}
	if after > safe || after < -safe {
		return 0, false, storageRequest("invalid storage cursor")
	}
	return after, true, nil
}

func validScanOrder(order ScanOrder) bool { return order == Ascending || order == Descending }

// resolveScan follows the original scanStart: a cursor continues in the order
// it was created with whether the query repeats or omits it; a different order
// is rejected. A cursor without an order continues in the scan's default.
func resolveScan(requested, fallback ScanOrder, cursor Cursor) (ScanOrder, int64, bool, error) {
	if requested != "" && !validScanOrder(requested) {
		return "", 0, false, storageRequest("invalid scan order: " + string(requested))
	}
	if cursor == nil {
		if requested != "" {
			fallback = requested
		}
		return fallback, 0, false, nil
	}
	after, ok, err := cursorAfter(cursor)
	if err != nil || !ok {
		return "", 0, false, storageRequest("invalid storage cursor")
	}
	order := fallback
	if stored, present := cursor["order"]; present {
		text, isString := stored.(string)
		if !isString || !validScanOrder(ScanOrder(text)) {
			return "", 0, false, storageRequest("invalid storage cursor")
		}
		order = ScanOrder(text)
	}
	if requested != "" && requested != order {
		return "", 0, false, storageRequest("the cursor continues a " + string(order) + " scan; the query asks for " + string(requested))
	}
	return order, after, true, nil
}
func scanPage[V any](values map[ID]V, order ScanOrder, after int64, hasAfter bool, limit int, match func(V) bool, detach func(V) (V, error)) (Page[V], error) {
	keys := ids(values)
	if order == Descending {
		for i, j := 0, len(keys)-1; i < j; i, j = i+1, j-1 {
			keys[i], keys[j] = keys[j], keys[i]
		}
	}
	page := Page[V]{Items: []V{}}
	var last ID
	for _, id := range keys {
		if hasAfter && ((order == Ascending && int64(id) <= after) || (order == Descending && int64(id) >= after)) {
			continue
		}
		value := values[id]
		if !match(value) {
			continue
		}
		if len(page.Items) == limit {
			page.Next = nextCursor(last, order)
			break
		}
		owned, err := detach(value)
		if err != nil {
			return Page[V]{}, err
		}
		page.Items = append(page.Items, owned)
		last = id
	}
	return page, nil
}
func scanConversations(state Snapshot, q ConversationQuery, limit int, cursor Cursor, l Limits) (Page[Conversation], error) {
	if err := checkReadIDs(q.OwnerTask, q.OwnerConversation); err != nil {
		return Page[Conversation]{}, err
	}
	order, after, hasAfter, err := resolveScan(q.Order, Ascending, cursor)
	if err != nil {
		return Page[Conversation]{}, err
	}
	return scanPage(state.Conversations, order, after, hasAfter, limit, func(v Conversation) bool {
		if q.OwnerTask != 0 && v.Owner != q.OwnerTask {
			return false
		}
		if q.OwnerConversation != 0 {
			owner, ok := state.Tasks[v.Owner]
			if !ok || owner.Conversation != q.OwnerConversation {
				return false
			}
		}
		return true
	}, func(v Conversation) (Conversation, error) { return v, nil })
}
func scanTasks(state Snapshot, q TaskQuery, limit int, cursor Cursor, l Limits) (Page[Task], error) {
	if err := checkReadIDs(q.Conversation); err != nil {
		return Page[Task]{}, err
	}
	order, after, hasAfter, err := resolveScan(q.Order, Ascending, cursor)
	if err != nil {
		return Page[Task]{}, err
	}
	return scanPage(state.Tasks, order, after, hasAfter, limit, func(v Task) bool {
		return (q.Conversation == 0 || v.Conversation == q.Conversation) && (q.Kind == "" || v.Kind == q.Kind) && (q.Status == "" || v.Status == q.Status) && (q.AbortRequested == nil || taskAborted(v) == *q.AbortRequested) && (q.Background == nil || taskBackground(v) == *q.Background)
	}, func(v Task) (Task, error) { return copyTask(v, l) })
}
func scanSubmissions(state Snapshot, q SubmissionQuery, limit int, cursor Cursor, l Limits) (Page[Submission], error) {
	if err := checkReadIDs(q.Conversation); err != nil {
		return Page[Submission]{}, err
	}
	order, after, hasAfter, err := resolveScan(q.Order, Ascending, cursor)
	if err != nil {
		return Page[Submission]{}, err
	}
	return scanPage(state.Submissions, order, after, hasAfter, limit, func(v Submission) bool {
		return (q.Conversation == 0 || v.Conversation == q.Conversation) && (q.Status == "" || v.Status == q.Status)
	}, func(v Submission) (Submission, error) {
		value, e := copyObject(v.Value, l)
		v.Value = value
		return v, e
	})
}

func (c *storeCore) ScanConversations(ctx context.Context, q ConversationQuery, limit int, cursor Cursor) (Page[Conversation], error) {
	if e := c.enter(ctx); e != nil {
		return Page[Conversation]{}, e
	}
	defer c.leave()
	if e := checkPage(c.limits, limit); e != nil {
		return Page[Conversation]{}, e
	}
	return scanConversations(c.state, q, limit, cursor, c.limits)
}
func (c *storeCore) ScanTasks(ctx context.Context, q TaskQuery, limit int, cursor Cursor) (Page[Task], error) {
	if e := c.enter(ctx); e != nil {
		return Page[Task]{}, e
	}
	defer c.leave()
	if e := checkPage(c.limits, limit); e != nil {
		return Page[Task]{}, e
	}
	return scanTasks(c.state, q, limit, cursor, c.limits)
}
func (c *storeCore) ScanSubmissions(ctx context.Context, q SubmissionQuery, limit int, cursor Cursor) (Page[Submission], error) {
	if e := c.enter(ctx); e != nil {
		return Page[Submission]{}, e
	}
	defer c.leave()
	if e := checkPage(c.limits, limit); e != nil {
		return Page[Submission]{}, e
	}
	return scanSubmissions(c.state, q, limit, cursor, c.limits)
}

// Transaction scans include staged writes and reject escaped/concurrent handles.
func (t *Tx) ScanConversations(q ConversationQuery, limit int, cursor Cursor) (Page[Conversation], error) {
	if e := t.enter(); e != nil {
		return Page[Conversation]{}, e
	}
	defer t.leave()
	if e := checkPage(t.limits, limit); e != nil {
		return Page[Conversation]{}, e
	}
	state, e := t.current()
	if e != nil {
		return Page[Conversation]{}, e
	}
	return scanConversations(state, q, limit, cursor, t.limits)
}
func (t *Tx) ScanEntries(q EntryQuery, limit int, cursor Cursor) (Page[Entry], error) {
	if e := t.enter(); e != nil {
		return Page[Entry]{}, e
	}
	defer t.leave()
	if e := checkPage(t.limits, limit); e != nil {
		return Page[Entry]{}, e
	}
	state, e := t.current()
	if e != nil {
		return Page[Entry]{}, e
	}
	return scanEntries(state, q, limit, cursor, t.limits)
}
func (t *Tx) ScanTasks(q TaskQuery, limit int, cursor Cursor) (Page[Task], error) {
	if e := t.enter(); e != nil {
		return Page[Task]{}, e
	}
	defer t.leave()
	if e := checkPage(t.limits, limit); e != nil {
		return Page[Task]{}, e
	}
	state, e := t.current()
	if e != nil {
		return Page[Task]{}, e
	}
	return scanTasks(state, q, limit, cursor, t.limits)
}
func (t *Tx) ScanSubmissions(q SubmissionQuery, limit int, cursor Cursor) (Page[Submission], error) {
	if e := t.enter(); e != nil {
		return Page[Submission]{}, e
	}
	defer t.leave()
	if e := checkPage(t.limits, limit); e != nil {
		return Page[Submission]{}, e
	}
	state, e := t.current()
	if e != nil {
		return Page[Submission]{}, e
	}
	return scanSubmissions(state, q, limit, cursor, t.limits)
}

// Session scans share transaction admission/closure; no old-backend fallback.
func (s *Session) ScanConversations(ctx context.Context, q ConversationQuery, limit int, cursor Cursor) (Page[Conversation], error) {
	if e := s.enter(ctx); e != nil {
		return Page[Conversation]{}, e
	}
	defer s.leave()
	// Upstream gates every read: a job must not observe state an admitted
	// write has not published.
	if e := s.touchAll(ctx); e != nil {
		return Page[Conversation]{}, e
	}
	if !s.hasNativeReadImage() {
		return s.guardedStorage().ScanConversations(ctx, q, limit, cursor)
	}
	image, err := s.taskSnapshot(ctx)
	if err != nil {
		return Page[Conversation]{}, err
	}
	return scanConversations(image, q, limit, cursor, s.core.limits)
}
func (s *Session) ScanEntries(ctx context.Context, q EntryQuery, limit int, cursor Cursor) (Page[Entry], error) {
	if e := s.enter(ctx); e != nil {
		return Page[Entry]{}, e
	}
	defer s.leave()
	// Upstream gates every read: a job must not observe state an admitted
	// write has not published.
	if e := s.touchAll(ctx); e != nil {
		return Page[Entry]{}, e
	}
	if !s.hasNativeReadImage() {
		return s.guardedStorage().ScanEntries(ctx, q, limit, cursor)
	}
	image, err := s.taskSnapshot(ctx)
	if err != nil {
		return Page[Entry]{}, err
	}
	return scanEntries(image, q, limit, cursor, s.core.limits)
}
func (s *Session) ScanTasks(ctx context.Context, q TaskQuery, limit int, cursor Cursor) (Page[Task], error) {
	if e := s.enter(ctx); e != nil {
		return Page[Task]{}, e
	}
	defer s.leave()
	// Upstream gates every read: a job must not observe state an admitted
	// write has not published.
	if e := s.touchAll(ctx); e != nil {
		return Page[Task]{}, e
	}
	if !s.hasNativeReadImage() {
		return s.guardedStorage().ScanTasks(ctx, q, limit, cursor)
	}
	image, err := s.taskSnapshot(ctx)
	if err != nil {
		return Page[Task]{}, err
	}
	return scanTasks(image, q, limit, cursor, s.core.limits)
}
func (s *Session) ScanSubmissions(ctx context.Context, q SubmissionQuery, limit int, cursor Cursor) (Page[Submission], error) {
	if e := s.enter(ctx); e != nil {
		return Page[Submission]{}, e
	}
	defer s.leave()
	// Upstream gates every read: a job must not observe state an admitted
	// write has not published.
	if e := s.touchAll(ctx); e != nil {
		return Page[Submission]{}, e
	}
	if !s.hasNativeReadImage() {
		return s.guardedStorage().ScanSubmissions(ctx, q, limit, cursor)
	}
	image, err := s.taskSnapshot(ctx)
	if err != nil {
		return Page[Submission]{}, err
	}
	return scanSubmissions(image, q, limit, cursor, s.core.limits)
}
