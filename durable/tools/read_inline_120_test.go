package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
)

func TestRead120DefaultInlineImagesAndProcessorLimits(t *testing.T) {
	root := t.TempDir()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 20, 10))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "image.png"), encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Read(LocalEnv(root)).Execute(context.Background(), durable.JSON{"path": "image.png"}, nil)
	if err != nil || result.IsError || len(result.Blocks) != 1 || result.Blocks[0].Type != "image" || result.Blocks[0].Data != base64.StdEncoding.EncodeToString(encoded.Bytes()) {
		t.Fatal(result, err)
	}
	_, _, err = LocalEnv(root).LoadInlineImage(context.Background(), "image.png", ImageLimits{MaxWidth: 4, MaxHeight: 4, MaxBase64: 1024})
	if err == nil {
		t.Fatal("oversize image bypassed no-processor limits")
	}
}

func TestRead120DefaultLargeImageReachesProvider(t *testing.T) {
	root := t.TempDir()
	raster := image.NewNRGBA(image.Rect(0, 0, 300, 300))
	random := rand.New(rand.NewSource(1))
	for i := range raster.Pix {
		raster.Pix[i] = byte(random.Intn(256))
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, raster); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "large.png"), encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	expected := base64.StdEncoding.EncodeToString(encoded.Bytes())
	if len(expected) <= durable.DefaultLimits().MaxStringBytes {
		t.Fatal("fixture not large")
	}
	registry := durable.NewRegistry()
	if err := registry.Register(Read(LocalEnv(root))); err != nil {
		t.Fatal(err)
	}
	api := goai.Api("default-read-image-120")
	calls := 0
	seen := false
	goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		calls++
		message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonStop, Content: []goai.ContentBlock{{Type: "text", Text: "done"}}}
		if calls == 1 {
			message.StopReason = goai.StopReasonToolUse
			message.Content = []goai.ContentBlock{{Type: "toolCall", ID: "read", Name: "read", Arguments: map[string]any{"path": "large.png"}}}
		} else {
			for _, m := range input.Messages {
				for _, block := range m.Content {
					if block.Type == "image" {
						seen = true
						if block.Data != expected {
							t.Error("read image changed")
						}
					}
				}
			}
		}
		ch := make(chan goai.Event, 1)
		ch <- &goai.DoneEvent{Reason: message.StopReason, Message: message}
		close(ch)
		return ch
	}})
	defer goai.UnregisterApi(api)
	store, err := durable.NewMemory()
	if err != nil {
		t.Fatal(err)
	}
	h, err := durable.Open(context.Background(), store, durable.Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model {
		return &goai.Model{Provider: "test", ID: "model", Api: api, Input: []string{"text", "image"}, ContextWindow: 10000, MaxTokens: 100}
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close(context.Background())
	conv, err := h.Root(context.Background(), durable.AgentChange{Model: durable.ModelRef{Provider: "test", ID: "model"}})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := conv.Submit(context.Background(), durable.Input{Content: "read"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := sub.Wait(ctx)
	if err != nil || result.Submission.Status != "done" || !seen || calls != 2 {
		t.Fatal(result, err, seen, calls)
	}
}
