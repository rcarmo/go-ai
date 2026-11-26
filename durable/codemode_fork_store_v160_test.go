package durable

import (
	"context"
	"reflect"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Source codemode.test.ts:888–911. Compare the returned text block, not an
// unstructured substring or decoded JSON. The fork precedes the first write.
func TestSourceV160CodemodeForkScopedStore(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	checkV160CodemodeForkScopedStore(t, engine, false)
}

// Also exercise the saved historical entry after reopening SQLite, using
// the same three source scripts in the verified published portable guest.
func TestSourceV160CodemodePublishedWorkerForkScopedStore(t *testing.T) {
	checkPublishedWorkerForkScopedStore(t, officialWorkerPackageRootV160(t))
}

func TestSourceV161CodemodePublishedWorkerForkScopedStore(t *testing.T) {
	checkPublishedWorkerForkScopedStore(t, officialWorkerPackageRoot(t, "GOAI_CODEMODE_V161_PACKAGE_ROOT", "1.6.1"))
}

func checkPublishedWorkerForkScopedStore(t *testing.T, packageRoot string) {
	t.Helper()
	sandbox, remote := newJournalWorkerSandbox(packageRoot)
	checkV160CodemodeForkScopedStore(t, sandbox, true)
	if got := remote.starts.Load(); got != 3 {
		t.Errorf("portable guest starts = %d; want 3", got)
	}
}

func checkV160CodemodeForkScopedStore(t *testing.T, sandbox CodemodeSandbox, reopenBeforeFork bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(bg, 15*time.Second)
	defer cancel()
	codemode, err := NewCodemodeExtension(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if err = registry.Install(&codemode); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		switch requests.Add(1) {
		case 1:
			ch <- toolAnswer("first-write", "codemode", JSON{"code": `store("k", 1); return load("k")`})
		case 3:
			ch <- toolAnswer("parent-read", "codemode", JSON{"code": `return [load("k") ?? null]`})
		case 5:
			ch <- toolAnswer("fork-read", "codemode", JSON{"code": `return [load("k") ?? null]`})
		default:
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, options)
	conversation := root(t, h, ref)
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	submit := func(c *ConversationHandle, text string) {
		t.Helper()
		submission, err := c.Submit(ctx, Input{Content: text})
		if err != nil {
			t.Fatal(err)
		}
		if settlement, err := submission.Wait(ctx); err != nil || settlement.Submission.Status != "done" {
			t.Fatalf("%s submission = %+v, %v", text, settlement, err)
		}
	}
	submit(conversation, "first")
	page, err := conversation.Entries(ctx, EntryQuery{}, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	var firstUser ID
	for _, entry := range page.Items {
		if entry.Kind == "pi.user" {
			firstUser = entry.ID
			break
		}
	}
	if firstUser == 0 {
		t.Fatal("missing first input entry")
	}
	submit(conversation, "second")
	body := func(c *ConversationHandle, callID string) string {
		t.Helper()
		result := v160ToolResult(t, ctx, c, callID)
		if result.IsError || len(result.Content) != 2 || result.Content[0].Type != "text" || result.Content[1].Type != "text" ||
			!regexp.MustCompile(`^Script completed\nWall time [\d.]+ seconds\nOutput:\n$`).MatchString(result.Content[0].Text) {
			t.Fatalf("%s: expected separate status and result blocks; got %+v", callID, result)
		}
		return result.Content[1].Text
	}
	got := []string{body(conversation, "first-write"), body(conversation, "parent-read")}
	if !reflect.DeepEqual(got, []string{"1", "[1]"}) {
		t.Fatalf("parent store results = %q; want [\"1\" \"[1]\"]", got)
	}
	if reopenBeforeFork {
		if err := h.Close(ctx); err != nil {
			t.Fatal(err)
		}
		h = openHarness(t, reopenStoreAfterHarnessClose(t, store), options)
		conversation, err = h.Conversation(ctx, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Resume(ctx); err != nil {
			t.Fatal(err)
		}
	}
	fork, err := conversation.Fork(ctx, firstUser)
	if err != nil {
		t.Fatal(err)
	}
	submit(fork, "fork")
	if got := body(fork, "fork-read"); got != "[null]" {
		t.Fatalf("fork store result = %q; want [null]", got)
	}
	if got := requests.Load(); got != 6 {
		t.Errorf("model requests = %d; want 6", got)
	}
	// Supplemental: the public state retains independent, rewindable store
	// documents. The parent still has k; the fork's first read created an
	// empty store, not a copy of the parent's current value.
	state, err := h.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[ID]JSON{conversation.ID(): {"values": JSON{"k": 1}}, fork.ID(): {"values": JSON{}}}
	seen := map[ID]bool{}
	for _, doc := range state.Documents {
		if doc.Kind != "pi.codemode.store" {
			continue
		}
		if expected, ok := want[doc.Owner]; !ok || seen[doc.Owner] || !equalJSONValue(doc.Value, expected) || doc.Scope != "conversation" || doc.History != "rewindable" || doc.Fork != "asOf" || doc.Retired {
			t.Errorf("unexpected store: owner=%d scope=%s history=%s fork=%s retired=%t value=%v", doc.Owner, doc.Scope, doc.History, doc.Fork, doc.Retired, doc.Value)
		}
		seen[doc.Owner] = true
	}
	if !seen[conversation.ID()] || !seen[fork.ID()] {
		t.Errorf("store owners = %v; want both parent %d and fork %d", seen, conversation.ID(), fork.ID())
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
}
