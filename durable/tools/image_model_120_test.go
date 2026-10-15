package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
)

func TestRead120InvocationModelLimitsAndNonvision(t *testing.T) {
	for _, tc := range []struct {
		name          string
		vision        bool
		resize        *goai.ModelImageResizeOptions
		wantImage     bool
		width, height int
	}{
		// Raw provider requests retain images; TransformMessages supplies the
		// text-only model placeholder without altering transcript history.
		{"text", false, &goai.ModelImageResizeOptions{MaxWidth: 4, MaxHeight: 4, MaxBytes: 1024}, true, 4, 2},
		{"vision", true, &goai.ModelImageResizeOptions{MaxWidth: 4, MaxHeight: 4, MaxBytes: 1024}, true, 4, 2},
		{"omitted", true, &goai.ModelImageResizeOptions{}, true, 20, 10},
		{"zero-width", true, &goai.ModelImageResizeOptions{MaxWidthSet: true}, false, 0, 0},
		{"zero-height", true, &goai.ModelImageResizeOptions{MaxHeightSet: true}, false, 0, 0},
		{"zero-bytes", true, &goai.ModelImageResizeOptions{MaxBytesSet: true}, false, 0, 0},
		{"negative-width", true, &goai.ModelImageResizeOptions{MaxWidth: -1}, false, 0, 0},
		{"invalid-quality", true, &goai.ModelImageResizeOptions{JPEGQuality: 101}, false, 0, 0},
	} {
		name, vision := tc.name, tc.vision
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			var encoded bytes.Buffer
			png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 20, 10)))
			if e := os.WriteFile(filepath.Join(root, "image.png"), encoded.Bytes(), 0600); e != nil {
				t.Fatal(e)
			}
			registry := durable.NewRegistry()
			if e := registry.Register(ReadWithImages(LocalEnv(root))); e != nil {
				t.Fatal(e)
			}
			api := goai.Api("image-model-120")
			calls := 0
			goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonStop, Content: []goai.ContentBlock{{Type: "text", Text: "done"}}}
				calls++
				if calls == 1 {
					message.StopReason = goai.StopReasonToolUse
					message.Content = []goai.ContentBlock{{Type: "toolCall", ID: "read-call", Name: "read", Arguments: map[string]any{"path": "image.png"}}}
				} else {
					last := input.Messages[len(input.Messages)-1]
					found := false
					for _, block := range last.Content {
						if block.Type == "image" {
							found = true
							data, e := base64.StdEncoding.DecodeString(block.Data)
							if e != nil {
								t.Error(e)
							}
							config, _, e := image.DecodeConfig(bytes.NewReader(data))
							if e != nil || config.Width != tc.width || config.Height != tc.height {
								t.Error(config, e)
							}
						}
					}
					if found != tc.wantImage {
						t.Error("image retention policy", found, tc.wantImage)
					}
					if !vision {
						transformed := goai.TransformMessages(input.Messages, &goai.Model{Input: []string{"text"}})
						placeholder := false
						for _, m := range transformed {
							for _, block := range m.Content {
								if block.Type == "image" {
									t.Error("image survived nonvision transformation")
								}
								if block.Type == "text" && strings.Contains(block.Text, "image omitted") {
									placeholder = true
								}
							}
						}
						if !placeholder {
							t.Error("missing nonvision placeholder")
						}
					}
				}
				ch <- &goai.DoneEvent{Reason: message.StopReason, Message: message}
				close(ch)
				return ch
			}})
			defer goai.UnregisterApi(api)
			input := []string{"text"}
			if vision {
				input = append(input, "image")
			}
			store, e := durable.NewMemory()
			if e != nil {
				t.Fatal(e)
			}
			h, e := durable.Open(context.Background(), store, durable.Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model {
				return &goai.Model{Provider: "test", ID: "model", Api: api, Input: input, ContextWindow: 10000, MaxTokens: 100, InputLimits: &goai.ModelInputLimits{Images: &goai.ModelImageInputLimits{Resize: tc.resize}}}
			}})
			if e != nil {
				t.Fatal(e)
			}
			defer h.Close(context.Background())
			conv, e := h.Root(context.Background(), durable.AgentChange{Model: durable.ModelRef{Provider: "test", ID: "model"}})
			if e != nil {
				t.Fatal(e)
			}
			sub, e := conv.Submit(context.Background(), durable.Input{Content: "read"})
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			result, e := sub.Wait(ctx)
			if e != nil || result.Submission.Status != "done" || calls != 2 {
				t.Fatal(result, e, calls)
			}
		})
	}
}
