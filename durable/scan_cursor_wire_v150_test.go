package durable

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

// Original pi-durable 1.5.0 cursors are plain JSON objects {after, order}.
// Pages and continuations are captured from the original MemoryStorage by
// scripts/capture-durable-cursors.mjs; assertions are applied unchanged.
type cursorWirePage struct {
	Name    string          `json:"name"`
	Method  string          `json:"method"`
	Query   map[string]any  `json:"query"`
	Limit   int             `json:"limit"`
	Cursor  map[string]any  `json:"cursor"`
	Items   []ID            `json:"items"`
	Next    json.RawMessage `json:"next"`
	Error   string          `json:"error"`
	Message string          `json:"message"`
}

func TestPiDurableV150ScanCursorWire(t *testing.T) {
	raw, err := os.ReadFile("testdata/pi-durable-v150/scan-cursors.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		IDs   map[string]ID    `json:"ids"`
		Pages []cursorWirePage `json:"pages"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		got := map[string]ID{}
		for _, name := range []string{"c2", "c3", "e1", "e2", "e3", "t1", "t2", "s1", "s2"} {
			got[name] = mint(t, b.store)
		}
		if !reflect.DeepEqual(got, fixture.IDs) {
			t.Fatalf("minted IDs %v, original %v", got, fixture.IDs)
		}
		apply(t, b.store, Write{Op: "create-conversation", Conversation: &Conversation{ID: got["c2"]}}, Write{Op: "create-conversation", Conversation: &Conversation{ID: got["c3"]}})
		apply(t, b.store, scanEntry(got["e1"], 1), scanEntry(got["e2"], 1), scanEntry(got["e3"], 1))
		task := func(id ID) Write {
			record := foundationTask(id, nil)
			record.Kind = "test.task"
			record.Execution.Native.State.Checkpoint = JSON{}
			return Write{Op: "put-task", Task: &record}
		}
		apply(t, b.store, task(got["t1"]), task(got["t2"]))
		sub := func(id ID) Write {
			return Write{Op: "put-submission", Submission: &Submission{ID: id, Conversation: 1, Type: "write", Status: "pending", Value: JSON{}}}
		}
		apply(t, b.store, sub(got["s1"]), sub(got["s2"]))
		ordered := b.store.(OrderedStorage)
		h := history(t, b.store)
		for _, p := range fixture.Pages {
			t.Run(p.Name, func(t *testing.T) {
				order, _ := p.Query["order"].(string)
				cursor := Cursor(p.Cursor)
				var ids []ID
				var next Cursor
				var err error
				switch p.Method {
				case "scanConversations":
					var page Page[Conversation]
					page, err = ordered.ScanConversations(bg, ConversationQuery{Order: ScanOrder(order)}, p.Limit, cursor)
					for _, v := range page.Items {
						ids = append(ids, v.ID)
					}
					next = page.Next
				case "scanEntries":
					var page Page[Entry]
					page, err = h.ScanEntries(bg, EntryQuery{Conversation: 1, Order: ScanOrder(order)}, p.Limit, cursor)
					for _, v := range page.Items {
						ids = append(ids, v.ID)
					}
					next = page.Next
				case "scanTasks":
					kind, _ := p.Query["kind"].(string)
					var page Page[TaskRecord]
					page, err = ordered.ScanTasks(bg, TaskQuery{Kind: kind, Order: ScanOrder(order)}, p.Limit, cursor)
					for _, v := range page.Items {
						ids = append(ids, v.ID)
					}
					next = page.Next
				case "scanSubmissions":
					var page Page[Submission]
					page, err = ordered.ScanSubmissions(bg, SubmissionQuery{Order: ScanOrder(order)}, p.Limit, cursor)
					for _, v := range page.Items {
						ids = append(ids, v.ID)
					}
					next = page.Next
				}
				if p.Error != "" {
					var request *StorageRequestError
					if !errors.As(err, &request) {
						t.Fatalf("want StorageRequestError (%s), got %v", p.Message, err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(ids, p.Items) {
					t.Fatalf("items %v, original %v", ids, p.Items)
				}
				encoded, err := json.Marshal(next)
				if err != nil {
					t.Fatal(err)
				}
				var want bytes.Buffer
				if err := json.Compact(&want, p.Next); err != nil {
					t.Fatal(err)
				}
				if string(encoded) != want.String() {
					t.Fatalf("next %s, original %s", encoded, p.Next)
				}
			})
		}
	})
}
