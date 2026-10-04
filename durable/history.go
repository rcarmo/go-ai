package durable

import (
	"context"
	"encoding/base64"
	"sort"
)

// Cursor is backend-owned continuation state. Round-trip it unchanged to the
// same scan/query; it is not an offset into a mutable slice.
type Cursor string

// Page contains detached records and an optional continuation (empty at end).
type Page[T any] struct {
	Items []T    `json:"items"`
	Next  Cursor `json:"next,omitempty"`
}

// EntryQuery selects inclusive ID bounds in fork-aware, newest-first history.
// Zero bounds are omitted. This differs intentionally from the legacy direct
// Entries API, whose EntryCursor remains commit/position ordered.
type EntryQuery struct {
	Conversation ID `json:"conversation"`
	MinEntryID   ID `json:"minEntryId,omitempty"`
	MaxEntryID   ID `json:"maxEntryId,omitempty"`
}

// HistoryStorage is the additive entry-history surface implemented by native
// stores. Storage's existing methods and legacy cursors remain compatible.
type HistoryStorage interface {
	ScanEntries(context.Context, EntryQuery, int, Cursor) (Page[Entry], error)
	VisibleEntry(context.Context, ID, ID) (Entry, bool, error)
	FindLatestHeadMarker(context.Context, ID, ID) (Entry, bool, error)
}

type historyCursor struct {
	Query EntryQuery `json:"query"`
	After ID         `json:"after"`
}

func validateAncestry(s Snapshot, final bool) error {
	for id, v := range s.Conversations {
		if uint64(v.Parent) > MaxID || uint64(v.ParentAt) > MaxID || (v.Parent == 0) != (v.ParentAt == 0) || v.Parent == id {
			return reject("invalid conversation ancestry")
		}
		seen := map[ID]bool{id: true}
		for next := v.Parent; next != 0; next = s.Conversations[next].Parent {
			if seen[next] {
				return reject("conversation ancestry cycle")
			}
			seen[next] = true
			if _, ok := s.Conversations[next]; !ok {
				if final {
					return reject("conversation parent missing")
				}
				break
			}
		}
		if final && v.Parent != 0 && !entryVisible(s, v.Parent, v.ParentAt) {
			return reject("fork cutoff is not visible from parent")
		}
	}
	return nil
}

func entryVisible(s Snapshot, conversation, id ID) bool {
	entry, ok := s.Entries[id]
	if !ok {
		return false
	}
	upper := ID(MaxID)
	seen := map[ID]bool{}
	for conversation != 0 && !seen[conversation] {
		seen[conversation] = true
		v, ok := s.Conversations[conversation]
		if !ok {
			return false
		}
		if entry.Conversation == conversation {
			return id <= upper
		}
		if v.ParentAt < upper {
			upper = v.ParentAt
		}
		conversation = v.Parent
	}
	return false
}

// visibleHistory reads only immutable owned state. The caller holds the store or
// Session line; returned entries remain private until their JSON is detached.
func visibleHistory(s Snapshot, q EntryQuery) ([]Entry, error) {
	if _, ok := s.Conversations[q.Conversation]; !ok {
		return nil, reject("unknown conversation")
	}
	if uint64(q.MinEntryID) > MaxID || uint64(q.MaxEntryID) > MaxID {
		return nil, reject("entry query outside ID bounds")
	}
	upper := q.MaxEntryID
	if upper == 0 {
		upper = ID(MaxID)
	}
	byConversation := map[ID][]Entry{}
	for _, e := range s.Entries {
		if e.ID >= q.MinEntryID && e.ID <= upper {
			byConversation[e.Conversation] = append(byConversation[e.Conversation], e)
		}
	}
	out := []Entry{}
	seen := map[ID]bool{}
	for id := q.Conversation; id != 0; {
		if seen[id] {
			return nil, reject("conversation ancestry cycle")
		}
		seen[id] = true
		v, ok := s.Conversations[id]
		if !ok {
			return nil, reject("conversation parent missing")
		}
		entries := byConversation[id]
		sort.Slice(entries, func(i, j int) bool { return entries[i].ID > entries[j].ID })
		for _, e := range entries {
			if e.ID <= upper {
				out = append(out, e)
			}
		}
		if v.ParentAt < upper {
			upper = v.ParentAt
		}
		if upper < q.MinEntryID {
			break
		}
		id = v.Parent
	}
	return out, nil
}

func (c *storeCore) ScanEntries(ctx context.Context, q EntryQuery, limit int, cursor Cursor) (Page[Entry], error) {
	if err := c.enter(ctx); err != nil {
		return Page[Entry]{}, err
	}
	defer c.leave()
	if err := checkPage(c.limits, limit); err != nil {
		return Page[Entry]{}, err
	}
	original := q
	if cursor != "" {
		if len(cursor) > 1024 {
			return Page[Entry]{}, reject("invalid entry cursor")
		}
		data, err := base64.RawURLEncoding.DecodeString(string(cursor))
		if err != nil {
			return Page[Entry]{}, reject("invalid entry cursor")
		}
		var continuation historyCursor
		if err = decodeStrict(data, c.limits, 1024, &continuation); err != nil {
			return Page[Entry]{}, err
		}
		if continuation.Query != q || continuation.After < 1 || uint64(continuation.After) > MaxID {
			return Page[Entry]{}, reject("entry cursor query mismatch")
		}
		// An exclusive ID ceiling is stable when newer entries are appended.
		if continuation.After == 1 {
			if _, ok := c.state.Conversations[q.Conversation]; !ok {
				return Page[Entry]{}, reject("unknown conversation")
			}
			return Page[Entry]{Items: []Entry{}}, nil
		}
		if q.MaxEntryID == 0 || q.MaxEntryID >= continuation.After {
			q.MaxEntryID = continuation.After - 1
		}
	}
	entries, err := visibleHistory(c.state, q)
	if err != nil {
		return Page[Entry]{}, err
	}
	page := Page[Entry]{Items: []Entry{}}
	if len(entries) > limit {
		data, err := encodeBounded(historyCursor{original, entries[limit-1].ID}, c.limits, 1024)
		if err != nil {
			return Page[Entry]{}, err
		}
		page.Next = Cursor(base64.RawURLEncoding.EncodeToString(data))
		entries = entries[:limit]
	}
	for _, entry := range entries {
		entry, err = copyEntry(entry, c.limits)
		if err != nil {
			return Page[Entry]{}, err
		}
		page.Items = append(page.Items, entry)
	}
	return page, nil
}

// VisibleEntry returns the entry's owning conversation and original commit Seq,
// not the descendant conversation from which it was looked up.
func (c *storeCore) VisibleEntry(ctx context.Context, conversation, id ID) (Entry, bool, error) {
	if err := c.enter(ctx); err != nil {
		return Entry{}, false, err
	}
	defer c.leave()
	if _, ok := c.state.Conversations[conversation]; !ok {
		return Entry{}, false, reject("unknown conversation")
	}
	if id == 0 || uint64(id) > MaxID {
		return Entry{}, false, reject("invalid entry ID")
	}
	if !entryVisible(c.state, conversation, id) {
		return Entry{}, false, nil
	}
	entry := c.state.Entries[id]
	entry, err := copyEntry(entry, c.limits)
	return entry, err == nil, err
}

// FindLatestHeadMarker applies the same ancestor cutoffs and optional inclusive
// entry-ID ceiling as ScanEntries. Zero ceiling means current visible history.
func (c *storeCore) FindLatestHeadMarker(ctx context.Context, conversation, at ID) (Entry, bool, error) {
	if err := c.enter(ctx); err != nil {
		return Entry{}, false, err
	}
	defer c.leave()
	entries, err := visibleHistory(c.state, EntryQuery{Conversation: conversation, MaxEntryID: at})
	if err != nil {
		return Entry{}, false, err
	}
	for _, entry := range entries {
		if entry.Head != 0 {
			entry, err = copyEntry(entry, c.limits)
			return entry, err == nil, err
		}
	}
	return Entry{}, false, nil
}

var _ HistoryStorage = (*MemoryStorage)(nil)
var _ HistoryStorage = (*JournalStorage)(nil)
