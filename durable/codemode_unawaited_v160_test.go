package durable

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/quickjs"
)

const v160UnawaitedSlowScript = `tools.slow(); return 'early'`

// sandbox.test.ts:376–394. The raw native engine dispatches the callback
// immediately. It must observe cancellation before the successful return.
func TestSourceV160CodemodeInlineEngineCancelsUnawaitedCall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	engine, err := quickjs.Default()
	if err != nil {
		t.Fatal(err)
	}
	var called atomic.Int32
	var aborted atomic.Bool
	r := engine.Execute(ctx, v160UnawaitedSlowScript, quickjs.Options{Tools: []quickjs.Tool{{
		Name: "slow", Execute: func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
			called.Add(1)
			<-ctx.Done()
			aborted.Store(errors.Is(ctx.Err(), context.Canceled))
			return nil, errors.New("aborted")
		},
	}}})
	if !r.OK || r.Error != nil || string(r.Value) != `"early"` { // L391
		t.Errorf("result=%+v; want successful early return", r)
	}
	if len(r.Calls) != 1 || r.Calls[0].Name != "slow" || r.Calls[0].Status != "cancelled" { // L392
		t.Errorf("calls=%+v; want slow/cancelled", r.Calls)
	}
	if called.Load() != 1 || !aborted.Load() { // L393
		t.Errorf("callbacks=%d aborted=%t; want one callback cancelled before return", called.Load(), aborted.Load())
	}
}

// The published host owns AbortSignal; the worker wire by itself does not
// carry this callback flag. Check the actual portable and Node Worker hosts.
func TestSourceV160CodemodePublishedHostsCancelUnawaitedCall(t *testing.T) {
	root := officialWorkerPackageRootV160(t)
	for _, placement := range []string{"portable", "node-worker"} {
		t.Run(placement, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "node", "testdata/worker-unawaited-v160.mjs", placement, v160UnawaitedSlowScript)
			cmd.Env = append(os.Environ(), "GOAI_WORKER_PACKAGE_ROOT="+root)
			raw, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("published host failed: %v\n%s", err, raw)
			}
			var got struct {
				OK, Aborted bool
				Value       string
				Called      int
				Calls       []struct{ Name, Status string }
			}
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("published host returned invalid JSON: %v\n%s", err, raw)
			}
			if !got.OK || got.Value != "early" || len(got.Calls) != 1 || got.Calls[0].Name != "slow" || got.Calls[0].Status != "cancelled" || !got.Aborted || got.Called != 1 {
				t.Errorf("published result=%+v; want early, slow/cancelled, and one aborted callback", got)
			}
		})
	}
}

// Supplemental public Durable projection of the same script. The portable
// guest ends at its first drain: publish the cancelled call, but never give
// an already finished guest an application tool effect or delivery.
func TestV160CodemodePortableUnawaitedCallDoesNotAdmitEffect(t *testing.T) {
	sandbox, remote := newJournalWorkerSandboxV160(t)
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
	var effects atomic.Int32
	slow := wrapRegistration("slow")
	slow.Execute = func(context.Context, any, *ToolAPI) (ToolResult, error) {
		effects.Add(1)
		return ToolResult{Content: "unexpected effect"}, nil
	}
	if err := registry.Register(slow); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			ch <- toolAnswer("early", "codemode", JSON{"code": v160UnawaitedSlowScript})
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
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	if outcome, err := sub.Wait(ctx); err != nil || outcome.Submission.Status != "done" {
		t.Fatalf("submission status=%q err=%v; want done", outcome.Submission.Status, err)
	}
	result := v160ToolResult(t, ctx, conversation, "early")
	if result.IsError || len(result.Content) != 2 || result.Content[1].Text != "early" {
		t.Errorf("script result=%q error=%t; want early", v160ToolResultText(result), result.IsError)
	}
	calls, _ := result.Details["calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("calls=%v; want one cancelled slow", calls)
	}
	if call := generationObject(calls[0]); call["name"] != "slow" || call["status"] != "cancelled" {
		t.Errorf("call=%v; want slow/cancelled", call)
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if effects.Load() != 0 || remote.deliveries.Load() != 0 || remote.starts.Load() != 1 {
		t.Errorf("effects=%d deliveries=%d starts=%d; want 0, 0, 1", effects.Load(), remote.deliveries.Load(), remote.starts.Load())
	}
}
