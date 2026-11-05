package durable

import (
	"context"
	"errors"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Native stored message kinds carry the role in Value; this maps that data-only
// envelope to the source entry kind for assertion comparison, not filtering.
func v150HistoryKinds(t *testing.T, entries []Entry) []string {
	t.Helper()
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		kind := entry.Kind
		if kind == "message" {
			var message MessageReceipt
			if err := fromObject(entry.Value, &message, DefaultLimits()); err != nil {
				t.Fatal(err)
			}
			switch message.Role {
			case goai.RoleUser:
				kind = "pi.user"
			case goai.RoleAssistant:
				kind = "pi.assistant"
			case goai.RoleToolResult:
				kind = "pi.tool-result"
			case goai.RoleSystem:
				kind = "pi.system"
			}
		}
		out = append(out, kind)
	}
	return out
}

// Complete pinned harness-hook-reads.test.ts:18 scenario: the hook sees only the
// committed input before the answer is stored, including single/typed reads.
func TestV150HookReadsCommittedRawHistory(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	userToken, err := DefineEntry("message")
	if err != nil {
		t.Fatal(err)
	}
	wrongToken, err := DefineEntry("pi.generation")
	if err != nil {
		t.Fatal(err)
	}
	var seen [][]string
	var captured *HookAPI
	registry := NewRegistry()
	if err := registry.Install(&Extension{Name: "reader", Hooks: GenerationHooks{OnYield: func(ctx context.Context, _ MessageReceipt, api *HookAPI) (*Input, error) {
		captured = api
		page, err := api.Entries(ctx, EntryQuery{Order: Ascending}, 10, nil)
		if err != nil {
			return nil, err
		}
		seen = append(seen, v150HistoryKinds(t, page.Items))
		if len(page.Items) != 1 {
			t.Errorf("pre-answer entries=%v", page.Items)
			return nil, nil
		}
		entry, found, err := api.TypedEntry(ctx, userToken, page.Items[0].ID)
		if err != nil || !found || !reflect.DeepEqual(v150HistoryKinds(t, []Entry{entry}), []string{"pi.user"}) {
			t.Errorf("user read found=%v err=%v", found, err)
		}
		if _, found, err := api.TypedEntry(ctx, wrongToken, page.Items[0].ID); err != nil || found {
			t.Errorf("wrong token found=%v err=%v", found, err)
		}
		return nil, nil
	}}}); err != nil {
		t.Fatal(err)
	}
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		ch <- terminal("done")
		close(ch)
		return ch
	})
	options.Registry = registry
	h := openHarness(t, store, options)
	conversation := root(t, h, ref)
	submission, err := conversation.Submit(bg, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if result := waitSubmission(t, submission); result.Record.Status != "done" {
		t.Fatal(result)
	}
	if err := conversation.WaitForIdle(bg); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen, [][]string{{"pi.user"}}) {
		t.Fatalf("hook history=%v", seen)
	}
	if captured == nil {
		t.Fatal("hook not invoked")
	}
	if _, err := captured.Entries(bg, EntryQuery{}, 10, nil); !errors.Is(err, ErrSealed) {
		t.Fatalf("escaped hook history=%v", err)
	}
}

// Complete pinned harness-hook-reads.test.ts:51 scenario: two one-entry pages,
// continuation termination and an inclusive one-ID range from an actual task.
func TestV150TaskPagesBoundedRawHistory(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	var pages [][]ID
	var captured *TaskRuntime
	reader := taskDefinition(t, "test.reader", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		captured = r
		first, err := r.Entries(ctx, EntryQuery{Order: Ascending}, 1, nil)
		if err != nil {
			return err
		}
		if len(first.Items) != 1 || first.Next == nil {
			t.Errorf("first page=%+v", first)
			return nil
		}
		second, err := r.Entries(ctx, EntryQuery{Order: Ascending}, 1, first.Next)
		if err != nil {
			return err
		}
		if len(second.Items) != 1 || second.Next != nil {
			t.Errorf("second page=%+v", second)
			return nil
		}
		bounded, err := r.Entries(ctx, EntryQuery{MinEntryID: second.Items[0].ID, MaxEntryID: second.Items[0].ID}, 10, nil)
		if err != nil {
			return err
		}
		for _, page := range []Page[Entry]{first, second, bounded} {
			ids := []ID{}
			for _, entry := range page.Items {
				ids = append(ids, entry.ID)
			}
			pages = append(pages, ids)
		}
		// Native caller-control and ownership checks supplement the source scenario.
		if _, err := r.Entries(ctx, EntryQuery{Conversation: ID(MaxID)}, 1, nil); err == nil {
			t.Error("foreign conversation accepted")
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := r.Entries(canceled, EntryQuery{}, 1, nil); !errors.Is(err, context.Canceled) {
			t.Errorf("canceled history=%v", err)
		}
		first.Items[0].Value["mutated"] = true
		again, _, err := r.Entry(ctx, first.Items[0].ID)
		if err != nil || again.Value["mutated"] != nil {
			t.Errorf("history aliases stored data: %v", err)
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	registry := NewRegistry()
	if _, err := registry.RegisterTask(reader); err != nil {
		t.Fatal(err)
	}
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		ch <- terminal("done")
		close(ch)
		return ch
	})
	options.Registry = registry
	h := openHarness(t, store, options)
	conversation := root(t, h, ref)
	submission, err := conversation.Submit(bg, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	waitSubmission(t, submission)
	id := createPublicTask(t, h, reader, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	if result := waitPublicTask(t, h, id); result.State.Outcome.Status != "completed" {
		t.Fatal(result)
	}
	all, err := conversation.ScanEntries(bg, EntryQuery{Order: Ascending}, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v150HistoryKinds(t, all.Items), []string{"pi.user", "pi.assistant"}) {
		t.Fatal("raw history kinds", v150HistoryKinds(t, all.Items))
	}
	if len(all.Items) != 2 || !reflect.DeepEqual(pages, [][]ID{{all.Items[0].ID}, {all.Items[1].ID}, {all.Items[1].ID}}) {
		t.Fatalf("pages=%v all=%+v", pages, all)
	}
	if _, err := captured.Entries(bg, EntryQuery{}, 1, nil); !errors.Is(err, ErrSealed) {
		t.Fatalf("ended task history=%v", err)
	}
}
