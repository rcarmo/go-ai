package durable

import (
	"errors"
	"testing"
)

func TestStorage120InvalidCursorTypedRecoverableRead(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		for _, cursor := range []Cursor{"bad", "eyJhZnRlciI6MH0"} {
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
		if _, e := b.store.ScanTasks(bg, TaskQuery{Order: "bad"}, 1, ""); e == nil {
			t.Fatal("invalid order accepted")
		}
	})
}
