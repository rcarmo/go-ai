package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type extensionRevokeCatalog120 struct {
	*typedCatalog120
	chat *goai.Model
}

func (c *extensionRevokeCatalog120) GetModel(provider goai.Provider, id string) *goai.Model {
	if c.chat.Provider == provider && c.chat.ID == id {
		return c.chat
	}
	return nil
}

func TestCodemode120ExtensionModelRevocationReopen(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		sandbox := &revokeSandbox120{sandbox: engine, ready: make(chan struct{})}
		var effects, requests atomic.Int64
		api := goai.ClassifierApi("extension-revoke-120")
		goai.RegisterClassifierApiProvider(&goai.ClassifierApiProvider{Api: api, Classify: func(*goai.ClassifierModel, goai.ClassifierContext, *goai.ClassifierOptions) (*goai.ClassifierResult, error) {
			effects.Add(1)
			return &goai.ClassifierResult{Answers: map[string]goai.ClassifierAnswer{"yes": {Type: "bool", Probability: 1}}, StopReason: goai.StopReasonStop}, nil
		}})
		registry := NewRegistry()
		enabled := true
		extension, err := NewCodemodeExtension(sandbox, CodemodeExtensionOptions{Models: &enabled})
		if err != nil {
			t.Fatal(err)
		}
		if err = registry.Install(&extension); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if requests.Add(1) == 1 {
				message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "revoke-call", Name: "codemode", Arguments: map[string]any{"code": `const model={provider:"test",id:"judge"};const c={state:{},questions:{yes:{type:"bool",instructions:"yes",criteria:{true:"yes",false:"no"}}}};const first=await models.classify(model,c);if(first.answers.yes.probability!==1)throw Error("lost delivery");let revoked=false;try{await models.classify(model,c)}catch(e){revoked=true}if(!revoked)throw Error("fresh paid effect admitted");text("revoked");`}}}}
				ch <- &goai.DoneEvent{Reason: goai.StopReasonToolUse, Message: message}
			} else {
				if strings.Contains(input.SystemPrompt, "declare const models") {
					t.Error("replacement prompt retains paid declarations")
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
		options.PaidModels = true
		options.Catalog = &extensionRevokeCatalog120{typedCatalog120: &typedCatalog120{ModelRuntime: goai.NewModelRuntime(nil), classifier: &goai.ClassifierModel{Provider: "test", ID: "judge", Api: api}}, chat: options.Models(ref.Provider, ref.ID)}
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
		enabled = false
		replacement, err := NewCodemodeExtension(sandbox, CodemodeExtensionOptions{Models: &enabled})
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
