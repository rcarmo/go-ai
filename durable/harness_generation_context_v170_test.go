package durable

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Public controls for the unchanged pi v1.7.0 generation and context-range
// originals. They deliberately use the production SQLite-backed API rather
// than a translated MemoryStorage or private JavaScript helper oracle.
func TestV170GenerationProviderSessionsStayConversationLocal(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var mu sync.Mutex
		seen := make(map[string][]string)
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, request *goai.Context, stream *goai.StreamOptions) <-chan goai.Event {
			text := ""
			for _, message := range request.Messages {
				if message.Role == goai.RoleUser {
					for _, block := range message.Content {
						if block.Type == "text" {
							text = block.Text
						}
					}
				}
			}
			mu.Lock()
			seen[text] = append(seen[text], stream.SessionID)
			mu.Unlock()
			ch := make(chan goai.Event, 1)
			ch <- terminal("answer:" + text)
			close(ch)
			return ch
		})
		h := openHarness(t, b.store, options)
		first := root(t, h, ref)
		second, err := h.CreateConversation(bg, AgentChange{Model: ref})
		if err != nil {
			t.Fatal(err)
		}
		for round := 1; round <= 2; round++ {
			var settled sync.WaitGroup
			settled.Add(2)
			for index, conversation := range []*ConversationHandle{first, second} {
				index, conversation := index, conversation
				go func() {
					defer settled.Done()
					sub, err := conversation.Submit(bg, Input{Content: string(rune('0'+round)) + "-" + string(rune('0'+index))})
					if err != nil {
						t.Error(err)
						return
					}
					if result, err := sub.Wait(context.Background()); err != nil || result.Record.Status != "done" {
						t.Errorf("round %d conversation %d: %+v, %v", round, index, result.Record, err)
					}
				}()
			}
			settled.Wait()
		}
		mu.Lock()
		defer mu.Unlock()
		firstID := seen["1-0"]
		secondID := seen["1-1"]
		if len(firstID) != 1 || len(secondID) != 1 || firstID[0] == "" || secondID[0] == "" || firstID[0] == secondID[0] {
			t.Fatalf("provider sessions were not conversation-local: first=%v second=%v", firstID, secondID)
		}
		for _, key := range []string{"1-0", "2-0"} {
			if len(seen[key]) != 1 || seen[key][0] != firstID[0] {
				t.Fatalf("first conversation session %q = %v; want %q", key, seen[key], firstID[0])
			}
		}
		for _, key := range []string{"1-1", "2-1"} {
			if len(seen[key]) != 1 || seen[key][0] != secondID[0] {
				t.Fatalf("second conversation session %q = %v; want %q", key, seen[key], secondID[0])
			}
		}
	})
}

type v170PausedEntryScan struct {
	Storage
	enabled atomic.Bool
	paused  chan struct{}
	release chan struct{}
}

func (s *v170PausedEntryScan) ScanEntries(ctx context.Context, query EntryQuery, size int, cursor Cursor) (Page[Entry], error) {
	page, err := s.Storage.ScanEntries(ctx, query, size, cursor)
	if err == nil && query.Conversation != 0 && s.enabled.CompareAndSwap(true, false) {
		close(s.paused)
		select {
		case <-s.release:
		case <-ctx.Done():
			return Page[Entry]{}, ctx.Err()
		}
	}
	return page, err
}

func TestV170ContextRangeRescansEntryCommittedDuringOffLineScan(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		adapter := &v170PausedEntryScan{Storage: b.store, paused: make(chan struct{}), release: make(chan struct{})}
		h := openHarness(t, adapter, Options{})
		conversation := root(t, h, ModelRef{})
		appendEntry := func(role goai.Role, text string) ID {
			t.Helper()
			entry, err := conversation.Commit(bg, func(tx *Tx) (any, error) {
				return tx.AppendEntry(conversation.ID(), EntryDraft{
					"kind": "app.message",
					"model": []any{JSON{
						"role": role, "content": []any{JSON{"type": "text", "text": text}}, "timestamp": int64(1),
					}},
				})
			})
			if err != nil {
				t.Fatal(err)
			}
			return entry.(Entry).ID
		}
		appendEntry(goai.RoleUser, "first")
		adapter.enabled.Store(true)
		read := make(chan ContextView, 1)
		failure := make(chan error, 1)
		go func() {
			view, err := conversation.ContextView(bg, 0)
			if err != nil {
				failure <- err
				return
			}
			read <- view
		}()
		select {
		case <-adapter.paused:
		case <-time.After(3 * time.Second):
			t.Fatal("entry scan did not pause")
		}
		appendEntry(goai.RoleUser, "missed")
		close(adapter.release)
		var first ContextView
		select {
		case err := <-failure:
			t.Fatal(err)
		case first = <-read:
		case <-time.After(3 * time.Second):
			t.Fatal("first context read did not finish")
		}
		appendEntry(goai.RoleUser, "last")
		again, err := conversation.ContextView(bg, 0)
		if err != nil {
			t.Fatal(err)
		}
		fresh, err := conversation.ContextView(bg, 0)
		if err != nil {
			t.Fatal(err)
		}
		got := func(view ContextView) []string {
			var texts []string
			for _, message := range view.Messages {
				for _, block := range message.Content {
					if block.Type == "text" {
						texts = append(texts, block.Text)
					}
				}
			}
			return texts
		}
		if values := got(first); len(values) != 1 || values[0] != "first" {
			t.Fatalf("off-line scan absorbed concurrent entry: %v", values)
		}
		if values := got(again); len(values) != 3 || values[0] != "first" || values[1] != "missed" || values[2] != "last" {
			t.Fatalf("incremental context skipped entry: %v", values)
		}
		if values, freshValues := got(again), got(fresh); len(values) != len(freshValues) {
			t.Fatalf("kept and fresh contexts differ: %v, %v", values, freshValues)
		} else {
			for index := range values {
				if values[index] != freshValues[index] {
					t.Fatalf("kept and fresh contexts differ: %v, %v", values, freshValues)
				}
			}
		}
	})
}
