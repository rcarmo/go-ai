package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
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
			store, e := newSQLiteStore(t)
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

// Source: images-fit.test.ts. Native pixels are checked by decoding the result;
// unchanged images must keep exactly the input payload.
func TestSourceV160FitImages(t *testing.T) {
	ctx := context.Background()
	limits := ImageLimits{MaxWidth: 2000, MaxHeight: 2000, MaxBase64: 9 << 19}
	small := fitPNG(t, 8, 6)
	if got := FitImage(ctx, small, "image/png", limits, NativeImageProcessor); got.Image == nil || !reflect.DeepEqual(*got.Image, fitBlock(small)) || len(got.Notes) != 0 || got.OmittedNote != "" {
		t.Fatalf("small image = %+v", got)
	}
	failing := ImageProcessor(func(context.Context, []byte, string, ImageLimits) (*PreparedImage, error) {
		return nil, fmt.Errorf("worker exited")
	})
	if got := FitImage(ctx, small, "image/png", limits, failing); got.Image != nil || got.OmittedNote != "An image (image/png) cannot be prepared for the model and was left out: worker exited." {
		t.Fatalf("processor error = %+v", got)
	}
	var called []string
	proc := ImageProcessor(func(ctx context.Context, data []byte, mime string, l ImageLimits) (*PreparedImage, error) {
		called = append(called, mime)
		return NativeImageProcessor(ctx, data, mime, l)
	})
	before, after := goai.ContentBlock{Type: "text", Text: "before"}, goai.ContentBlock{Type: "text", Text: "after"}
	if got := FitImages(ctx, []goai.ContentBlock{before, fitBlock(small)}, limits, proc); got.Changed || got.Content != nil || got.Notes != nil {
		t.Fatalf("unchanged = %+v", got)
	}
	if got := FitImages(ctx, []goai.ContentBlock{before}, limits, proc); got.Changed || !reflect.DeepEqual(called, []string{"image/png"}) {
		t.Fatalf("processor saw %v, got %+v", called, got)
	}
	limits.MaxWidth = 200
	got := FitImages(ctx, []goai.ContentBlock{before, fitBlock(fitPNG(t, 400, 100)), after}, limits, proc)
	if !got.Changed || len(got.Content) != 3 || !reflect.DeepEqual(got.Content[0], before) || !reflect.DeepEqual(got.Content[2], after) || !reflect.DeepEqual(got.Notes, []string{"Resized from 400x100 to 200x50. Multiply coordinates by 2.00 to map them to the original."}) {
		t.Fatalf("resized = %+v", got)
	}
	fitDims(t, got.Content[1], 200, 50)
	got = FitImages(ctx, []goai.ContentBlock{before, fitBlock(small[:40]), fitBlock([]byte("not an image")), {Type: "image", Data: "%%%", MimeType: "image/png"}}, limits, proc)
	const omitted = "An image (image/png) cannot be prepared for the model and was left out"
	if !got.Changed || !reflect.DeepEqual(got.Content, []goai.ContentBlock{before}) || len(got.Notes) != 3 || got.Notes[0] != omitted+"." || got.Notes[1] != omitted+"." || !strings.HasPrefix(got.Notes[2], omitted+": ") {
		t.Fatalf("omissions = %+v", got)
	}
}

func TestSourceV160FitImagesFormats(t *testing.T) {
	ctx := context.Background()
	limits := fitModelLimits(nil)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 40, 30)), nil); err != nil {
		t.Fatal(err)
	}
	raw := encoded.Bytes()
	got := FitImages(ctx, []goai.ContentBlock{fitBlock(raw)}, limits, NativeImageProcessor)
	want := fitBlock(raw)
	want.MimeType = "image/jpeg"
	if !got.Changed || !reflect.DeepEqual(got.Content, []goai.ContentBlock{want}) || len(got.Notes) != 0 {
		t.Fatalf("corrected type = %+v", got)
	}
	exif := make([]byte, 32)
	copy(exif, []byte("Exif\x00\x00II"))
	binary.LittleEndian.PutUint16(exif[8:], 42)
	binary.LittleEndian.PutUint32(exif[10:], 8)
	binary.LittleEndian.PutUint16(exif[14:], 1)
	binary.LittleEndian.PutUint16(exif[16:], 0x112)
	binary.LittleEndian.PutUint16(exif[18:], 3)
	binary.LittleEndian.PutUint32(exif[20:], 1)
	binary.LittleEndian.PutUint16(exif[24:], 6)
	oriented := append(append([]byte{}, raw[:2]...), 0xff, 0xe1, 0, byte(len(exif)+2))
	oriented = append(append(oriented, exif...), raw[2:]...)
	got = FitImages(ctx, []goai.ContentBlock{fitBlock(oriented)}, limits, NativeImageProcessor)
	if !got.Changed || len(got.Content) != 1 {
		t.Fatalf("oriented = %+v", got)
	}
	fitDims(t, got.Content[0], 30, 40)
	bmp := make([]byte, 102)
	copy(bmp, "BM")
	binary.LittleEndian.PutUint32(bmp[2:], uint32(len(bmp)))
	binary.LittleEndian.PutUint32(bmp[10:], 54)
	binary.LittleEndian.PutUint32(bmp[14:], 40)
	binary.LittleEndian.PutUint32(bmp[18:], 4)
	binary.LittleEndian.PutUint32(bmp[22:], 4)
	binary.LittleEndian.PutUint16(bmp[26:], 1)
	binary.LittleEndian.PutUint16(bmp[28:], 24)
	block := fitBlock(bmp)
	block.MimeType = "image/bmp"
	got = FitImages(ctx, []goai.ContentBlock{block}, limits, NativeImageProcessor)
	if !got.Changed || len(got.Content) != 1 || got.Content[0].MimeType != "image/png" || !reflect.DeepEqual(got.Notes, []string{"Converted from image/bmp to image/png."}) {
		t.Fatalf("converted = %+v", got)
	}
	noisy := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	seed := uint32(835)
	for y := range 64 {
		for x := range 64 {
			seed = seed*1664525 + 1013904223
			noisy.SetNRGBA(x, y, color.NRGBA{R: byte(seed >> 24), G: byte(seed >> 16), B: byte(seed >> 8), A: 255})
		}
	}
	encoded.Reset()
	if err := png.Encode(&encoded, noisy); err != nil {
		t.Fatal(err)
	}
	limits.MaxBase64 = 8000
	got = FitImages(ctx, []goai.ContentBlock{fitBlock(encoded.Bytes())}, limits, NativeImageProcessor)
	if !got.Changed || len(got.Content) != 1 || len(got.Content[0].Data) > 8000 || len(got.Notes) == 0 {
		t.Fatalf("byte budget: changed=%v, images=%d, notes=%v", got.Changed, len(got.Content), got.Notes)
	}
}

func TestSourceV160FitImagesExtension(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	registry := durable.NewRegistry()
	var processorCalls atomic.Int32
	if err := registry.Install(CreateFitImagesExtension(func(ctx context.Context, bytes []byte, mime string, limits ImageLimits) (*PreparedImage, error) {
		processorCalls.Add(1)
		return NativeImageProcessor(ctx, bytes, mime, limits)
	})); err != nil {
		t.Fatal(err)
	}
	small := fitBlock(fitPNG(t, 8, 6))
	wide := fitBlock(fitPNG(t, 400, 100))
	if err := registry.Register(durable.ToolRegistration{
		Definition: goai.Tool{Name: "picture", Parameters: json.RawMessage(`{"type":"object","properties":{"size":{"type":"string"}}}`)}, Implementation: "test.picture", Version: 1,
		Execute: func(_ context.Context, args any, _ *durable.ToolAPI) (durable.ToolResult, error) {
			if size, ok := mapField(args, "size").(string); ok && size == "small" {
				return durable.ToolResult{Blocks: []goai.ContentBlock{small}}, nil
			}
			return durable.ToolResult{Blocks: []goai.ContentBlock{{Type: "text", Text: "here"}, wide}, Diagnostics: []durable.ToolDiagnostic{{Severity: "info", Message: "the tool's own"}}}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	nestedSeen := make(chan durable.ToolResult, 1)
	if err := registry.Register(durable.ToolRegistration{Definition: goai.Tool{Name: "delegate", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "test.delegate.picture", Version: 1,
		Execute: func(ctx context.Context, _ any, api *durable.ToolAPI) (durable.ToolResult, error) {
			result, err := api.CallTool(ctx, "nested-picture", "picture", durable.JSON{"size": "wide"})
			if err != nil {
				return durable.ToolResult{}, err
			}
			nestedSeen <- result
			return durable.ToolResult{Content: "nested"}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	api := goai.Api("fit-images-160")
	var calls atomic.Int32
	goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		m := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonStop, Content: []goai.ContentBlock{{Type: "text", Text: "done"}}}
		if calls.Add(1) == 1 {
			m.StopReason = goai.StopReasonToolUse
			m.Content = []goai.ContentBlock{{Type: "toolCall", ID: "wide", Name: "picture", Arguments: map[string]any{"size": "wide"}}, {Type: "toolCall", ID: "small", Name: "picture", Arguments: map[string]any{"size": "small"}}, {Type: "toolCall", ID: "nested", Name: "delegate", Arguments: map[string]any{}}}
		}
		out := make(chan goai.Event, 1)
		out <- &goai.DoneEvent{Reason: m.StopReason, Message: m}
		close(out)
		return out
	}})
	defer goai.UnregisterApi(api)
	store, err := newSQLiteStore(t)
	if err != nil {
		t.Fatal(err)
	}
	h, err := durable.Open(ctx, store, durable.Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model {
		return &goai.Model{Provider: "test", ID: "model", Api: api, Input: []string{"text", "image"}, ContextWindow: 10000, MaxTokens: 100, InputLimits: &goai.ModelInputLimits{Images: &goai.ModelImageInputLimits{Resize: &goai.ModelImageResizeOptions{MaxWidth: 100}}}}
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close(context.Background())
	c, err := h.Root(ctx, durable.AgentChange{Model: durable.ModelRef{Provider: "test", ID: "model"}})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := c.Submit(ctx, durable.Input{Content: "show"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := sub.Wait(ctx); err != nil || got.Record.Status != "done" || calls.Load() != 2 {
		t.Fatalf("submission=%+v, provider calls=%d, err=%v", got.Record, calls.Load(), err)
	}
	page, err := c.Entries(ctx, durable.EntryQuery{}, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	stored := map[string][]goai.ContentBlock{}
	for _, entry := range page.Items {
		for _, m := range entry.Model {
			if m.Role == goai.RoleToolResult {
				stored[m.ToolCallID] = m.Content
			}
		}
	}
	if !reflect.DeepEqual(stored["small"], []goai.ContentBlock{small}) {
		t.Fatalf("small = %+v", stored["small"])
	}
	body := stored["wide"]
	if len(body) != 3 || body[0].Type != "text" || body[0].Text != "here" || body[2].Type != "text" || body[2].Text != "<harness>\n[info] the tool's own\n[info] Resized from 400x100 to 100x25. Multiply coordinates by 4.00 to map them to the original.\n</harness>" {
		t.Fatalf("wide result has %d blocks: %+v", len(body), body)
	}
	fitDims(t, body[1], 100, 25)
	if len(nestedSeen) != 1 || processorCalls.Load() != 2 {
		t.Fatalf("nested tool ran %d times; processor called %d times", len(nestedSeen), processorCalls.Load())
	}
	nested := <-nestedSeen
	// The native nested adapter exposes unstructured output as structured JSON.
	if nested.IsError || !nested.HasStructuredOutput {
		t.Fatalf("nested result lost its output: %+v", nested)
	}
	items, ok := nested.StructuredOutput.([]any)
	if !ok || len(items) != 2 || mapField(items[1], "type") != "image" || mapField(items[1], "data") != wide.Data || mapField(items[1], "mimeType") != wide.MimeType {
		t.Fatalf("nested image was changed: %+v", nested.StructuredOutput)
	}

}

func fitPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func fitBlock(png []byte) goai.ContentBlock {
	return goai.ContentBlock{Type: "image", Data: base64.StdEncoding.EncodeToString(png), MimeType: "image/png"}
}
func fitDims(t *testing.T, block goai.ContentBlock, w, h int) {
	t.Helper()
	if block.Type != "image" {
		t.Fatalf("not an image: %v", block.Type)
	}
	data, err := base64.StdEncoding.DecodeString(block.Data)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != w || cfg.Height != h {
		t.Fatalf("image dimensions = %dx%d, want %dx%d, %v", cfg.Width, cfg.Height, w, h, err)
	}
}
func mapField(args any, key string) any {
	switch m := args.(type) {
	case map[string]any:
		return m[key]
	case durable.JSON:
		return m[key]
	}
	return nil
}
