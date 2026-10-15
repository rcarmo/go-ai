package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodemode120ExtensionGlobalRevocationReopen(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		sandbox := &revokeSandbox120{sandbox: engine, ready: make(chan struct{})}
		var effects, requests atomic.Int64
		registry := NewRegistry()
		globals := []CodemodeGlobal{{Name: "host.lookup", Version: 1, Signature: "lookup(args: string): Promise<string>", Execute: func(context.Context, any) (any, error) { effects.Add(1); return "confirmed", nil }}}
		extension, err := NewCodemodeExtension(sandbox, CodemodeExtensionOptions{Globals: &globals})
		if err != nil {
			t.Fatal(err)
		}
		if err = registry.Install(&extension); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if requests.Add(1) == 1 {
				message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "revoke-call", Name: "codemode", Arguments: map[string]any{"code": `const first=await host.lookup("first");if(first!=="confirmed")throw Error("lost delivery");let revoked=false;try{await host.lookup("second")}catch(e){revoked=true}if(!revoked)throw Error("fresh global admitted");text("revoked");`}}}}
				ch <- &goai.DoneEvent{Reason: goai.StopReasonToolUse, Message: message}
			} else {
				if strings.Contains(input.SystemPrompt, "lookup(args:") {
					t.Error("replacement prompt retains global declaration")
				}
				var last goai.Message
				for _, message := range input.Messages {
					if message.Role == goai.RoleToolResult {
						last = message
					}
				}
				if last.IsError || !strings.HasSuffix(goai.ContentText(last.Content), "Output:\nrevoked") {
					t.Error("revocation transcript", last)
				}
				ch <- terminal("done")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conv := root(t, h, ref)
		sub, err := conv.Submit(bg, Input{Content: "revoke"})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-sandbox.ready:
		case <-time.After(3 * time.Second):
			state, _ := h.Snapshot(bg)
			t.Fatalf("pause not reached requests=%d effects=%d tasks=%+v", requests.Load(), effects.Load(), state.Tasks)
		}
		if effects.Load() != 1 {
			t.Fatal(effects.Load())
		}
		if err = h.Close(bg); err != nil {
			t.Fatal(err)
		}
		globals = []CodemodeGlobal{}
		replacement, err := NewCodemodeExtension(sandbox, CodemodeExtensionOptions{Globals: &globals})
		if err != nil {
			t.Fatal(err)
		}
		if err = registry.Install(&replacement); err != nil {
			t.Fatal(err)
		}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		if err = next.Resume(bg); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 10*time.Second)
		defer cancel()
		reopened, err := next.Submission(ctx, sub.ID())
		if err != nil || reopened == nil {
			t.Fatal(err)
		}
		result, err := reopened.Wait(ctx)
		if err != nil || result.Submission.Status != "done" || effects.Load() != 1 || requests.Load() != 2 || sandbox.starts.Load() != 2 {
			t.Fatal(result, err, effects.Load(), requests.Load(), sandbox.starts.Load())
		}
	})
}
