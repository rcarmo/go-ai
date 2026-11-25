package tools

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
	_ "github.com/rcarmo/go-ai/inference/provider/openai"
)

// The exact encode(4, 4, "png") used at tools-images.test.ts:148 and :154.
const modelDetailsPNG = "iVBORw0KGgoAAAANSUhEUgAAAAQAAAAECAYAAACp8Z5+AAAASUlEQVR4Ae3AA6AkWZbG8f937o3IzKdyS2Oubdu2bdu2bdu2bWmMnpZKr54yMyLu+Xa3anqmhztr1a9ibHE/qsUDUXlOVJ4T/wj1aAQPQGq1fgAAAABJRU5ErkJggg=="
const modelDetailsDiagnostic = "<harness>\n[info] Read image file [image/png].\n</harness>"

func TestSourceV160ModelImageDetails(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString(modelDetailsPNG)
	if err != nil {
		t.Fatal(err)
	}
	wantImage := []goai.ContentBlock{{Type: "image", Data: modelDetailsPNG, MimeType: "image/png"}}
	wantReceipt := []goai.ContentBlock{wantImage[0], {Type: "text", Text: modelDetailsDiagnostic}}
	for _, tc := range []struct {
		name    string
		input   []string
		details durable.JSON
	}{
		{"text-only", []string{"text"}, durable.JSON{"imageUnsupportedByModel": "test"}}, // L151–153
		{"vision", []string{"text", "image"}, nil},                                       // L155
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := LocalEnv(t.TempDir())
			if err := env.WriteFile(t.Context(), "a.png", png); err != nil {
				t.Fatal(err)
			}
			result, conv := readWithImageModel(t, Read(env), tc.input, nil)
			if result.IsError || !reflect.DeepEqual(result.Blocks, wantImage) {
				t.Fatalf("read output did not preserve the exact image: %+v", result.Blocks)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Message != "Read image file [image/png]." {
				t.Errorf("diagnostics = %+v, want only the pinned read message", result.Diagnostics)
			}
			if !reflect.DeepEqual(result.Details, tc.details) || result.HasDetails {
				t.Errorf("read details = %#v (+ tagged %v), want %#v", result.Details, result.HasDetails, tc.details)
			}
			view, err := conv.ContextView(t.Context(), 0)
			if err != nil {
				t.Fatal(err)
			}
			var receipts []durable.MessageReceipt
			for _, msg := range view.Messages {
				if msg.Role == goai.RoleToolResult {
					receipts = append(receipts, msg)
				}
			}
			if len(receipts) != 1 || !reflect.DeepEqual(receipts[0].Content, wantReceipt) ||
				!reflect.DeepEqual(receipts[0].Details, tc.details) || receipts[0].HasDetails {
				t.Errorf("settled read receipt did not retain exact bytes and UI details: %+v", receipts)
			}
			if tc.details != nil {
				replay, err := conv.Context(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				checkTextOnlyReadPayload(t, replay)
				vision := &goai.Model{Input: []string{"text", "image"}}
				var replayed []goai.ContentBlock
				for _, msg := range goai.TransformMessages(replay.Messages, vision) {
					if msg.Role == goai.RoleToolResult {
						replayed = append(replayed, msg.Content...)
					}
				}
				if !reflect.DeepEqual(replayed, wantReceipt) {
					t.Errorf("vision replay changed the image or its read diagnostic: %+v", replayed)
				}
			}
		})
	}
}

func checkTextOnlyReadPayload(t *testing.T, replay *goai.Context) {
	t.Helper()
	model := &goai.Model{Provider: goai.ProviderOpenAI, ID: "gpt-4o-mini", Api: goai.ApiOpenAICompletions,
		Input: []string{"text"}, BaseURL: "http://127.0.0.1:1"}
	stop := errors.New("stop after observing the payload, before HTTP")
	var payload []byte
	captured := false
	for event := range goai.Stream(t.Context(), model, replay, &goai.StreamOptions{
		APIKey: "test-key", HTTPTransport: rejectReadPayloadTransport{},
		OnPayload: func(value any, _ *goai.Model) (any, error) {
			var err error
			payload, err = json.Marshal(value)
			if err != nil {
				return nil, err
			}
			return nil, stop
		},
	}) {
		if e, ok := event.(*goai.ErrorEvent); ok && errors.Is(e.Err, stop) {
			captured = true
		}
	}
	if !captured {
		t.Fatal("did not stop the real provider serializer at OnPayload")
	}
	if bytes.Contains(payload, []byte("imageUnsupportedByModel")) || bytes.Contains(payload, []byte(modelDetailsPNG)) {
		t.Errorf("provider payload includes UI details or PNG bytes: %s", payload)
	}
	var request struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatal(err)
	}
	var tools []string
	for _, msg := range request.Messages {
		if msg.Role == "tool" {
			var text string
			if err := json.Unmarshal(msg.Content, &text); err != nil {
				t.Fatal(err)
			}
			tools = append(tools, text)
		}
	}
	want := "(tool image omitted: model does not support images)\n" + modelDetailsDiagnostic
	if !reflect.DeepEqual(tools, []string{want}) {
		t.Errorf("provider tool content = %q; want the placeholder and normal read diagnostic", tools)
	}
}

type rejectReadPayloadTransport struct{}

func (rejectReadPayloadTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unexpected HTTP request in image details test")
}
