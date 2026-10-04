package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func documentStore(t *testing.T, store Storage) DocumentHistoryStorage {
	t.Helper()
	h, ok := store.(DocumentHistoryStorage)
	if !ok {
		t.Fatal("native store lacks document history")
	}
	return h
}
func documentRead(t *testing.T, h DocumentHistoryStorage, id ID, at DocumentPoint, want string) Document {
	t.Helper()
	d, ok, err := h.DocumentAt(bg, id, at)
	if err != nil || !ok || d.Value["value"] != want {
		t.Fatalf("document%d at%+v got%+v present%v err%v want%s", id, at, d, ok, err, want)
	}
	return d
}
func historyDocument(id, owner ID, kind, value, history, fork string) Document {
	return Document{ID: id, Scope: "conversation", Owner: owner, Kind: kind, Version: 1, Value: JSON{"value": value}, History: history, Fork: fork}
}
func putDocument(d Document) Write    { return Write{Op: "put-document", Document: &d} }
func retireDocument(d Document) Write { return Write{Op: "retire-document", Document: &d} }

func TestDocumentHistoryLifetimeVersionDetachedAndReopen(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		s := b.store
		id := mint(t, s)
		d := historyDocument(id, 1, "notes", "initial", "rewindable", "asOf")
		d.Value["nested"] = JSON{"items": []any{"owned"}}
		created := apply(t, s, putDocument(d))
		d.Value["nested"].(JSON)["items"].([]any)[0] = "caller-mutation"
		d.Value = JSON{"value": "changed"}
		changed := apply(t, s, putDocument(d))
		d.Version, d.Value = 9, JSON{"value": "migrated"}
		migrated := apply(t, s, putDocument(d))
		second := d
		second.ID, second.Version, second.Value = mint(t, s), 1, JSON{"value": "new-incarnation"}
		// Creation before retirement and retirement before final old content
		// are deliberately shuffled; one commit produces a half-open lifetime.
		d.Value = JSON{"value": "retiring-final"}
		retired := apply(t, s, putDocument(second), retireDocument(d), putDocument(d))
		verify := func(s Storage) {
			h := documentStore(t, s)
			old := documentRead(t, h, id, DocumentPoint{Seq: created}, "initial")
			if old.CreatedAt != created || old.RetiredAt != retired || old.Value["nested"].(map[string]any)["items"].([]any)[0] != "owned" {
				t.Fatal(old)
			}
			old.Value["nested"].(map[string]any)["items"].([]any)[0] = "read-mutation"
			if documentRead(t, h, id, DocumentPoint{Seq: created}, "initial").Value["nested"].(map[string]any)["items"].([]any)[0] != "owned" {
				t.Fatal("history read alias")
			}
			documentRead(t, h, id, DocumentPoint{Seq: changed}, "changed")
			if documentRead(t, h, id, DocumentPoint{Seq: migrated}, "migrated").Version != 9 {
				t.Fatal("version not historical")
			}
			for _, point := range []DocumentPoint{{Seq: created - 1}, {Seq: retired}, CurrentDocumentPoint()} {
				if _, ok, err := h.DocumentAt(bg, id, point); err != nil || ok {
					t.Fatal("old incarnation visible outside lifetime", point, ok, err)
				}
			}
			address := DocumentAddress{Scope: "conversation", Owner: 1, Kind: "notes"}
			for _, tc := range []struct {
				at uint64
				id ID
			}{{changed, id}, {retired, second.ID}} {
				record, ok, err := h.FindDocument(bg, address, DocumentPoint{Seq: tc.at})
				if err != nil || !ok || record.ID != tc.id || record.Value != nil {
					t.Fatal(record, ok, err)
				}
				page, err := h.ScanDocuments(bg, DocumentQuery{Scope: "conversation", Owner: 1, At: DocumentPoint{Seq: tc.at}}, 10, "")
				if err != nil || len(page.Items) != 1 || page.Items[0].ID != tc.id || page.Items[0].Value != nil {
					t.Fatal(page, err)
				}
			}
			documentRead(t, h, second.ID, CurrentDocumentPoint(), "new-incarnation")
			image := snap(t, s)
			image.DocumentRevisions[id][0].Value["value"] = "snapshot-mutation"
			documentRead(t, h, id, DocumentPoint{Seq: created}, "initial")
		}
		verify(s)
		s = b.reopen()
		verify(s)
	})
}

func TestDocumentHistoryExactFamilyAddressCurrentOnlyAndEmptyLifetime(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		s := b.store
		ws := []Write{}
		want := map[DocumentAddress]ID{}
		for _, key := range []string{"", "__proto__", " \x00exact ", "constructor"} {
			d := historyDocument(mint(t, s), 1, "family", key, "rewindable", "current")
			d.Family, d.Key = true, key
			want[documentAddress(d)] = d.ID
			ws = append(ws, putDocument(d))
		}
		singleton := historyDocument(mint(t, s), 1, "family", "singleton", "latest", "initial")
		want[documentAddress(singleton)] = singleton.ID
		ws = append(ws, putDocument(singleton))
		created := apply(t, s, ws...)
		h := documentStore(t, s)
		for address, id := range want {
			got, ok, err := h.FindDocument(bg, address, CurrentDocumentPoint())
			if err != nil || !ok || got.ID != id {
				t.Fatal(address, got, ok, err)
			}
		}
		q := DocumentQuery{Scope: "conversation", Owner: 1, At: CurrentDocumentPoint(), Kind: "family"}
		page, err := h.ScanDocuments(bg, q, 2, "")
		if err != nil || len(page.Items) != 2 || page.Next == "" {
			t.Fatal(page, err)
		}
		encoded, _ := json.Marshal(page.Next)
		var cursor Cursor
		if err = json.Unmarshal(encoded, &cursor); err != nil {
			t.Fatal(err)
		}
		next, err := h.ScanDocuments(bg, q, 10, cursor)
		if err != nil || len(next.Items) != 3 || next.Next != "" {
			t.Fatal(next, err)
		}
		for _, run := range []func() error{
			func() error { _, _, e := h.DocumentAt(bg, singleton.ID, DocumentPoint{Seq: created}); return e },
			func() error {
				_, _, e := h.FindDocument(bg, DocumentAddress{Scope: "conversation", Kind: "family"}, CurrentDocumentPoint())
				return e
			},
			func() error {
				_, _, e := h.DocumentAt(bg, singleton.ID, DocumentPoint{Current: true, Seq: 1})
				return e
			},
			func() error { _, _, e := h.DocumentAt(bg, ID(MaxID+1), CurrentDocumentPoint()); return e },
			func() error { _, e := h.ScanDocuments(bg, q, 0, ""); return e },
			func() error { _, e := h.ScanDocuments(bg, q, 1, "invalid!"); return e },
			func() error {
				_, e := h.ScanDocuments(bg, DocumentQuery{Scope: "conversation", Owner: 1, At: DocumentPoint{Seq: created}}, 1, cursor)
				return e
			},
		} {
			rejected(t, run())
		}
		// Historical membership of latest docs is allowed; historical content
		// requires rewindable policy, including after retirement.
		if _, ok, err := h.FindDocument(bg, documentAddress(singleton), DocumentPoint{Seq: created}); err != nil || !ok {
			t.Fatal(ok, err)
		}
		empty := historyDocument(mint(t, s), 1, "empty", "never-visible", "rewindable", "asOf")
		emptySeq := apply(t, s, retireDocument(empty), putDocument(empty))
		if _, ok, err := h.FindDocument(bg, documentAddress(empty), DocumentPoint{Seq: emptySeq}); err != nil || ok {
			t.Fatal("empty lifetime membership", ok, err)
		}
		if _, ok, err := h.DocumentAt(bg, empty.ID, DocumentPoint{Seq: emptySeq}); err != nil || ok {
			t.Fatal("empty lifetime content", ok, err)
		}
		if err := s.Close(bg); err != nil {
			t.Fatal(err)
		}
		for _, run := range []func() error{
			func() error { _, _, e := h.DocumentAt(bg, singleton.ID, CurrentDocumentPoint()); return e },
			func() error {
				_, _, e := h.FindDocument(bg, documentAddress(singleton), CurrentDocumentPoint())
				return e
			},
			func() error { _, e := h.ScanDocuments(bg, q, 1, ""); return e },
		} {
			if !errors.Is(run(), ErrClosed) {
				t.Fatal("history read after close")
			}
		}
	})
}

func TestDocumentHistoryCopyIndependentPreBatchAndRollback(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		s := b.store
		firstChild, secondChild := mint(t, s), mint(t, s)
		apply(t, s, Write{Op: "create-conversation", Conversation: &Conversation{ID: firstChild}}, Write{Op: "create-conversation", Conversation: &Conversation{ID: secondChild}})
		source := historyDocument(mint(t, s), 1, "copy.source", "old", "rewindable", "asOf")
		source.Version = 17
		created := apply(t, s, putDocument(source))
		source.Value = JSON{"value": "current"}
		apply(t, s, putDocument(source))
		copyWrite := func(owner ID, point DocumentPoint) (Write, ID) {
			d := source
			d.ID, d.Owner, d.Version, d.Value = mint(t, s), owner, 0, nil
			return Write{Op: "copy-document", Document: &d, Source: &DocumentCopySource{ID: source.ID, At: point}}, d.ID
		}
		a, aid := copyWrite(firstChild, DocumentPoint{Seq: created})
		bcopy, bid := copyWrite(secondChild, CurrentDocumentPoint())
		apply(t, s, a, bcopy)
		h := documentStore(t, s)
		if documentRead(t, h, aid, CurrentDocumentPoint(), "old").Version != 17 || documentRead(t, h, bid, CurrentDocumentPoint(), "current").Version != 17 {
			t.Fatal("stored copy version lost")
		}
		for _, mode := range []string{"change-before", "change-after", "retire", "mismatch", "invalid-point", "non-conversation", "invented-value", "missing-source", "existing-target", "duplicate-target"} {
			t.Run(mode, func(t *testing.T) {
				w, _ := copyWrite(firstChild, CurrentDocumentPoint())
				w.Document.Kind = "copy.source"
				// Retire previous copy in same batch so address collision cannot
				// mask an invalid source or mismatched copy contract.
				old := snap(t, s).Documents[aid]
				ws := []Write{retireDocument(old), w}
				switch mode {
				case "change-before":
					ws = append([]Write{putDocument(source)}, ws...)
				case "change-after":
					ws = append(ws, putDocument(source))
				case "retire":
					ws = append(ws, retireDocument(source))
				case "mismatch":
					w.Document.Fork = "current"
				case "invalid-point":
					w.Source.At = DocumentPoint{Current: true, Seq: created}
				case "non-conversation":
					w.Document.Scope = "session"
					w.Document.Owner = 0
				case "invented-value":
					w.Document.Value = JSON{"injected": true}
				case "missing-source":
					w.Source.ID = ID(MaxID)
				case "existing-target":
					w.Document.ID = bid
					w.Document.Owner = secondChild
					ws = []Write{w}
				case "duplicate-target":
					ws = append(ws, w)
				}
				before := snap(t, s)
				_, err := s.Apply(bg, Batch{Writes: ws})
				rejected(t, err)
				if !equalSnapshot(before, snap(t, s)) {
					t.Fatal("failed copy batch changed tables/history")
				}
			})
		}
		source.Value = JSON{"value": "changed-after-copy"}
		apply(t, s, putDocument(source), retireDocument(source))
		documentRead(t, h, bid, CurrentDocumentPoint(), "current")
		s = b.reopen()
		h = documentStore(t, s)
		documentRead(t, h, aid, CurrentDocumentPoint(), "old")
		documentRead(t, h, bid, CurrentDocumentPoint(), "current")
	})
}

func TestDocumentHistoryTxForkAncestorPoliciesSourceFenceAndReincarnation(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		s := b.store
		cut := mint(t, s)
		asOf := historyDocument(mint(t, s), 1, "fork.asof", "root-at-entry", "rewindable", "asOf")
		current := historyDocument(mint(t, s), 1, "fork.current", "root-current", "latest", "current")
		initial := historyDocument(mint(t, s), 1, "fork.initial", "not-copied", "latest", "initial")
		apply(t, s, entry(cut, JSON{}), putDocument(asOf), putDocument(current), putDocument(initial), entry(mint(t, s), JSON{"excluded": "same-commit"}))
		session, err := OpenSession(s)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close(bg)
		var parent Conversation
		_, err = session.Commit(bg, func(tx *Tx) error { var e error; parent, e = tx.ForkConversation(1, cut, 0); return e })
		if err != nil {
			t.Fatal(err)
		}
		var currentParent Document
		image, _ := session.Snapshot(bg)
		for _, d := range image.Documents {
			if d.Owner == parent.ID && d.Kind == current.Kind {
				currentParent = d
			}
		}
		_, err = session.Commit(bg, func(tx *Tx) error {
			h, e := tx.Document(currentParent.ID)
			if e != nil {
				return e
			}
			return h.Set(JSON{"value": "parent-current"})
		})
		if err != nil {
			t.Fatal(err)
		}
		var child Conversation
		_, err = session.Commit(bg, func(tx *Tx) error { var e error; child, e = tx.ForkConversation(parent.ID, cut, 0); return e })
		if err != nil {
			t.Fatal(err)
		}
		h := documentStore(t, s)
		for _, check := range []struct{ kind, value string }{{asOf.Kind, "root-at-entry"}, {current.Kind, "parent-current"}} {
			d, ok, e := h.FindDocument(bg, DocumentAddress{Scope: "conversation", Owner: child.ID, Kind: check.kind}, CurrentDocumentPoint())
			if e != nil || !ok {
				t.Fatal(d, ok, e)
			}
			documentRead(t, h, d.ID, CurrentDocumentPoint(), check.value)
		}
		if _, ok, e := h.FindDocument(bg, DocumentAddress{Scope: "conversation", Owner: child.ID, Kind: initial.Kind}, CurrentDocumentPoint()); e != nil || ok {
			t.Fatal("initial policy copied", ok, e)
		}
		for _, beforeFork := range []bool{true, false} {
			before, _ := session.Snapshot(bg)
			_, err = session.Commit(bg, func(tx *Tx) error {
				h, e := tx.Document(currentParent.ID)
				if e != nil {
					return e
				}
				if beforeFork {
					if e = h.Set(JSON{"value": "forbidden"}); e != nil {
						return e
					}
				}
				if _, e = tx.ForkConversation(parent.ID, cut, 0); e != nil {
					return e
				}
				if !beforeFork {
					return h.Set(JSON{"value": "forbidden"})
				}
				return nil
			})
			rejected(t, err)
			after, _ := session.Snapshot(bg)
			before.HighWater = after.HighWater
			if !equalSnapshot(before, after) {
				t.Fatal("source fence failed rollback")
			}
		}
		var replacementID ID
		_, err = session.Commit(bg, func(tx *Tx) error {
			fork, e := tx.ForkConversation(1, cut, 0)
			if e != nil {
				return e
			}
			state, e := tx.current()
			if e != nil {
				return e
			}
			for _, d := range state.Documents {
				if d.Owner == fork.ID && d.Kind == asOf.Kind {
					old, e := tx.Document(d.ID)
					if e != nil {
						return e
					}
					if e = old.Retire(); e != nil {
						return e
					}
					replacementID, e = tx.MintID()
					if e != nil {
						return e
					}
					_, e = tx.CreateDocument(historyDocument(replacementID, fork.ID, asOf.Kind, "replacement", "rewindable", "asOf"))
					return e
				}
			}
			return errors.New("copied document missing")
		})
		if err != nil {
			t.Fatal(err)
		}
		documentRead(t, h, replacementID, CurrentDocumentPoint(), "replacement")
	})
}

func TestDocumentHistoryUncertainCopyAndRevisionBudget(t *testing.T) {
	t.Run("landed-copy", func(t *testing.T) {
		s, dir := newJournal(t)
		source := historyDocument(mint(t, s), 1, "copy", "source", "rewindable", "asOf")
		child := mint(t, s)
		apply(t, s, putDocument(source), Write{Op: "create-conversation", Conversation: &Conversation{ID: child}})
		target := source
		target.ID, target.Owner, target.Version, target.Value = mint(t, s), child, 0, nil
		appendFrame := s.appendFrame
		s.appendFrame = func(typ byte, ordinal, high uint64, p []byte) error {
			if e := appendFrame(typ, ordinal, high, p); e != nil {
				return e
			}
			return errors.New("uncertainack")
		}
		_, err := s.Apply(bg, Batch{Writes: []Write{{Op: "copy-document", Document: &target, Source: &DocumentCopySource{source.ID, CurrentDocumentPoint()}}}})
		if !errors.Is(err, ErrPoisoned) {
			t.Fatal(err)
		}
		if _, _, err = s.DocumentAt(bg, target.ID, CurrentDocumentPoint()); !errors.Is(err, ErrPoisoned) {
			t.Fatal("unacknowledged copy published", err)
		}
		if err = s.Close(bg); err != nil {
			t.Fatal(err)
		}
		r, err := OpenJournal(dir, JournalOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close(bg)
		documentRead(t, r, target.ID, CurrentDocumentPoint(), "source")
	})
	t.Run("revision-record-budget", func(t *testing.T) {
		limits := DefaultLimits()
		limits.MaxRecords = 4
		s, err := OpenMemory(MemoryOptions{Limits: &limits})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		d := historyDocument(mint(t, s), 1, "bounded", "one", "rewindable", "initial")
		apply(t, s, putDocument(d))
		d.Value = JSON{"value": "two"}
		apply(t, s, putDocument(d))
		before := snap(t, s)
		d.Value = JSON{"value": "three"}
		_, err = s.Apply(bg, Batch{Writes: []Write{putDocument(d)}})
		rejected(t, err)
		if !equalSnapshot(before, snap(t, s)) {
			t.Fatal("revision budget not atomic")
		}
	})
}

func TestDocumentHistoryPublicForkProductionHTTPAgentAndReopen(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]any
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		data, _ := json.Marshal(body)
		if !strings.Contains(string(data), "agent-at-cut") || !strings.Contains(string(data), "inherited") || strings.Contains(string(data), "agent-current") {
			t.Errorf("wrongforkrequest %s", data)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"fork\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"copied answer\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"fork\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	s, dir := newJournal(t)
	model := fakeModel(goai.ApiOpenAICompletions)
	model.BaseURL = server.URL
	options := Options{Models: func(goai.Provider, string) *goai.Model { return model }, RequestOptions: func(context.Context, ModelRef) (*goai.StreamOptions, error) {
		return &goai.StreamOptions{APIKey: "LOCAL_TEST"}, nil
	}}
	h := openHarness(t, s, options)
	ref := ModelRef{Provider: goai.ProviderOpenAI, ID: model.ID}
	parent, err := h.Root(bg, AgentChange{Model: ref, SystemPrompt: "agent-at-cut"})
	if err != nil {
		t.Fatal(err)
	}
	var cut, app ID
	_, err = parent.Commit(bg, func(tx *Tx) error {
		var e error
		cut, e = tx.MintID()
		if e != nil {
			return e
		}
		value, e := dtoObject(userReceipt("inherited"), tx.limits)
		if e != nil {
			return e
		}
		if e = tx.AppendEntry(Entry{ID: cut, Conversation: 1, Kind: "message", Value: value}); e != nil {
			return e
		}
		app, e = tx.MintID()
		if e != nil {
			return e
		}
		_, e = tx.CreateDocument(historyDocument(app, 1, "app", "at-cut", "rewindable", "asOf"))
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = parent.Configure(bg, AgentChange{Model: ref, SystemPrompt: "agent-current"}); err != nil {
		t.Fatal(err)
	}
	child, err := parent.Fork(bg, cut)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("forkdispatched")
	}
	d, ok, err := s.FindDocument(bg, DocumentAddress{Scope: "conversation", Owner: child.ID(), Kind: "app"}, CurrentDocumentPoint())
	if err != nil || !ok {
		t.Fatal(d, ok, err)
	}
	documentRead(t, s, d.ID, CurrentDocumentPoint(), "at-cut")
	ctx, err := child.Context(bg)
	if err != nil || ctx.SystemPrompt != "agent-at-cut" || len(ctx.Messages) != 1 {
		t.Fatal(ctx, err)
	}
	if err = h.Close(bg); err != nil {
		t.Fatal(err)
	}
	s, err = OpenJournal(dir, JournalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h2 := openHarness(t, s, options)
	child, err = h2.Conversation(bg, child.ID())
	if err != nil {
		t.Fatal(err)
	}
	sub, err := child.Submit(bg, Input{Content: "question", RequestID: "fork"})
	if err != nil {
		t.Fatal(err)
	}
	settled := waitSubmission(t, sub)
	if settled.Submission.Status != "done" || settled.Message.Content[0].Text != "copied answer" || calls.Load() != 1 {
		t.Fatal(settled, calls.Load())
	}
	usage, ok, err := s.FindDocument(bg, DocumentAddress{Scope: "conversation", Owner: 1, Kind: "pi.usage"}, CurrentDocumentPoint())
	if err != nil || !ok {
		t.Fatal(err)
	}
	parentUsage, ok, err := s.DocumentAt(bg, usage.ID, CurrentDocumentPoint())
	if err != nil || !ok || !reflect.DeepEqual(parentUsage.Value, JSON{"models": map[string]any{}, "tools": map[string]any{}}) {
		t.Fatal("child usage attributedparent", parentUsage, err)
	}
}

func TestDocumentHistoryForkFamilyPagesAndDuplicatePolicyRollback(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		s := b.store
		cut := mint(t, s)
		apply(t, s, entry(cut, JSON{}))
		// The separate raised-limit case uses a small page bound and permits
		// one atomic fork with all 260 family members.
		for start := 0; start < 260; start += 100 {
			ws := []Write{}
			for i := start; i < start+100 && i < 260; i++ {
				d := historyDocument(mint(t, s), 1, "pages", fmt.Sprintf("member-%d", i), "latest", "current")
				d.Family, d.Key = true, fmt.Sprintf("member-%d", i)
				ws = append(ws, putDocument(d))
			}
			apply(t, s, ws...)
		}
		// Default MaxWrites256 cannot hold 261 fork commands: reject atomically
		// rather than silently splitting a single transaction or dropping members.
		session, err := OpenSession(s)
		if err != nil {
			t.Fatal(err)
		}
		before, _ := session.Snapshot(bg)
		_, err = session.Commit(bg, func(tx *Tx) error { _, e := tx.ForkConversation(1, cut, 0); return e })
		rejected(t, err)
		after, _ := session.Snapshot(bg)
		before.HighWater = after.HighWater
		if !equalSnapshot(before, after) {
			t.Fatal("oversized fork partially committed")
		}
		if err = session.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("all-members-raised-batch-limit", func(t *testing.T) {
		limits := DefaultLimits()
		limits.MaxWrites = 512
		limits.MaxPage = 2
		s, err := OpenMemory(MemoryOptions{Limits: &limits})
		if err != nil {
			t.Fatal(err)
		}
		session, err := OpenSession(s)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close(bg)
		var cut ID
		_, err = session.Commit(bg, func(tx *Tx) error {
			var e error
			cut, e = tx.MintID()
			if e != nil {
				return e
			}
			return tx.AppendEntry(Entry{ID: cut, Conversation: 1, Kind: "note", Value: JSON{}})
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = session.Commit(bg, func(tx *Tx) error {
			for i := 0; i < 260; i++ {
				id, e := tx.MintID()
				if e != nil {
					return e
				}
				d := historyDocument(id, 1, "pages", fmt.Sprint(i), "latest", "current")
				d.Family, d.Key = true, fmt.Sprint(i)
				if _, e = tx.CreateDocument(d); e != nil {
					return e
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		var child Conversation
		_, err = session.Commit(bg, func(tx *Tx) error { var e error; child, e = tx.ForkConversation(1, cut, 0); return e })
		if err != nil {
			t.Fatal(err)
		}
		var cursor Cursor
		count := 0
		for {
			page, e := s.ScanDocuments(bg, DocumentQuery{Scope: "conversation", Owner: child.ID, At: CurrentDocumentPoint()}, 2, cursor)
			if e != nil {
				t.Fatal(e)
			}
			count += len(page.Items)
			if page.Next == "" {
				break
			}
			cursor = page.Next
		}
		if count != 260 {
			t.Fatal("family copy dropped members", count)
		}
	})
	t.Run("duplicate-asof-current", func(t *testing.T) {
		s, err := NewMemory()
		if err != nil {
			t.Fatal(err)
		}
		cut := mint(t, s)
		d := historyDocument(mint(t, s), 1, "duplicate", "old", "rewindable", "asOf")
		apply(t, s, entry(cut, JSON{}), putDocument(d))
		apply(t, s, retireDocument(d))
		d.ID, d.History, d.Fork = mint(t, s), "latest", "current"
		apply(t, s, putDocument(d))
		session, err := OpenSession(s)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close(bg)
		before, _ := session.Snapshot(bg)
		_, err = session.Commit(bg, func(tx *Tx) error { _, e := tx.ForkConversation(1, cut, 0); return e })
		rejected(t, err)
		after, _ := session.Snapshot(bg)
		before.HighWater = after.HighWater
		if !equalSnapshot(before, after) {
			t.Fatal("duplicate fork partiallyadopted")
		}
	})
}

func TestDocumentHistoryLegacyAgentRejectsUnprovableHistoricalFork(t *testing.T) {
	s, err := NewMemory()
	if err != nil {
		t.Fatal(err)
	}
	cut := mint(t, s)
	agent := Document{ID: mint(t, s), Scope: "conversation", Owner: 1, Kind: "pi.agent", Version: 1, Value: JSON{}}
	apply(t, s, entry(cut, JSON{}), putDocument(agent))
	h := openHarness(t, s, Options{})
	parent, err := h.Conversation(bg, 1)
	if err != nil {
		t.Fatal(err)
	}
	before, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = parent.Fork(bg, cut)
	rejected(t, err)
	after, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	if !equalSnapshot(before, after) {
		t.Fatal("legacy agent refusal changedstate")
	}
}

func TestDocumentHistoryCopyExpandedBatchLimitAndInvalidPolicy(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxFramePayloadBytes = 2048
	limits.MaxRecordBytes = 1024
	limits.MaxDocumentBytes = 768
	limits.MaxStringBytes = 512
	s, err := OpenMemory(MemoryOptions{Limits: &limits})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(bg)
	source := historyDocument(mint(t, s), 1, "expanded", "source", "rewindable", "asOf")
	source.Value["payload"] = strings.Repeat("x", 500)
	apply(t, s, putDocument(source))
	ws := []Write{}
	for i := 0; i < 4; i++ {
		child := mint(t, s)
		apply(t, s, Write{Op: "create-conversation", Conversation: &Conversation{ID: child}})
		target := source
		target.ID, target.Owner, target.Value, target.Version = mint(t, s), child, nil, 0
		ws = append(ws, Write{Op: "copy-document", Document: &target, Source: &DocumentCopySource{ID: source.ID, At: CurrentDocumentPoint()}})
	}
	before := snap(t, s)
	_, err = s.Apply(bg, Batch{Writes: ws})
	rejected(t, err)
	if !equalSnapshot(before, snap(t, s)) {
		t.Fatal("expandedcopybatchpartiallyadopted")
	}
	for _, mode := range []string{"asof-latest", "bad-history", "bad-fork", "invent-created", "invent-retired"} {
		t.Run(mode, func(t *testing.T) {
			d := historyDocument(mint(t, s), 1, "policy", "bad", "rewindable", "asOf")
			switch mode {
			case "asof-latest":
				d.History = "latest"
			case "bad-history":
				d.History = "unknown"
			case "bad-fork":
				d.Fork = "unknown"
			case "invent-created":
				d.CreatedAt = 1
			case "invent-retired":
				d.RetiredAt = 1
			}
			before := snap(t, s)
			_, err := s.Apply(bg, Batch{Writes: []Write{putDocument(d)}})
			rejected(t, err)
			if !equalSnapshot(before, snap(t, s)) {
				t.Fatal("invalidpolicychangedstate")
			}
		})
	}
}

// The high-level Session fork fence covers current-policy parent documents that
// do not yet exist in storage; the low-level copy-source ID fence cannot see them.
func TestDocumentHistoryForkCurrentParentNewDocumentFence(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		s := b.store
		cut, other := mint(t, s), mint(t, s)
		unselectedSource := historyDocument(mint(t, s), other, "copy.unselected", "committed-source", "latest", "current")
		apply(t, s, entry(cut, JSON{}), Write{Op: "create-conversation", Conversation: &Conversation{ID: other}}, putDocument(unselectedSource))
		session, err := OpenSession(s)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close(bg)
		for _, mode := range []string{"new-before", "new-after", "new-update-before", "new-retire-before", "new-retire-after", "copy-before", "copy-after"} {
			t.Run(mode, func(t *testing.T) {
				before, e := session.Snapshot(bg)
				if e != nil {
					t.Fatal(e)
				}
				var forkID, documentID ID
				_, e = session.Commit(bg, func(tx *Tx) error {
					id, e := tx.MintID()
					if e != nil {
						return e
					}
					documentID = id
					// Literal new parent document was absent from the pre-batch
					// selected sources when the fork started.
					d := Document{ID: id, Scope: "conversation", Owner: 1, Kind: "new.current", Version: 1, Value: JSON{"value": "literal-new"}, History: "latest", Fork: "current"}
					if strings.HasSuffix(mode, "after") {
						fork, e := tx.ForkConversation(1, cut, 0)
						if e != nil {
							return e
						}
						forkID = fork.ID
					}
					if strings.HasPrefix(mode, "copy") {
						// Copy an unrelated committed source into the fork parent;
						// the source itself is not in that parent's selected ID set.
						d.Kind = "copy.unselected"
						d.Version = 0
						d.Value = nil
						_, e = tx.CopyDocument(d, DocumentCopySource{ID: unselectedSource.ID, At: CurrentDocumentPoint()})
						if e != nil {
							return e
						}
					} else {
						h, e := tx.CreateDocument(d)
						if e != nil {
							return e
						}
						if mode == "new-update-before" {
							if e = h.Set(JSON{"value": "literal-updated"}); e != nil {
								return e
							}
						}
						if strings.HasPrefix(mode, "new-retire") {
							if e = h.Retire(); e != nil {
								return e
							}
						}
					}
					if strings.HasSuffix(mode, "before") {
						fork, e := tx.ForkConversation(1, cut, 0)
						if e != nil {
							return e
						}
						forkID = fork.ID
					}
					return nil
				})
				rejected(t, e)
				var rejection *StorageRejected
				if !errors.As(e, &rejection) || !strings.Contains(rejection.Reason, "current-policy parent") {
					t.Fatal("wrong rejection masked fence", e)
				}
				after, e := session.Snapshot(bg)
				if e != nil {
					t.Fatal(e)
				}
				before.HighWater = after.HighWater // reservations are deliberately durable
				if !equalSnapshot(before, after) {
					t.Fatal("fork/new parent content adopted despite rejection")
				}
				if _, ok := after.Conversations[forkID]; forkID != 0 && ok {
					t.Fatal("orphan fork")
				}
				if _, ok := after.Documents[documentID]; ok {
					t.Fatal("new parent incarnation leaked")
				}
			})
		}
	})
}

func TestDocumentHistoryForkCurrentFenceKeepsLowLevelCopyAndAncestorWrites(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		s := b.store
		cut, other := mint(t, s), mint(t, s)
		d := historyDocument(mint(t, s), 1, "ancestor.current", "root", "latest", "current")
		apply(t, s, entry(cut, JSON{}), putDocument(d), Write{Op: "create-conversation", Conversation: &Conversation{ID: other}})
		session, e := OpenSession(s)
		if e != nil {
			t.Fatal(e)
		}
		defer session.Close(bg)
		var parent, child Conversation
		_, e = session.Commit(bg, func(tx *Tx) error { var e error; parent, e = tx.ForkConversation(1, cut, 0); return e })
		if e != nil {
			t.Fatal(e)
		}
		// Current policy is resolved from the immediate parent. Mutating an
		// unrelated current-policy document in an older ancestor stays legal.
		_, e = session.Commit(bg, func(tx *Tx) error {
			var e error
			child, e = tx.ForkConversation(parent.ID, cut, 0)
			if e != nil {
				return e
			}
			h, e := tx.Document(d.ID)
			if e != nil {
				return e
			}
			return h.Set(JSON{"value": "ancestor-updated"})
		})
		if e != nil {
			t.Fatal(e)
		}
		h := documentStore(t, s)
		copied, ok, e := h.FindDocument(bg, DocumentAddress{Scope: "conversation", Owner: child.ID, Kind: d.Kind}, CurrentDocumentPoint())
		if e != nil || !ok {
			t.Fatal(copied, ok, e)
		}
		documentRead(t, h, copied.ID, CurrentDocumentPoint(), "root")
		documentRead(t, h, d.ID, CurrentDocumentPoint(), "ancestor-updated")
		// A definition-free low-level copy does not introduce the high-level
		// parent fence: an unrelated current document in its source conversation
		// may change as long as the exact copied source ID remains unchanged.
		_, e = session.Commit(bg, func(tx *Tx) error {
			id, e := tx.MintID()
			if e != nil {
				return e
			}
			target := Document{ID: id, Scope: "conversation", Owner: other, Kind: d.Kind, History: "latest", Fork: "current"}
			if _, e = tx.CopyDocument(target, DocumentCopySource{ID: d.ID, At: CurrentDocumentPoint()}); e != nil {
				return e
			}
			id, e = tx.MintID()
			if e != nil {
				return e
			}
			_, e = tx.CreateDocument(Document{ID: id, Scope: "conversation", Owner: 1, Kind: "unrelated.current", Version: 1, Value: JSON{"value": "allowed"}, History: "latest", Fork: "current"})
			return e
		})
		if e != nil {
			t.Fatal(e)
		}
	})
}
