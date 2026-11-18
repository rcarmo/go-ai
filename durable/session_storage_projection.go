package durable

import (
	"context"
	"reflect"
)

// storageProjection is an optional native read shortcut, not a condition of
// the upstream Storage contract. A public decorator can supply only scans.
type storageProjection interface {
	Snapshot(context.Context) (Snapshot, error)
}

// readStorageProjection reconstructs the current validation tables. Storage
// exposes exact entry sequences but no global head or document revision dump.
// The greatest visible sequence is a lower bound until the first receipt;
// never fabricate history. Point reads can still use the public Storage API.
func readStorageProjection(ctx context.Context, store Storage, limits Limits) (Snapshot, error) {
	if storage, ok := store.(storageProjection); ok {
		return storage.Snapshot(ctx)
	}
	image := initialState()
	limit := min(128, limits.MaxPage)
	used := map[ID]bool{}
	track := func(id ID, seq uint64) error {
		if err := checkReadIDs(id); err != nil {
			return err
		}
		if used[id] || len(used) >= limits.MaxRecords || seq > MaxID {
			return reject("invalid Storage projection record")
		}
		used[id] = true
		image.HighWater = max(image.HighWater, uint64(id))
		image.Seq = max(image.Seq, seq)
		return nil
	}
	if err := walkStoragePages(ctx, ConversationQuery{}, limit, store.ScanConversations, func(c Conversation) error {
		if err := track(c.ID, 0); err != nil {
			return err
		}
		image.Conversations[c.ID] = c
		return nil
	}); err != nil {
		return Snapshot{}, err
	}
	for _, conversation := range ids(image.Conversations) {
		within := make(map[ID]bool)
		if err := walkStoragePages(ctx, EntryQuery{Conversation: conversation}, limit, store.ScanEntries, func(e Entry) error {
			if within[e.ID] {
				return reject("repeated Storage entry in one scan")
			}
			within[e.ID] = true
			if _, found := image.Entries[e.ID]; found {
				return nil
			} // visible through an ancestor already read
			r, found, err := store.Entry(ctx, e.ID)
			if err != nil {
				return err
			}
			if !found || r.Entry.ID != e.ID || r.CommitSeq == 0 {
				return reject("Storage entry disappeared during projection")
			}
			if err := track(r.Entry.ID, r.CommitSeq); err != nil {
				return err
			}
			r.Entry.Seq = r.CommitSeq
			image.Entries[e.ID] = nativeEntryDiagnosticData(r.Entry)
			return nil
		}); err != nil {
			return Snapshot{}, err
		}
	}
	if err := walkStoragePages(ctx, TaskQuery{}, limit, store.ScanTasks, func(r TaskRecord) error {
		if err := track(r.ID, 0); err != nil {
			return err
		}
		record, err := nativeTask(r, limits)
		if err == nil {
			image.Tasks[r.ID] = record
		}
		return err
	}); err != nil {
		return Snapshot{}, err
	}
	if err := walkStoragePages(ctx, SubmissionQuery{}, limit, store.ScanSubmissions, func(r SubmissionRecord) error {
		if err := track(r.ID, 0); err != nil {
			return err
		}
		record, err := nativeSubmission(r, limits)
		if err == nil {
			image.Submissions[r.ID] = record
		}
		return err
	}); err != nil {
		return Snapshot{}, err
	}
	scopes := []DocumentScope{{Scope: "session"}}
	for _, id := range ids(image.Conversations) {
		scopes = append(scopes, DocumentScope{Scope: "conversation", Owner: id})
	}
	for _, id := range ids(image.Tasks) {
		scopes = append(scopes, DocumentScope{Scope: "task", Owner: id})
	}
	for _, scope := range scopes {
		point := CurrentDocumentPoint()
		if err := walkStoragePages(ctx, DocumentQuery{Scope: scope.Scope, Owner: scope.Owner, At: point}, limit, store.ScanDocuments, func(d Document) error {
			v, found, err := store.DocumentAt(ctx, d.ID, point)
			if err != nil {
				return err
			}
			if !found || v.ID != d.ID {
				return reject("Storage document disappeared during projection")
			}
			if err := track(v.ID, v.CreatedAt); err != nil {
				return err
			}
			image.Documents[v.ID] = v
			return nil
		}); err != nil {
			return Snapshot{}, err
		}
	}
	deriveConversationOwners(&image, nil)
	return image, nil
}

func walkStoragePages[Q, T any](ctx context.Context, q Q, limit int, scan func(context.Context, Q, int, Cursor) (Page[T], error), visit func(T) error) error {
	var cursor Cursor
	for {
		page, err := scan(ctx, q, limit, cursor)
		if err != nil {
			return err
		}
		if len(page.Items) > limit || ((len(page.Items) == 0 || reflect.DeepEqual(cursor, page.Next)) && page.Next != nil) {
			return reject("non-progressing Storage page")
		}
		for _, item := range page.Items {
			if err := visit(item); err != nil {
				return err
			}
		}
		if page.Next == nil {
			return nil
		}
		cursor = page.Next
	}
}
