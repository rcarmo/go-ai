package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
)

// Exact encode(8, 6, "png") from the pinned fixture used by tools-images.test.ts:159.
const modelLimitPNG = "iVBORw0KGgoAAAANSUhEUgAAAAgAAAAGCAYAAAD+Bd/7AAAAUElEQVR4Ae3AA6AkWZbG8f937o3IzKdyS2Oubdu2bdu2bdu2bWmMnpZKr54yMyLu+Xa3anqmhztr1a9ibPGCUC1eGCovHJUXjsoLR+WF4x8B9TAEF4Sg4YsAAAAASUVORK5CYII="

func TestSourceV160ModelImageLimits(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString(modelLimitPNG)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		prepare bool
	}{
		{"without-preparation", false}, // tools-images.test.ts:161
		{"with-preparation", true},     // tools-images.test.ts:163
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := LocalEnv(t.TempDir())
			if err := env.WriteFile(t.Context(), "a.png", png); err != nil {
				t.Fatal(err)
			}
			read := Read(env)
			if tc.prepare {
				read = ReadWithImages(env)
			}
			// The same registration and bytes fit when no model overrides the defaults.
			unlimited, err := read.Execute(t.Context(), durable.JSON{"path": "a.png"}, nil)
			want := []goai.ContentBlock{{Type: "image", Data: modelLimitPNG, MimeType: "image/png"}}
			if err != nil || unlimited.IsError || !reflect.DeepEqual(unlimited.Blocks, want) {
				t.Fatalf("default-size PNG did not pass through exactly: result=%+v err=%v", unlimited, err)
			}
			result := readWithModelWidth(t, read)
			if !tc.prepare {
				if !result.IsError || len(result.Blocks) != 0 || len(result.Diagnostics) != 1 ||
					!strings.Contains(result.Diagnostics[0].Message, "of 8x6, larger than 4x2000,") {
					t.Errorf("model width did not refuse 8x6 against 4x2000: result=%+v", result)
				}
				return
			}
			if result.IsError || len(result.Blocks) != 1 || result.Blocks[0].Type != "image" {
				t.Fatalf("model-limited preparation did not return exactly one image: result=%+v", result)
			}
			data, err := base64.StdEncoding.DecodeString(result.Blocks[0].Data)
			if err != nil {
				t.Fatal(err)
			}
			decoded, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if got := decoded.Bounds().Size(); got != image.Pt(4, 3) {
				t.Errorf("prepared dimensions = %v, want (4,3)", got)
			}
		})
	}
}

// The provider emits one local tool call and then completes. The harness, not
// the fixture, constructs the invocation API and resolves the active model.
func readWithModelWidth(t *testing.T, read durable.ToolRegistration) durable.ToolResult {
	t.Helper()
	type observation struct {
		result durable.ToolResult
		err    error
	}
	results := make(chan observation, 2)
	execute := read.Execute
	read.Execute = func(ctx context.Context, args any, api *durable.ToolAPI) (durable.ToolResult, error) {
		out, err := execute(ctx, args, api)
		results <- observation{out, err}
		return out, err
	}
	registry := durable.NewRegistry()
	if err := registry.Register(read); err != nil {
		t.Fatal(err)
	}
	const api goai.Api = "source-v160-read-model-width"
	var calls atomic.Int32
	goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(_ context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		out := make(chan goai.Event, 1)
		message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonStop, Content: []goai.ContentBlock{{Type: "text", Text: "done"}}}
		if calls.Add(1) == 1 {
			message.StopReason = goai.StopReasonToolUse
			message.Content = []goai.ContentBlock{{Type: "toolCall", ID: "read-model-width", Name: "read", Arguments: durable.JSON{"path": "a.png"}}}
		}
		out <- &goai.DoneEvent{Reason: message.StopReason, Message: message}
		close(out)
		return out
	}})
	t.Cleanup(func() { goai.UnregisterApi(api) })
	store, err := newSQLiteStore(t)
	if err != nil {
		t.Fatal(err)
	}
	model := &goai.Model{Provider: "test", ID: "test", Api: api, Input: []string{"text", "image"}, ContextWindow: 10000, MaxTokens: 100,
		InputLimits: &goai.ModelInputLimits{Images: &goai.ModelImageInputLimits{Resize: &goai.ModelImageResizeOptions{MaxWidth: 4}}}}
	harness, err := durable.Open(t.Context(), store, durable.Options{Registry: registry, Models: func(provider goai.Provider, id string) *goai.Model {
		if provider == model.Provider && id == model.ID {
			return model
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := harness.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	conv, err := harness.Root(t.Context(), durable.AgentChange{Model: durable.ModelRef{Provider: model.Provider, ID: model.ID}})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := conv.Submit(t.Context(), durable.Input{Content: "Read the image"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	waited, err := sub.Wait(ctx)
	if err != nil || waited.Submission.Status != "done" || calls.Load() != 2 || len(results) != 1 {
		t.Fatalf("active read invocation did not settle once: calls=%d read results=%d status=%q err=%v", calls.Load(), len(results), waited.Submission.Status, err)
	}
	observed := <-results
	if observed.err != nil {
		t.Fatal(observed.err)
	}
	return observed.result
}
