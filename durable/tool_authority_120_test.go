package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestTool120CallerMetadataSnapshotAndWrapperIsolation(t *testing.T) {
	registry := NewRegistry()
	callers := []string{"tools"}
	reg := wrapRegistration("internal")
	reg.Deferred = true
	reg.Callers = callers
	if err := registry.Register(reg); err != nil {
		t.Fatal(err)
	}
	callers[0] = "model"
	stored, _ := registry.current("internal")
	if toolCallerAllowed(stored.offer, "model") || !toolCallerAllowed(stored.offer, "tools") {
		t.Fatal("registration caller alias")
	}
	snapshot, err := registry.taskSnapshot(DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.offers) != 1 || snapshot.offers[0].Callers == nil {
		t.Fatal("metadata missing")
	}
	(*snapshot.offers[0].Callers)[0] = "model"
	stored, _ = registry.current("internal")
	if toolCallerAllowed(stored.offer, "model") {
		t.Fatal("snapshot caller alias")
	}
	wrapped, err := applyToolWrap(ExtensionWrap{Tool: "internal", WrapTool: func(reg ToolRegistration) (ToolRegistration, error) {
		if !reg.Deferred || len(reg.Callers) != 1 || reg.Callers[0] != "tools" {
			t.Error("wrapper lost metadata")
		}
		reg.Callers[0] = "model"
		return reg, nil
	}}, stored, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !toolCallerAllowed(wrapped.offer, "model") || toolCallerAllowed(stored.offer, "model") {
		t.Fatal("wrapper caller ownership")
	}
	if sameImplementation(stored.offer, wrapped.offer) {
		t.Fatal("caller policy ignored in offer comparison")
	}
}

func TestTool120ModelCannotInvokeInternalOrDeferredTools(t *testing.T) {
	for _, mode := range []string{"internal", "deferred", "neither"} {
		t.Run(mode, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				runs := 0
				r := NewRegistry()
				reg := wrapRegistration("private")
				if mode == "deferred" {
					reg.Deferred = true
				} else if mode == "internal" {
					reg.Callers = []string{"tools"}
				} else {
					reg.Callers = []string{}
				}
				reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
					runs++
					return ToolResult{Content: "forbidden"}, nil
				}
				if err := r.Register(reg); err != nil {
					t.Fatal(err)
				}
				calls := 0
				ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
					ch := make(chan goai.Event, 1)
					calls++
					if calls == 1 {
						ch <- toolAnswer("forged-call", "private", JSON{})
					} else {
						ch <- terminal("done")
					}
					close(ch)
					return ch
				})
				options.Registry = r
				h := openHarness(t, b.store, options)
				conv := root(t, h, ref)
				sub, err := conv.Submit(bg, Input{Content: "go"})
				if err != nil {
					t.Fatal(err)
				}
				waitSubmission(t, sub)
				if runs != 0 {
					t.Fatal("model bypassed caller/offered authority", runs)
				}
			})
		})
	}
}
