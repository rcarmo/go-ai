package durable

import (
	"context"
	"encoding/base64"
	"reflect"
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
type scanCursor[Q any] struct {
	Query Q         `json:"query"`
	After ID        `json:"after"`
	Order ScanOrder `json:"order,omitempty"`
}

func validScanOrder(order ScanOrder) bool { return order == Ascending || order == Descending }

// The query in a continuation omits order: callers can omit or repeat the stored
// order, but cannot alter filters or switch direction. Pre-1.1 entry cursors have
// no order and therefore retain their original descending default.
func resolveScan[Q any](query Q, requested, fallback ScanOrder, cursor Cursor, l Limits) (ScanOrder, ID, error) {
	if requested != "" && !validScanOrder(requested) {
		return "", 0, reject("invalid scan order")
	}
	if cursor == "" {
		if requested != "" {
			fallback = requested
		}
		return fallback, 0, nil
	}
	if len(cursor) > 1024 {
		return "", 0, reject("invalid storage cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(string(cursor))
	if err != nil {
		return "", 0, reject("invalid storage cursor")
	}
	var continuation scanCursor[Q]
	if err = decodeStrict(data, l, 1024, &continuation); err != nil {
		return "", 0, err
	}
	if continuation.After < 1 || uint64(continuation.After) > MaxID || !reflect.DeepEqual(continuation.Query, query) {
		return "", 0, reject("storage cursor query mismatch")
	}
	order := continuation.Order
	if order == "" {
		order = fallback
	}
	if !validScanOrder(order) {
		return "", 0, reject("invalid storage cursor order")
	}
	if requested != "" && requested != order {
		return "", 0, reject("storage cursor order mismatch")
	}
	return order, continuation.After, nil
}
func scanPage[V any, Q any](values map[ID]V, query Q, order ScanOrder, after ID, limit int, l Limits, match func(V) bool, detach func(V) (V, error)) (Page[V], error) {
	keys := ids(values)
	if order == Descending {
		for i, j := 0, len(keys)-1; i < j; i, j = i+1, j-1 {
			keys[i], keys[j] = keys[j], keys[i]
		}
	}
	page := Page[V]{Items: []V{}}
	var last ID
	for _, id := range keys {
		if after != 0 && ((order == Ascending && id <= after) || (order == Descending && id >= after)) {
			continue
		}
		value := values[id]
		if !match(value) {
			continue
		}
		if len(page.Items) == limit {
			data, err := encodeBounded(scanCursor[Q]{Query: query, After: last, Order: order}, l, 1024)
			if err != nil {
				return Page[V]{}, err
			}
			page.Next = Cursor(base64.RawURLEncoding.EncodeToString(data))
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
	requested := q.Order
	q.Order = ""
	order, after, err := resolveScan(q, requested, Ascending, cursor, l)
	if err != nil {
		return Page[Conversation]{}, err
	}
	return scanPage(state.Conversations, q, order, after, limit, l, func(v Conversation) bool {
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
	requested := q.Order
	q.Order = ""
	order, after, err := resolveScan(q, requested, Ascending, cursor, l)
	if err != nil {
		return Page[Task]{}, err
	}
	return scanPage(state.Tasks, q, order, after, limit, l, func(v Task) bool {
		return (q.Conversation == 0 || v.Conversation == q.Conversation) && (q.Kind == "" || v.Kind == q.Kind) && (q.Status == "" || v.Status == q.Status) && (q.AbortRequested == nil || taskAborted(v) == *q.AbortRequested) && (q.Background == nil || taskBackground(v) == *q.Background)
	}, func(v Task) (Task, error) { return copyTask(v, l) })
}
func scanSubmissions(state Snapshot, q SubmissionQuery, limit int, cursor Cursor, l Limits) (Page[Submission], error) {
	requested := q.Order
	q.Order = ""
	order, after, err := resolveScan(q, requested, Ascending, cursor, l)
	if err != nil {
		return Page[Submission]{}, err
	}
	return scanPage(state.Submissions, q, order, after, limit, l, func(v Submission) bool {
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
	return s.store.ScanConversations(ctx, q, limit, cursor)
}
func (s *Session) ScanEntries(ctx context.Context, q EntryQuery, limit int, cursor Cursor) (Page[Entry], error) {
	if e := s.enter(ctx); e != nil {
		return Page[Entry]{}, e
	}
	defer s.leave()
	return s.store.ScanEntries(ctx, q, limit, cursor)
}
func (s *Session) ScanTasks(ctx context.Context, q TaskQuery, limit int, cursor Cursor) (Page[Task], error) {
	if e := s.enter(ctx); e != nil {
		return Page[Task]{}, e
	}
	defer s.leave()
	return s.store.ScanTasks(ctx, q, limit, cursor)
}
func (s *Session) ScanSubmissions(ctx context.Context, q SubmissionQuery, limit int, cursor Cursor) (Page[Submission], error) {
	if e := s.enter(ctx); e != nil {
		return Page[Submission]{}, e
	}
	defer s.leave()
	return s.store.ScanSubmissions(ctx, q, limit, cursor)
}

var _ OrderedStorage = (*MemoryStorage)(nil)
var _ OrderedStorage = (*JournalStorage)(nil)
