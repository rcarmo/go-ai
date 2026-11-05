package durable

import (
	"errors"
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
		apply(t, b.store, Write{Op: "put-task", Task: &Task{ID: task1, Conversation: root, Kind: "custom", Status: "pending", Checkpoint: JSON{"abort": true, "nested": JSON{"x": 1}}}}, Write{Op: "put-task", Task: &Task{ID: task2, Conversation: root, Kind: "custom", Status: "pending", Checkpoint: JSON{}}})
		owned := mint(t, b.store)
		apply(t, b.store, Write{Op: "create-conversation", Conversation: &Conversation{ID: owned, Owner: task1}})
		sub1, sub2 := mint(t, b.store), mint(t, b.store)
		apply(t, b.store, Write{Op: "put-submission", Submission: &Submission{ID: sub1, Conversation: root, Type: "write", Status: "pending", Value: JSON{"x": 1}}}, Write{Op: "put-submission", Submission: &Submission{ID: sub2, Conversation: root, Type: "write", Status: "pending", Value: JSON{"x": 2}}})
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
			tasks.Items[0].Checkpoint["mutated"] = true
			again, err := ordered.ScanTasks(bg, TaskQuery{}, 10, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range again.Items {
				if v.Checkpoint["mutated"] != nil {
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
			subs.Items[0].Value["mutated"] = true
			againSub, err := ordered.ScanSubmissions(bg, SubmissionQuery{}, 10, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range againSub.Items {
				if v.Value["mutated"] != nil {
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
		session, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		var escaped *Tx
		var staged ID
		_, err = session.Commit(bg, func(tx *Tx) error {
			escaped = tx
			var e error
			staged, e = tx.MintID()
			if e != nil {
				return e
			}
			if e = tx.CreateConversation(Conversation{ID: staged}); e != nil {
				return e
			}
			cp, e := tx.ScanConversations(ConversationQuery{Order: Descending}, 1, nil)
			if e != nil {
				return e
			}
			if len(cp.Items) != 1 || cp.Items[0].ID != staged {
				t.Fatal("staged record missing", cp)
			}
			tp, e := tx.ScanTasks(TaskQuery{Order: Descending}, 10, nil)
			if e != nil {
				return e
			}
			if tp.Items[0].ID != task2 {
				t.Fatal(tp)
			}
			sp, e := tx.ScanSubmissions(SubmissionQuery{Order: Descending}, 10, nil)
			if e != nil {
				return e
			}
			if sp.Items[0].ID != sub2 {
				t.Fatal(sp)
			}
			ent, e := tx.MintID()
			if e != nil {
				return e
			}
			if e = tx.AppendEntry(Entry{ID: ent, Conversation: staged, Kind: "event", Value: JSON{"x": 1}}); e != nil {
				return e
			}
			ep, e := tx.ScanEntries(EntryQuery{Conversation: staged, Order: Ascending}, 1, nil)
			if e != nil {
				return e
			}
			if len(ep.Items) != 1 || ep.Items[0].ID != ent {
				t.Fatal(ep)
			}
			return nil
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
