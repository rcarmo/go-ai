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

func history(t *testing.T, s Storage) HistoryStorage {
	t.Helper()
	v, ok := s.(HistoryStorage)
	if !ok {
		t.Fatal("native store lacks history surface")
	}
	return v
}
func historyIDs(page Page[Entry]) []ID {
	r := []ID{}
	for _, e := range page.Items {
		r = append(r, e.ID)
	}
	return r
}
func requireHistory(t *testing.T, h HistoryStorage, q EntryQuery, limit int, cursor Cursor, want ...ID) Page[Entry] {
	t.Helper()
	page, err := h.ScanEntries(bg, q, limit, cursor)
	if err != nil || !reflect.DeepEqual(historyIDs(page), want) {
		t.Fatalf("history got%v want%v err%v", historyIDs(page), want, err)
	}
	return page
}

func TestHistoryForkAncestryPagesHeadAndReopen(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s := b.store
		first, cut, excluded := mint(t, s), mint(t, s), mint(t, s)
		rootSeq := apply(t, s, entry(excluded, JSON{}), entry(first, JSON{"nested": JSON{"value": "original"}}), Write{Op: "append-entry", Entry: &Entry{ID: cut, Conversation: 1, Kind: "marker", Value: JSON{}, Head: first}})
		child := mint(t, s)
		apply(t, s, Write{Op: "create-conversation", Conversation: &Conversation{ID: child, Parent: 1, ParentAt: cut}})
		childCut, childExcluded := mint(t, s), mint(t, s)
		apply(t, s, Write{Op: "append-entry", Entry: &Entry{ID: childCut, Conversation: child, Kind: "note", Value: JSON{}}}, Write{Op: "append-entry", Entry: &Entry{ID: childExcluded, Conversation: child, Kind: "note", Value: JSON{}}})
		rootLater := mint(t, s)
		apply(t, s, entry(rootLater, JSON{}))
		grandchild := mint(t, s)
		apply(t, s, Write{Op: "create-conversation", Conversation: &Conversation{ID: grandchild, Parent: child, ParentAt: childCut}})
		head, tail := mint(t, s), mint(t, s)
		grandSeq := apply(t, s, Write{Op: "append-entry", Entry: &Entry{ID: tail, Conversation: grandchild, Kind: "note", Value: JSON{}}}, Write{Op: "append-entry", Entry: &Entry{ID: head, Conversation: grandchild, Kind: "marker", Value: JSON{}, Head: head}})
		childLater := mint(t, s)
		apply(t, s, Write{Op: "append-entry", Entry: &Entry{ID: childLater, Conversation: child, Kind: "note", Value: JSON{}}})
		verify := func(s Storage) {
			h := history(t, s)
			q := EntryQuery{Conversation: grandchild}
			page := requireHistory(t, h, q, 2, nil, tail, head)
			if page.Next == nil {
				t.Fatal("missing continuation")
			}
			encoded, _ := json.Marshal(page.Next)
			var roundTrip Cursor
			if err := json.Unmarshal(encoded, &roundTrip); err != nil {
				t.Fatal(err)
			}
			page = requireHistory(t, h, q, 2, roundTrip, childCut, cut)
			page = requireHistory(t, h, q, 2, page.Next, first)
			if page.Next != nil {
				t.Fatal("continuation after end")
			}
			for _, check := range []struct{ at, id, head ID }{{0, head, head}, {childCut, cut, first}} {
				marker, ok, err := h.FindLatestHeadMarker(bg, grandchild, check.at)
				if err != nil || !ok || marker.ID != check.id || marker.Head != check.head {
					t.Fatal(marker, ok, err)
				}
			}
			if _, ok, err := h.FindLatestHeadMarker(bg, grandchild, first); err != nil || ok {
				t.Fatal("unexpected earlier marker", ok, err)
			}
			requireHistory(t, h, EntryQuery{Conversation: grandchild, MinEntryID: head}, 10, nil, tail, head)
			requireHistory(t, h, EntryQuery{Conversation: grandchild, MinEntryID: first, MaxEntryID: childCut}, 10, nil, childCut, cut, first)
			for _, check := range []struct {
				id, owner ID
				seq       uint64
			}{{first, 1, rootSeq}, {childCut, child, rootSeq + 2}, {tail, grandchild, grandSeq}} {
				v, ok, err := h.Entry(bg, grandchild, check.id)
				if err != nil || !ok || v.Entry.Conversation != check.owner || v.CommitSeq != check.seq {
					t.Fatal(v, ok, err)
				}
			}
			for _, id := range []ID{excluded, rootLater, childExcluded, childLater, ID(MaxID)} {
				if _, ok, err := h.Entry(bg, grandchild, id); err != nil || ok {
					t.Fatal("cut entry became visible", id, ok, err)
				}
			}
			if _, ok, err := h.Entry(bg, 1, head); err != nil || ok {
				t.Fatal("child visible from parent", ok, err)
			}
			v, _, _ := h.Entry(bg, grandchild, first)
			v.Entry.Value["nested"].(map[string]any)["value"] = "mutated"
			page = requireHistory(t, h, EntryQuery{Conversation: grandchild, MaxEntryID: first}, 1, nil, first)
			if page.Items[0].Value["nested"].(map[string]any)["value"] != "original" {
				t.Fatal("visible lookup aliases storage")
			}
			page.Items[0].Value["nested"].(map[string]any)["value"] = "again"
			v, _, _ = h.Entry(bg, grandchild, first)
			if v.Entry.Value["nested"].(map[string]any)["value"] != "original" {
				t.Fatal("scan aliases storage")
			}
		}
		verify(s)
		// A descendant may fork at an entry owned by an older ancestor. The
		// immediate parent still controls current-copy policy in the next slice.
		ancestorFork := mint(t, s)
		apply(t, s, Write{Op: "create-conversation", Conversation: &Conversation{ID: ancestorFork, Parent: grandchild, ParentAt: first}})
		requireHistory(t, history(t, s), EntryQuery{Conversation: ancestorFork}, 10, nil, first)
		s = b.reopen()
		verify(s)
	})
}

func TestHistoryCursorStableNewCommitAndValidation(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s := b.store
		one, two, three := mint(t, s), mint(t, s), mint(t, s)
		apply(t, s, entry(three, JSON{}), entry(one, JSON{}), entry(two, JSON{}))
		h := history(t, s)
		q := EntryQuery{Conversation: 1}
		page := requireHistory(t, h, q, 2, nil, three, two)
		apply(t, s, entry(mint(t, s), JSON{}))
		next := requireHistory(t, h, q, 2, page.Next, one)
		if next.Next != nil {
			t.Fatal("unexpected continuation")
		}
		for _, run := range []func() error{
			func() error { _, e := h.ScanEntries(bg, q, 0, nil); return e },
			func() error { _, e := h.ScanEntries(bg, q, 1, Cursor{"after": strings.Repeat("x", 3)}); return e },
			func() error { _, e := h.ScanEntries(bg, q, 1, Cursor{"order": "descending"}); return e },
			func() error { _, e := h.ScanEntries(bg, EntryQuery{Conversation: ID(MaxID)}, 1, nil); return e },
			func() error { _, _, e := h.Entry(bg, ID(MaxID), one); return e },
			func() error { _, _, e := h.FindLatestHeadMarker(bg, 1, ID(MaxID+1)); return e },
		} {
			rejected(t, run())
		}
		if _, found, err := h.Entry(bg, 1, 0); err != nil || found {
			t.Fatal("missing zero entry", found, err)
		}
		if err := s.Close(bg); err != nil {
			t.Fatal(err)
		}
		for _, run := range []func() error{
			func() error { _, e := h.ScanEntries(bg, q, 1, nil); return e },
			func() error { _, _, e := h.Entry(bg, 1, one); return e },
			func() error { _, _, e := h.FindLatestHeadMarker(bg, 1, 0); return e },
		} {
			if !errors.Is(run(), ErrClosed) {
				t.Fatal("history accepts read after close")
			}
		}
	})
}

func TestHistoryAncestryFailureAtomicAndTxForwardReferences(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s := b.store
		cut := mint(t, s)
		apply(t, s, entry(cut, JSON{}))
		for _, mode := range []string{"missing-parent", "missing-entry", "self", "cycle", "no-parent", "bad-id", "invisible-cut", "future-head", "foreign-head"} {
			t.Run(mode, func(t *testing.T) {
				child, other, id := mint(t, s), mint(t, s), mint(t, s)
				ws := []Write{{Op: "create-conversation", Conversation: &Conversation{ID: child, Parent: 1, ParentAt: cut}}, {Op: "create-conversation", Conversation: &Conversation{ID: other}}, {Op: "append-entry", Entry: &Entry{ID: id, Conversation: child, Kind: "note", Value: JSON{}}}}
				switch mode {
				case "missing-parent":
					ws[0].Conversation.Parent = ID(MaxID)
				case "missing-entry":
					ws[0].Conversation.ParentAt = ID(MaxID)
				case "self":
					ws[0].Conversation.Parent = child
				case "cycle":
					ws[0].Conversation.Parent = other
					ws[1].Conversation.Parent = child
					ws[1].Conversation.ParentAt = cut
				case "no-parent":
					ws[0].Conversation.Parent = 0
				case "bad-id":
					ws[0].Conversation.ParentAt = ID(MaxID + 1)
				case "invisible-cut":
					ws[0].Conversation.Parent = other
				case "future-head":
					ws[2].Entry.Head = ID(MaxID)
				case "foreign-head":
					ws = append(ws, Write{Op: "append-entry", Entry: &Entry{ID: mint(t, s), Conversation: other, Kind: "note", Value: JSON{}, Head: cut}})
				}
				before := snap(t, s)
				_, err := s.Apply(bg, Batch{Writes: ws})
				rejected(t, err)
				if !equalSnapshot(before, snap(t, s)) {
					t.Fatal("failed ancestry/head batch adopted table writes")
				}
			})
		}
		seedRootConversation(t, s)
		session, err := OpenSession(s)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close(bg)
		var child, forward ID
		_, err = session.Commit(bg, func(tx *Tx) (any, error) {
			var err error
			child, err = tx.MintID()
			if err != nil {
				return nil, err
			}
			forward, err = tx.MintID()
			if err != nil {
				return nil, err
			}
			if err = tx.CreateConversation(Conversation{ID: child, Parent: 1, ParentAt: forward}); err != nil {
				return nil, err
			}
			return nil, tx.appendEntryRecord(Entry{ID: forward, Conversation: 1, Kind: "note", Value: JSON{}, Head: forward})
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok, err := history(t, s).Entry(bg, child, forward); err != nil || !ok {
			t.Fatal(ok, err)
		}
	})
}

func TestHistoryProductionHTTPInheritedContextAndHead(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		data, _ := json.Marshal(body)
		for _, want := range []string{"inherited", "question"} {
			if !strings.Contains(string(data), want) {
				t.Errorf("missing %s: %s", want, data)
			}
		}
		for _, excluded := range []string{"before-head", "excluded-same-commit", "parent-later"} {
			if strings.Contains(string(data), excluded) {
				t.Errorf("excluded %s: %s", excluded, data)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"fork-answer\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"fork answer\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"fork-answer\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	s, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	model := fakeModel(goai.ApiOpenAICompletions)
	model.BaseURL = server.URL
	h := openHarness(t, s, Options{Models: func(goai.Provider, string) *goai.Model { return model }, RequestOptions: func(context.Context, ModelRef) (*goai.StreamOptions, error) {
		return &goai.StreamOptions{APIKey: "LOCAL_TEST"}, nil
	}})
	parent := root(t, h, ModelRef{Provider: goai.ProviderOpenAI, ID: model.ID})
	var childID ID
	_, err = parent.Commit(bg, func(tx *Tx) (any, error) {
		for _, text := range []string{"before-head", "inherited", "excluded-same-commit"} {
			id, err := tx.MintID()
			if err != nil {
				return nil, err
			}
			value, err := dtoObject(messageReceipt{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: text}}}, tx.limits)
			if err != nil {
				return nil, err
			}
			e := Entry{ID: id, Conversation: 1, Kind: "message", Value: value}
			if text == "inherited" {
				e.Head = id
				childID, err = tx.MintID()
				if err != nil {
					return nil, err
				}
				if err = tx.CreateConversation(Conversation{ID: childID, Parent: 1, ParentAt: id}); err != nil {
					return nil, err
				}
			}
			if err = tx.appendEntryRecord(e); err != nil {
				return nil, err
			}
		}
		id, err := tx.MintID()
		if err != nil {
			return nil, err
		}
		value, err := dtoObject(messageReceipt{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "parent-later"}}}, tx.limits)
		if err != nil {
			return nil, err
		}
		return nil, tx.appendMessage(id, 1, 0, value)
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := h.Conversation(bg, childID)
	if err != nil {
		t.Fatal(err)
	}
	if err = child.Configure(bg, AgentChange{Model: ModelRef{Provider: goai.ProviderOpenAI, ID: model.ID}, SystemPrompt: "child"}); err != nil {
		t.Fatal(err)
	}
	ctx, err := child.Context(bg)
	if err != nil || len(ctx.Messages) != 1 || ctx.Messages[0].Content[0].Text != "inherited" {
		t.Fatal(ctx, err)
	}
	sub, err := child.Submit(bg, Input{Content: "question", RequestID: "fork-request"})
	if err != nil {
		t.Fatal(err)
	}
	settled := waitSubmission(t, sub)
	if settled.Submission.Status != "done" || settled.Message == nil || settled.Message.Content[0].Text != "fork answer" || calls.Load() != 1 {
		t.Fatal(settled, calls.Load())
	}
}

func TestHistoryUncertainSQLiteWinnerAndCorruptAncestry(t *testing.T) {
	t.Run("uncertain-landed-fork", func(t *testing.T) {
		s, dir := newSQLiteTestStore(t)
		cut := mint(t, s)
		apply(t, s, entry(cut, JSON{}))
		child := mint(t, s)
		appendFrame := s.appendFrame
		s.appendFrame = func(typ byte, ordinal, high uint64, payload []byte) error {
			if err := appendFrame(typ, ordinal, high, payload); err != nil {
				return err
			}
			return fmt.Errorf("unknown acknowledgement")
		}
		_, err := s.Apply(bg, Batch{Writes: []Write{{Op: "create-conversation", Conversation: &Conversation{ID: child, Parent: 1, ParentAt: cut}}}})
		if !errors.Is(err, ErrPoisoned) {
			t.Fatal(err)
		}
		if _, err = s.ScanEntries(bg, EntryQuery{Conversation: child}, 1, nil); !errors.Is(err, ErrPoisoned) {
			t.Fatal("poisoned history published", err)
		}
		if err = s.Close(bg); err != nil {
			t.Fatal(err)
		}
		r, err := openSQLiteTestDirectory(dir, SQLiteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close(bg)
		requireHistory(t, r, EntryQuery{Conversation: child}, 1, nil, cut)
	})
	t.Run("valid-frame-invalid-ancestry", func(t *testing.T) {
		s, dir := newSQLiteTestStore(t)
		cut, child := mint(t, s), mint(t, s)
		apply(t, s, entry(cut, JSON{}))
		ordinal, high, seq := s.ordinal, s.state.HighWater, s.state.Seq
		payload := []byte(fmt.Sprintf(`{"seq":%d,"writes":[{"op":"create-conversation","conversation":{"id":%d,"parent":1,"parentAt":%d}}]}`, seq+1, child, child))
		if err := s.exec("INSERT INTO frames(ordinal,kind,highwater,payload) VALUES(?,?,?,?)", int64(ordinal+1), 2, int64(high), payload); err != nil {
			t.Fatal(err)
		}
		if err := s.Close(bg); err != nil {
			t.Fatal(err)
		}
		r, err := openSQLiteTestDirectory(dir, SQLiteOptions{})
		if r != nil {
			_ = r.Close(bg)
		}
		if !errors.Is(err, ErrCorrupt) {
			t.Fatal("invalid ancestry replay accepted", err)
		}
	})
}
