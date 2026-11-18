package durable

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

// CachedStorage forwards history reads rather than copying its publication
// image. Record the actual public overloads invoked behind the Session line.
type sourceHistoryStorage struct {
	*SQLiteStorage
	calls []string
}

func (s *sourceHistoryStorage) Entry(ctx context.Context, ids ...ID) (StoredEntry, bool, error) {
	s.calls = append(s.calls, fmt.Sprint("entry ", ids))
	return s.SQLiteStorage.Entry(ctx, ids...)
}
func (s *sourceHistoryStorage) ScanEntries(ctx context.Context, q EntryQuery, limit int, cursor Cursor) (Page[Entry], error) {
	s.calls = append(s.calls, fmt.Sprintf("scan %d %d", q.Conversation, limit))
	return s.SQLiteStorage.ScanEntries(ctx, q, limit, cursor)
}
func (s *sourceHistoryStorage) FindLatestHeadMarker(ctx context.Context, conversation, at ID) (Entry, bool, error) {
	s.calls = append(s.calls, fmt.Sprintf("head %d %d", conversation, at))
	return s.SQLiteStorage.FindLatestHeadMarker(ctx, conversation, at)
}

func TestSourcePublicStorageForwardsHistoryReads(t *testing.T) {
	bg := context.Background()
	sqliteStore(t, func(t *testing.T, f sqliteFixture) {
		store := &sourceHistoryStorage{SQLiteStorage: f.store.(*SQLiteStorage)}
		session, err := OpenSession(store)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = session.Close(bg) }()
		value, err := session.Commit(bg, func(tx *Tx) (any, error) {
			return tx.AppendEntry(1, EntryDraft{"kind": "summary", "head": "self"})
		})
		if err != nil {
			t.Fatal(err)
		}
		made := value.(Entry)
		stored, found, err := store.SQLiteStorage.Entry(bg, made.ID)
		if err != nil || !found || !reflect.DeepEqual(made, stored.Entry) {
			t.Fatalf("stored summary differs from created record: %+v, %+v, %v, %v", made, stored, found, err)
		}
		query := EntryQuery{Conversation: 1}
		page, err := store.SQLiteStorage.ScanEntries(bg, query, 10, nil)
		if err != nil {
			t.Fatal(err)
		}
		marker, found, err := store.SQLiteStorage.FindLatestHeadMarker(bg, 1, made.ID)
		if err != nil || !found || marker.Head != made.ID {
			t.Fatalf("missing self marker: %+v, %v, %v", marker, found, err)
		}
		for _, ids := range [][]ID{{made.ID}, {1, made.ID}} {
			if got, yes, err := session.Entry(bg, ids...); err != nil || !yes || !reflect.DeepEqual(got, stored) {
				t.Fatalf("Session.entry %v: %+v, %v, %v", ids, got, yes, err)
			}
		}
		if got, err := session.ScanEntries(bg, query, 10, nil); err != nil || !reflect.DeepEqual(got, page) {
			t.Fatalf("Session.scanEntries: %+v, %v", got, err)
		}
		_, err = session.Commit(bg, func(tx *Tx) (any, error) {
			if got, yes, err := tx.Entry(made.ID); err != nil || !yes || !reflect.DeepEqual(got, stored.Entry) {
				return nil, fmt.Errorf("Tx.entry: %+v, %v, %v", got, yes, err)
			}
			if got, err := tx.ScanEntries(query, 10, nil); err != nil || !reflect.DeepEqual(got, page) {
				return nil, fmt.Errorf("Tx.scanEntries: %+v, %v", got, err)
			}
			if got, yes, err := tx.LatestEntryID(1); err != nil || !yes || got != made.ID {
				return nil, fmt.Errorf("Tx.latestEntryId: %d, %v, %v", got, yes, err)
			}
			if got, yes, err := tx.LatestHeadMarker(1); err != nil || !yes || !reflect.DeepEqual(got, marker) {
				return nil, fmt.Errorf("Tx.latestHeadMarker: %+v, %v, %v", got, yes, err)
			}
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		err = session.ReadOnLine(bg, func(line *ReadLine) error {
			if got, err := line.ScanEntries(query, 10, nil); err != nil || !reflect.DeepEqual(got, page) {
				return fmt.Errorf("line.scanEntries: %+v, %v", got, err)
			}
			if got, yes, err := line.FindLatestHeadMarker(1, made.ID); err != nil || !yes || !reflect.DeepEqual(got, marker) {
				return fmt.Errorf("line.findLatestHeadMarker: %+v, %v, %v", got, yes, err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		entryCall := fmt.Sprintf("entry [%d]", made.ID)
		want := []string{entryCall, fmt.Sprintf("entry [1 %d]", made.ID), "scan 1 10", entryCall, "scan 1 10", "scan 1 10", fmt.Sprintf("head 1 %d", made.ID)}
		if !reflect.DeepEqual(store.calls, want) {
			t.Fatalf("public Storage history calls = %q, want %q", store.calls, want)
		}
	})
}
