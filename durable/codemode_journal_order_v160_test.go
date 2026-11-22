package durable

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

type orderReceiptQuickJSV160 struct {
	*countedQuickJSV160
	secondDelivered chan struct{}
	once            sync.Once
}

func (s *orderReceiptQuickJSV160) Start(ctx context.Context, source string, start CodemodeStart) (CodemodeExecution, error) {
	execution, err := s.countedQuickJSV160.Start(ctx, source, start)
	if err != nil {
		return nil, err
	}
	return &orderReceiptExecutionV160{CodemodeExecution: execution, sandbox: s}, nil
}

type orderReceiptExecutionV160 struct {
	CodemodeExecution
	sandbox *orderReceiptQuickJSV160
}

func (e *orderReceiptExecutionV160) AcknowledgeDrain(ctx context.Context, drain CodemodeDrain) error {
	return e.CodemodeExecution.(CodemodeDrainExecution).AcknowledgeDrain(ctx, drain)
}

func (e *orderReceiptExecutionV160) Deliver(ctx context.Context, delivery CodemodeDelivery) error {
	if err := e.CodemodeExecution.Deliver(ctx, delivery); err != nil {
		return err
	}
	if delivery.Call.ID == 2 {
		e.sandbox.once.Do(func() { close(e.sandbox.secondDelivered) })
	}
	return nil
}

// Source codemode.test.ts:231–245. Gate the first call on the second call's
// delivery receipt so completion and issuance orders differ deterministically.
func TestSourceV160CodemodeCompletionOrderReadback(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 15*time.Second)
	defer cancel()
	engine, err := NewQuickJSSandbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	sandbox := &orderReceiptQuickJSV160{countedQuickJSV160: &countedQuickJSV160{QuickJSSandbox: engine}, secondDelivered: make(chan struct{})}
	codemode, err := NewCodemodeExtension(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if err = registry.Install(&codemode); err != nil {
		t.Fatal(err)
	}
	slow := wrapRegistration("slow")
	slow.Execute = func(ctx context.Context, raw any, _ *ToolAPI) (ToolResult, error) {
		id := raw.(JSON)["id"]
		if equalJSONValue(id, 1) {
			select {
			case <-sandbox.secondDelivered:
			case <-ctx.Done():
				return ToolResult{}, ctx.Err()
			}
		} else if !equalJSONValue(id, 2) {
			return ToolResult{}, fmt.Errorf("unexpected slow id %v", id)
		}
		return ToolResult{Content: fmt.Sprintf("slow %v", id)}, nil
	}
	var mu sync.Mutex
	echoes := map[string]int{}
	echo := wrapRegistration("echo")
	echo.ReplaySafe = false
	echo.Execute = func(_ context.Context, raw any, _ *ToolAPI) (ToolResult, error) {
		text := raw.(JSON)["text"].(string)
		mu.Lock()
		echoes[text]++
		mu.Unlock()
		return ToolResult{Content: "echo " + text}, nil
	}
	var block atomic.Bool
	block.Store(true)
	held := make(chan struct{})
	hold := wrapRegistration("hold")
	hold.Execute = func(ctx context.Context, _ any, _ *ToolAPI) (ToolResult, error) {
		if block.Load() {
			close(held)
			<-ctx.Done()
			return ToolResult{}, ctx.Err()
		}
		return ToolResult{Content: "held"}, nil
	}
	for _, tool := range []ToolRegistration{slow, echo, hold} {
		if err := registry.Register(tool); err != nil {
			t.Fatal(err)
		}
	}
	const script = `const order = [];
await Promise.all([tools.slow({ id: 1 }).then(() => order.push(1)), tools.slow({ id: 2 }).then(() => order.push(2))]);
await tools.echo({ text: order.join(",") });
await tools.hold({});
return order;`
	var requests atomic.Int32
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			ch <- toolAnswer("s1", "codemode", JSON{"code": script})
		} else {
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
	sub, err := conversation.Submit(ctx, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-held:
	case <-ctx.Done():
		t.Fatalf("hold not reached: %v", ctx.Err())
	}
	// Supplemental: confirm the exact publication order in the durable journal.
	state, err := h.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int
	for _, doc := range state.Documents {
		if doc.Kind != codemodeJournalKind {
			continue
		}
		var journal codemodeJournalState
		if err := decodeDocumentValue(doc, DefaultLimits(), &journal); err != nil {
			t.Fatal(err)
		}
		for _, delivery := range journal.Deliveries {
			ids = append(ids, delivery.Call.ID)
		}
	}
	if !slices.Equal(ids, []int{2, 1, 3}) {
		t.Fatalf("journal delivery order = %v; want [2 1 3]", ids)
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	block.Store(false)
	next := openHarness(t, reopenStoreAfterHarnessClose(t, store), options)
	if err := next.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := next.Submission(ctx, sub.ID())
	if err != nil || reopened == nil {
		t.Fatalf("reopen submission: %v, %v", reopened, err)
	}
	if result, err := reopened.Wait(ctx); err != nil || result.Submission.Status != "done" {
		t.Fatalf("reopened submission: %+v, %v", result, err)
	}
	conversation, err = next.Conversation(ctx, conversation.ID())
	if err != nil {
		t.Fatal(err)
	}
	result := v160ToolResult(t, ctx, conversation, "s1")
	header, output, found := strings.Cut(v160ToolResultText(result), "\nOutput:\n")
	if result.IsError || !found || !regexp.MustCompile(`^Script completed\nWall time [\d.]+ seconds$`).MatchString(header) || output != "[2,1]" {
		t.Fatalf("published replay result = %q, isError=%t; want [2,1]", v160ToolResultText(result), result.IsError)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(echoes, map[string]int{"2,1": 1}) {
		t.Fatalf("echo effects = %v; want map[2,1:1]", echoes)
	}
	if starts := sandbox.starts.Load(); starts != 2 {
		t.Fatalf("QuickJS starts = %d; want 2", starts)
	}
}
