package durable

import (
	"errors"
	"testing"
)

func TestStorage120InvalidCursorTypedRecoverableRead(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		for _, cursor := range []Cursor{{"after": "bad"}, {"after": 1.5}, {"after": float64(1 << 54)}, {"after": 1.0, "order": "up"}} {
			_, e := b.store.ScanTasks(bg, TaskQuery{}, 1, cursor)
			var request *StorageRequestError
			var rejected *StorageRejected
			if !errors.As(e, &request) || !errors.As(e, &rejected) {
				t.Fatal("request type missing", e)
			}
			if _, e = b.store.Snapshot(bg); e != nil {
				t.Fatal("invalid read poisoned storage", e)
			}
		}
		if _, e := b.store.ScanTasks(bg, TaskQuery{Order: "bad"}, 1, nil); e == nil {
			t.Fatal("invalid order accepted")
		}
	})
}
