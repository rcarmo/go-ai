package inference

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
	"testing"
)

func TestClientIdentity140AllRequestPathsAndCatalogDelegation(t *testing.T) {
	baseVersion := "base"
	requestVersion := "request"
	base := NewClient(nil, &goai.ClientIdentity{Version: &baseVersion, UserAgent: &goai.UserAgentMode{Mode: "short"}})
	defaults := &goai.ClientIdentity{Attribution: &goai.ClientAttribution{Name: "wrapper", URL: "https://example.com"}}
	client := WithClientIdentity(base, defaults)
	defaults.Attribution.Name = "mutated"
	if client.Identity().Attribution.Name != "wrapper" || base.Identity().Attribution != nil {
		t.Fatal("wrapper aliases source")
	}
	client.SetProvider(goai.StaticModelProvider{Provider: "shared", Models: []*goai.Model{{ID: "shared", Provider: "shared"}}})
	if base.GetModel("shared", "shared") == nil || client.ModelRuntime != base.ModelRuntime {
		t.Fatal("catalog mutation not delegated")
	}
	req := &goai.ClientIdentity{Version: &requestVersion, AttributionSet: true}
	calls := 0
	check := func(identity *goai.ClientIdentity) {
		t.Helper()
		calls++
		if identity == nil || identity.Attribution != nil || identity.UserAgent.Mode != "short" || *identity.Version != "request" {
			t.Fatalf("%+v", identity)
		}
	}
	api := Api("identity-client-140")
	stream := func(_ context.Context, _ *Model, _ *Context, o *StreamOptions) <-chan Event {
		check(o.Identity)
		ch := make(chan Event, 1)
		ch <- &goai.DoneEvent{Message: &Message{StopReason: goai.StopReasonStop}}
		close(ch)
		return ch
	}
	RegisterApi(&ApiProvider{Api: api, Stream: stream, FetchDeferred: func(ctx context.Context, m *Model, _ goai.DeferredHandle, o *StreamOptions) <-chan Event {
		return stream(ctx, m, nil, o)
	}, CancelDeferred: func(_ context.Context, _ *Model, _ goai.DeferredHandle, o *StreamOptions) error {
		check(o.Identity)
		return nil
	}})
	defer UnregisterApi(api)
	model := &Model{ID: "model", Api: api}
	opts := &StreamOptions{Identity: req}
	for range client.Stream(context.Background(), model, nil, opts) {
	}
	if _, err := client.Complete(context.Background(), model, nil, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := client.FetchDeferred(context.Background(), model, goai.DeferredHandle{}, opts); err != nil {
		t.Fatal(err)
	}
	if err := client.CancelDeferred(context.Background(), model, goai.DeferredHandle{}, opts); err != nil {
		t.Fatal(err)
	}
	goai.RegisterClassifierApiProvider(&goai.ClassifierApiProvider{Api: "identity-client-140", Classify: func(_ *goai.ClassifierModel, _ goai.ClassifierContext, o *goai.ClassifierOptions) (*goai.ClassifierResult, error) {
		check(o.Identity)
		return &goai.ClassifierResult{}, nil
	}})
	if _, err := client.Classify(&goai.ClassifierModel{Api: "identity-client-140"}, goai.ClassifierContext{State: map[string]any{}, Questions: map[string]goai.ClassifierQuestion{}}, &goai.ClassifierOptions{Identity: req}); err != nil {
		t.Fatal(err)
	}
	images.RegisterImagesApiProvider(&images.ImagesApiProvider{Api: "identity-client-140", GenerateImages: func(_ *images.ImagesModel, _ images.ImagesContext, o *images.ImagesOptions) (*images.AssistantImages, error) {
		check(o.Identity)
		return &images.AssistantImages{}, nil
	}})
	defer images.UnregisterImagesApiProvider("identity-client-140")
	if _, err := client.GenerateImages(&images.ImagesModel{Api: "identity-client-140"}, images.ImagesContext{}, &images.ImagesOptions{Identity: req}); err != nil {
		t.Fatal(err)
	}
	if calls != 6 || opts.Identity != req || req.UserAgent != nil {
		t.Fatal("request propagation mutated caller or missed a path", calls)
	}
}
