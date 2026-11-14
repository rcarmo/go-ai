package durable

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func scanEntry(id, conversation ID) Write {
	return Write{Op: "append-entry", Entry: &Entry{ID: id, Conversation: conversation, Kind: "event", Value: JSON{}}}
}

func TestScan110OrderForksCursorsAndLegacy(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		root := mint(t, b.store)
		one, two := mint(t, b.store), mint(t, b.store)
		apply(t, b.store, Write{Op: "create-conversation", Conversation: &Conversation{ID: root}}, scanEntry(one, root), scanEntry(two, root))
		child, excluded := mint(t, b.store), mint(t, b.store)
		apply(t, b.store, Write{Op: "create-conversation", Conversation: &Conversation{ID: child, Parent: root, ParentAt: two}}, scanEntry(excluded, root))
		three, four := mint(t, b.store), mint(t, b.store)
		apply(t, b.store, scanEntry(three, child), scanEntry(four, child))
		h := history(t, b.store)
		for _, order := range []ScanOrder{Ascending, Descending} {
			q := EntryQuery{Conversation: child, Order: order}
			first, err := h.ScanEntries(bg, q, 2, nil)
			if err != nil {
				t.Fatal(err)
			}
			want := []ID{one, two}
			lastWant := []ID{three, four}
			if order == Descending {
				want = []ID{four, three}
				lastWant = []ID{two, one}
			}
			if got := historyIDs(first); !reflect.DeepEqual(got, want) || first.Next == nil {
				t.Fatal(got, want, first)
			}
			if first.Next["order"] != string(order) || first.Next["after"] != float64(want[1]) {
				t.Fatal("original cursor shape", first.Next)
			}
			q.Order = ""
			last, err := h.ScanEntries(bg, q, 2, first.Next)
			if err != nil || !reflect.DeepEqual(historyIDs(last), lastWant) || last.Next != nil {
				t.Fatal(last, lastWant, err)
			}
			q.Order = order
			again, err := h.ScanEntries(bg, q, 2, first.Next)
			if err != nil || !reflect.DeepEqual(historyIDs(again), lastWant) {
				t.Fatal(again, err)
			}
			if order == Ascending {
				q.Order = Descending
			} else {
				q.Order = Ascending
			}
			if _, err = h.ScanEntries(bg, q, 2, first.Next); err == nil {
				t.Fatal("accepted opposite direction")
			}
			q.Order = order
			q.MinEntryID = two
			// Original cursors bind only after/order; changed bounds narrow the continuation.
			if _, err = h.ScanEntries(bg, q, 2, first.Next); err != nil {
				t.Fatal("original accepts changed bounds", err)
			}
		}
		// Cursors from pre-1.1 readers retain descending traversal.
		legacy := Cursor{"after": float64(three)}
		p, err := h.ScanEntries(bg, EntryQuery{Conversation: child}, 10, legacy)
		if err != nil || !reflect.DeepEqual(historyIDs(p), []ID{two, one}) {
			t.Fatal(p, err)
		}
		if _, err = h.ScanEntries(bg, EntryQuery{Conversation: child, Order: Ascending}, 10, legacy); err == nil {
			t.Fatal("legacy cursor changed default")
		}
		bounded, err := h.ScanEntries(bg, EntryQuery{Conversation: child, MinEntryID: two, MaxEntryID: three, Order: Ascending}, 10, nil)
		if err != nil || !reflect.DeepEqual(historyIDs(bounded), []ID{two, three}) {
			t.Fatal(bounded, err)
		}
		for _, q := range []EntryQuery{{Conversation: child, Order: "bad"}, {Conversation: child, Order: Ascending, MaxEntryID: one}} {
			p, err := h.ScanEntries(bg, q, 10, nil)
			if q.Order == "bad" {
				if err == nil {
					t.Fatal("invalid order")
				}
			} else if err != nil || !reflect.DeepEqual(historyIDs(p), []ID{one}) {
				t.Fatal(p, err)
			}
		}
		b.store = b.reopen()
		p, err = hAfterReopen(t, b.store, EntryQuery{Conversation: child, Order: Ascending})
		if err != nil || !reflect.DeepEqual(historyIDs(p), []ID{one, two, three, four}) {
			t.Fatal(p, err)
		}
	})
}

func hAfterReopen(t *testing.T, s Storage, q EntryQuery) (Page[Entry], error) {
	return history(t, s).ScanEntries(bg, q, 10, nil)
}

func TestScan110RecordsFiltersTransactionAndDetachment(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		root := mint(t, b.store)
		apply(t, b.store, Write{Op: "create-conversation", Conversation: &Conversation{ID: root}})
		task1, task2 := mint(t, b.store), mint(t, b.store)
		first, second := foundationTask(task1, nil), foundationTask(task2, nil)
		first.Conversation, second.Conversation = root, root
		first.Kind, second.Kind = "custom", "custom"
		first.Execution.Native.AbortRequested = true
		first.Execution.Native.State.Checkpoint = JSON{"nested": JSON{"x": 1}}
		second.Execution.Native.State.Checkpoint = JSON{}
		apply(t, b.store, Write{Op: "put-task", Task: &first}, Write{Op: "put-task", Task: &second})
		owned := mint(t, b.store)
		apply(t, b.store, Write{Op: "create-conversation", Conversation: &Conversation{ID: owned, Owner: task1}})
		sub1, sub2 := mint(t, b.store), mint(t, b.store)
		apply(t, b.store, Write{Op: "put-submission", Submission: &Submission{ID: sub1, Conversation: root, Type: "write", Status: "failed", Value: JSON{"errorCode": "host", "detail": JSON{"x": 1}}}}, Write{Op: "put-submission", Submission: &Submission{ID: sub2, Conversation: root, Type: "write", Status: "failed", Value: JSON{"errorCode": "host", "detail": JSON{"x": 2}}}})
		ordered := b.store.(OrderedStorage)
		for _, order := range []ScanOrder{Ascending, Descending} {
			c, err := ordered.ScanConversations(bg, ConversationQuery{Order: order}, 2, nil)
			if err != nil || c.Next == nil {
				t.Fatal(c, err)
			}
			c2, err := ordered.ScanConversations(bg, ConversationQuery{}, 1, c.Next)
			if err != nil || c2.Next != nil || c.Items[0].ID == c2.Items[0].ID || c.Items[1].ID == c2.Items[0].ID {
				t.Fatal(c2, err)
			}
			tasks, err := ordered.ScanTasks(bg, TaskQuery{Conversation: root, Order: order}, 1, nil)
			if err != nil || tasks.Next == nil {
				t.Fatal(tasks, err)
			}
			wantTask, wantSub := task1, sub1
			if order == Descending {
				wantTask, wantSub = task2, sub2
			}
			if tasks.Items[0].ID != wantTask {
				t.Fatal("task order", tasks)
			}
			opposite := Descending
			if order == Descending {
				opposite = Ascending
			}
			if _, err = ordered.ScanTasks(bg, TaskQuery{Conversation: root, Order: opposite}, 1, tasks.Next); err == nil {
				t.Fatal("task cursor changed direction")
			}
			if p, err := ordered.ScanTasks(bg, TaskQuery{Conversation: root, Kind: "changed"}, 1, tasks.Next); err != nil || len(p.Items) != 0 {
				t.Fatal("original applies changed filters to a continuation", p, err)
			}
			tasks2, err := ordered.ScanTasks(bg, TaskQuery{Conversation: root}, 1, tasks.Next)
			if err != nil || tasks2.Next != nil || tasks.Items[0].ID == tasks2.Items[0].ID {
				t.Fatal(tasks2, err)
			}
			taskCheckpointMap(tasks.Items[0].State.Checkpoint)["mutated"] = true
			again, err := ordered.ScanTasks(bg, TaskQuery{}, 10, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range again.Items {
				if taskCheckpointMap(v.State.Checkpoint)["mutated"] != nil {
					t.Fatal("task alias")
				}
			}
			subs, err := ordered.ScanSubmissions(bg, SubmissionQuery{Order: order}, 1, nil)
			if err != nil || subs.Next == nil {
				t.Fatal(subs, err)
			}
			if subs.Items[0].ID != wantSub {
				t.Fatal("submission order", subs)
			}
			subs2, err := ordered.ScanSubmissions(bg, SubmissionQuery{}, 1, subs.Next)
			if err != nil || subs2.Next != nil || subs.Items[0].ID == subs2.Items[0].ID {
				t.Fatal(subs2, err)
			}
			subs.Items[0].Detail.Value.(map[string]any)["mutated"] = true
			againSub, err := ordered.ScanSubmissions(bg, SubmissionQuery{}, 10, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range againSub.Items {
				if v.Detail.Value.(map[string]any)["mutated"] != nil {
					t.Fatal("submission alias")
				}
			}
		}
		c, err := ordered.ScanConversations(bg, ConversationQuery{OwnerConversation: root, OwnerTask: task1}, 10, nil)
		if err != nil || len(c.Items) != 1 || c.Items[0].ID != owned {
			t.Fatal(c, err)
		}
		yes, no := true, false
		p, err := ordered.ScanTasks(bg, TaskQuery{AbortRequested: &yes, Background: &no, Kind: "custom", Status: "pending"}, 10, nil)
		if err != nil || len(p.Items) != 1 || p.Items[0].ID != task1 {
			t.Fatal(p, err)
		}
		for _, cur := range []Cursor{{"after": "bad"}, {"order": "ascending"}, {"after": 1.0, "order": strings.Repeat("x", 3)}} {
			if _, err := ordered.ScanTasks(bg, TaskQuery{}, 1, cur); err == nil {
				t.Fatal("bad cursor")
			}
		}
		for _, n := range []int{0, DefaultLimits().MaxPage + 1} {
			if _, err := ordered.ScanSubmissions(bg, SubmissionQuery{}, n, nil); err == nil {
				t.Fatal("bad page limit")
			}
		}
		seedRootConversation(t, b.store)
		session, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		var escaped *Tx
		var staged ID
		_, err = session.Commit(bg, func(tx *Tx) (any, error) {
			escaped = tx
			tp, e := tx.ScanTasks(TaskQuery{Order: Descending}, 10, nil)
			if e != nil {
				return nil, e
			}
			if tp.Items[0].ID != task2 {
				t.Fatal(tp)
			}
			sp, e := tx.scanSubmissions(SubmissionQuery{Order: Descending}, 10, nil)
			if e != nil {
				return nil, e
			}
			if sp.Items[0].ID != sub2 {
				t.Fatal(sp)
			}
			staged, e = tx.mintID()
			if e != nil {
				return nil, e
			}
			if e = tx.createConversationRecord(Conversation{ID: staged}); e != nil {
				return nil, e
			}
			if _, e = tx.ScanConversations(ConversationQuery{Order: Descending}, 1, nil); !errors.As(e, new(*ReadAfterWrite)) {
				t.Fatal("source table read-after-write fence missing", e)
			}
			ent, e := tx.mintID()
			if e != nil {
				return nil, e
			}
			if e = tx.appendEntryRecord(Entry{ID: ent, Conversation: staged, Kind: "event", Value: JSON{"x": 1}}); e != nil {
				return nil, e
			}
			if _, e = tx.ScanEntries(EntryQuery{Conversation: staged, Order: Ascending}, 1, nil); !errors.As(e, new(*ReadAfterWrite)) {
				t.Fatal("source entry read-after-write fence missing", e)
			}
			// Internal assembly still owns the staged records; public reads do not.
			candidate, e := tx.current()
			if e != nil {
				return nil, e
			}
			if candidate.Conversations[staged].ID != staged || candidate.Entries[ent].ID != ent {
				t.Fatal("staged record missing")
			}
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = escaped.ScanTasks(TaskQuery{}, 1, nil); !errors.Is(err, ErrSealed) {
			t.Fatal(err)
		}
		if page, err := session.ScanConversations(bg, ConversationQuery{Order: Descending}, 1, nil); err != nil || page.Items[0].ID != staged {
			t.Fatal(page, err)
		}
		if page, err := session.ScanTasks(bg, TaskQuery{Order: Descending}, 1, nil); err != nil || page.Items[0].ID != task2 {
			t.Fatal(page, err)
		}
		if page, err := session.ScanSubmissions(bg, SubmissionQuery{Order: Descending}, 1, nil); err != nil || page.Items[0].ID != sub2 {
			t.Fatal(page, err)
		}
		if page, err := session.ScanEntries(bg, EntryQuery{Conversation: staged}, 1, nil); err != nil || len(page.Items) != 1 {
			t.Fatal(page, err)
		}
		if err = session.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}

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
					var page Page[SubmissionRecord]
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

func TestSQLite110OrderedScanCursorAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "scan.sqlite")
	store, e := OpenSQLite(path, SQLiteOptions{})
	if e != nil {
		t.Fatal(e)
	}
	seedRootConversation(t, store)
	first, second := mint(t, store), mint(t, store)
	apply(t, store, Write{Op: "create-conversation", Conversation: &Conversation{ID: first}}, Write{Op: "create-conversation", Conversation: &Conversation{ID: second}})
	page, e := store.ScanConversations(bg, ConversationQuery{Order: Descending}, 1, nil)
	if e != nil || len(page.Items) != 1 || page.Items[0].ID != second || page.Next == nil {
		t.Fatal(page, e)
	}
	if e = store.Close(bg); e != nil {
		t.Fatal(e)
	}
	store, e = OpenSQLite(path, SQLiteOptions{})
	if e != nil {
		t.Fatal(e)
	}
	seedRootConversation(t, store)
	defer store.Close(bg)
	last, e := store.ScanConversations(bg, ConversationQuery{}, 2, page.Next)
	if e != nil || len(last.Items) != 2 || last.Items[0].ID != first || last.Items[1].ID != 1 || last.Next != nil {
		t.Fatal(last, e)
	}
	if _, e = store.ScanConversations(bg, ConversationQuery{Order: Ascending}, 1, page.Next); e == nil {
		t.Fatal("cursor direction changed after reopen")
	}
}
