package durable

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// packages/durable/test/codemode.test.ts:214–229. The published portable
// guest replays the already completed nested call, then retries only hold.
func TestSourceV160CodemodePublishedWorkerCompletedCallReplay(t *testing.T) {
	const source = `const a = await tools.echo({ text: "a" });
const held = await tools.hold({ id: 1 });
const b = await tools.echo({ text: "b" });
return [a, held, b, Date.now() > 0];`
	got := runV160PublishedWorkerNestedReplay(t, source, workerReplayChangesV160{})
	if got.isError || got.text != `["echo a","held 1","echo b",true]` {
		t.Errorf("result = %q, error=%t; want [\"echo a\",\"held 1\",\"echo b\",true]", got.text, got.isError)
	}
	if !reflect.DeepEqual(got.echoes, map[string]int{"a": 1, "b": 1}) {
		t.Errorf("echo effects = %v; want map[a:1 b:1]", got.echoes)
	}
	if got.starts != 2 {
		t.Errorf("guest starts = %d; want 2", got.starts)
	}
	if got.holds != 2 {
		t.Errorf("hold runs = %d; want 2", got.holds)
	}
	// Supplemental: before the close, only echo a had a journaled result.
	if len(got.beforeClose) != 1 || got.beforeClose[0].Call.Name != "echo" || !got.beforeClose[0].HasValue || got.beforeClose[0].Value != "echo a" {
		t.Errorf("committed deliveries at hold = %+v; want only echo a", got.beforeClose)
	}
}

// packages/durable/test/codemode.test.ts:261–275. The source only observes the
// echo after the hold; the start receipts separately verify that its clock
// and random seed were pinned on both guest executions.
func TestSourceV160CodemodePublishedWorkerClockAndRandomness(t *testing.T) {
	const source = `const before = [Date.now(), Math.random()];
await tools.hold({});
await tools.echo({ text: String(before[0]) + "/" + String(before[1]) });
return "ok";`
	got := runV160PublishedWorkerNestedReplay(t, source, workerReplayChangesV160{})
	if got.text != "ok" {
		t.Errorf("result = %q; want ok", got.text)
	}
	var counts []int
	for _, n := range got.echoes {
		counts = append(counts, n)
	}
	if !reflect.DeepEqual(counts, []int{1}) {
		t.Errorf("echo counts = %v; want [1]", counts)
	}
	if len(got.inputs) != 2 || got.inputs[0] != got.inputs[1] {
		t.Fatalf("guest clock and seed inputs = %+v; want two identical starts", got.inputs)
	}
	for sample := range got.echoes {
		tick, random, found := strings.Cut(sample, "/")
		fraction, err := strconv.ParseFloat(random, 64)
		if !found || err != nil || tick != strconv.FormatInt(got.inputs[0].clock, 10) || !(fraction >= 0 && fraction < 1) {
			t.Errorf("guest clock/random sample = %q; want %d and a number in [0,1)", sample, got.inputs[0].clock)
		}
	}
}

type workerReplayInputsV160 struct {
	clock int64
	seed  uint64
}

type workerNestedReplayV160 struct {
	text        string
	isError     bool
	echoes      map[string]int
	starts      int32
	holds       int32
	replies     int32
	nested      int
	beforeClose []CodemodeDelivery
	inputs      []workerReplayInputsV160
}

type workerReplayChangesV160 struct {
	guestCode    func(int32, string) string
	guestStore   func(int32, JSON) JSON
	globals      []CodemodeGlobal
	now          func() int64
	beforeStart  func(*RemoteCodemodeSandbox)
	beforeResume func(context.Context, Storage, *RemoteCodemodeSandbox)
}

func runV160PublishedWorkerNestedReplay(t *testing.T, source string, changes workerReplayChangesV160) workerNestedReplayV160 {
	t.Helper()
	sandbox, remote := newJournalWorkerSandboxV160(t)
	remote.guestCode = changes.guestCode
	remote.guestStore = changes.guestStore
	if changes.beforeStart != nil {
		changes.beforeStart(sandbox)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	registry := NewRegistry()
	codemode, err := NewCodemodeExtension(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Install(&codemode); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var inputs []workerReplayInputsV160
	remote.observeStart = func(start CodemodeStart) {
		mu.Lock()
		inputs = append(inputs, workerReplayInputsV160{clock: start.StartedAt, seed: start.Seed})
		mu.Unlock()
	}
	echoes := map[string]int{}
	echo := wrapRegistration("echo")
	echo.ReplaySafe = false
	echo.Execute = func(_ context.Context, raw any, _ *ToolAPI) (ToolResult, error) {
		text, _ := raw.(JSON)["text"].(string)
		mu.Lock()
		echoes[text]++
		mu.Unlock()
		return ToolResult{Content: "echo " + text}, nil
	}
	var block atomic.Bool
	block.Store(true)
	held := make(chan struct{})
	var holds atomic.Int32
	hold := wrapRegistration("hold")
	hold.ReplaySafe = true
	hold.Execute = func(ctx context.Context, raw any, _ *ToolAPI) (ToolResult, error) {
		if run := holds.Add(1); block.Load() {
			if run == 1 {
				close(held)
			}
			<-ctx.Done()
			return ToolResult{}, ctx.Err()
		}
		id := raw.(JSON)["id"]
		if id == nil {
			id = ""
		}
		return ToolResult{Content: fmt.Sprint("held ", id)}, nil
	}
	for _, tool := range []ToolRegistration{echo, hold} {
		if err := registry.Register(tool); err != nil {
			t.Fatal(err)
		}
	}
	var requests atomic.Int32
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			ch <- toolAnswer("s1", "codemode", JSON{"code": source})
		} else {
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	options.CodemodeGlobals = changes.globals
	options.Now = changes.now
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, options)
	conversation := root(t, h, ref)
	sub, err := conversation.Submit(ctx, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	settled := make(chan error, 1)
	go func() { _, err := sub.Wait(ctx); settled <- err }()
	select {
	case <-held:
	case err := <-settled:
		if err != nil {
			t.Fatalf("submission stopped before hold: %v", err)
		}
		t.Fatalf("submission stopped before hold: %q", v160ToolResultText(v160ToolResult(t, ctx, conversation, "s1")))
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	state, err := h.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var beforeClose []CodemodeDelivery
	journals := 0
	for _, doc := range state.Documents {
		if doc.Kind == codemodeJournalKind {
			journals++
			var journal codemodeJournalState
			if err := decodeDocumentValue(doc, DefaultLimits(), &journal); err != nil {
				t.Fatal(err)
			}
			beforeClose = journal.Deliveries
		}
	}
	if journals != 1 {
		t.Fatalf("journals at hold = %d; want 1", journals)
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	block.Store(false)
	reopenedStore := reopenStoreAfterHarnessClose(t, store)
	t.Cleanup(func() { _ = reopenedStore.Close(context.Background()) })
	if changes.beforeResume != nil {
		changes.beforeResume(ctx, reopenedStore, sandbox)
	}
	next := openHarness(t, reopenedStore, options)
	if err := next.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := next.Submission(ctx, sub.ID())
	if err != nil || reopened == nil {
		t.Fatalf("reopen submission: %v, %v", reopened, err)
	}
	if result, err := reopened.Wait(ctx); err != nil || result.Submission.Status != "done" {
		t.Fatalf("replayed submission: %+v, %v", result, err)
	}
	conversation, err = next.Conversation(ctx, conversation.ID())
	if err != nil {
		t.Fatal(err)
	}
	result := v160ToolResult(t, ctx, conversation, "s1")
	text := v160ToolResultText(result)
	header, body, found := strings.Cut(text, "\nOutput:\n")
	if found && regexp.MustCompile(`^Script (completed|failed)\nWall time [\d.]+ seconds$`).MatchString(header) {
		text = body
	} else if !result.IsError {
		t.Fatalf("invalid tool result envelope: %q", text)
	}
	if requests.Load() != 2 {
		t.Errorf("model requests = %d; want 2", requests.Load())
	}
	page, err := next.Commit(ctx, func(tx *Tx) (any, error) {
		return tx.ScanTasks(TaskQuery{Kind: "pi.tool"}, 100, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	nested := 0
	for _, task := range page.(Page[TaskRecord]).Items {
		if task.Input != nil && generationObject(task.Input.Value)["kind"] == "nested" {
			nested++
		}
	}
	if err := next.Close(ctx); err != nil {
		t.Fatal(err)
	}
	return workerNestedReplayV160{text: text, isError: result.IsError, echoes: echoes,
		starts: remote.starts.Load(), holds: holds.Load(), replies: remote.deliveries.Load(), nested: nested, beforeClose: beforeClose, inputs: inputs}
}
