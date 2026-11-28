package bedrock

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	goai "github.com/rcarmo/go-ai"
)

func bedrockV170Model(endpoint string) *goai.Model {
	return &goai.Model{
		ID: "us.anthropic.claude-opus-4-8", Provider: goai.ProviderAmazonBedrock,
		Api: goai.ApiBedrockConverseStream, BaseURL: endpoint, Reasoning: true,
		Input: []string{"text"},
	}
}

func bedrockV170Options(model *goai.Model) *goai.StreamOptions {
	zero := 0
	return &goai.StreamOptions{
		Region: "us-east-1", MaxRetries: &zero, CacheRetention: goai.CacheRetentionNone,
		Env: goai.ProviderEnv{"AWS_BEDROCK_SKIP_AUTH": "1", "AWS_BEDROCK_FORCE_HTTP1": "1", "AWS_EC2_METADATA_DISABLED": "true"},
	}
}

func writeBedrockV170Event(t *testing.T, w http.ResponseWriter, name string, payload any) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var headers eventstream.Headers
	headers.Set(":message-type", eventstream.StringValue("event"))
	headers.Set(":event-type", eventstream.StringValue(name))
	if err := eventstream.NewEncoder().Encode(w, eventstream.Message{Headers: headers, Payload: body}); err != nil {
		t.Fatal(err)
	}
}

func bedrockV170Result(t *testing.T, ctx context.Context, model *goai.Model, conv *goai.Context, opts *goai.StreamOptions) (*goai.Message, error) {
	t.Helper()
	var terminal *goai.Message
	var terminalErr error
	terminalCount := 0
	for event := range goai.Stream(ctx, model, conv, opts) {
		switch event := event.(type) {
		case *goai.DoneEvent:
			terminalCount++
			terminal = event.Message
		case *goai.ErrorEvent:
			terminalCount++
			terminal, terminalErr = event.Error, event.Err
		}
	}
	if terminalCount != 1 {
		t.Fatalf("terminal events = %d, want 1", terminalCount)
	}
	return terminal, terminalErr
}

func TestBedrockV170PublicStreamHeadersAndRedaction(t *testing.T) {
	const modelID = "us.anthropic.claude-opus-4-8"
	opaque := []byte("encrypted reasoning")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		w.Header().Set("X-Amzn-Requestid", "req-123")
		w.Header().Set("X-Bifrost-Provider", "bedrock")
		w.Header().Set("X-Bifrost-Resolved-Model", modelID)
		writeBedrockV170Event(t, w, "messageStart", map[string]any{"role": "assistant"})
		writeBedrockV170Event(t, w, "contentBlockDelta", map[string]any{
			"contentBlockIndex": 0,
			"delta":             map[string]any{"reasoningContent": map[string]any{"redactedContent": opaque[:7]}},
		})
		writeBedrockV170Event(t, w, "contentBlockDelta", map[string]any{
			"contentBlockIndex": 0,
			"delta":             map[string]any{"reasoningContent": map[string]any{"redactedContent": opaque[7:]}},
		})
		writeBedrockV170Event(t, w, "contentBlockStop", map[string]any{"contentBlockIndex": 0})
		writeBedrockV170Event(t, w, "contentBlockDelta", map[string]any{
			"contentBlockIndex": 1, "delta": map[string]any{"text": "done"},
		})
		writeBedrockV170Event(t, w, "contentBlockStop", map[string]any{"contentBlockIndex": 1})
		writeBedrockV170Event(t, w, "messageStop", map[string]any{"stopReason": "end_turn"})
	}))
	defer server.Close()

	model := bedrockV170Model(server.URL)
	opts := bedrockV170Options(model)
	var rawTypes []string
	var responses int
	opts.OnProviderStreamEvent = func(event any, got *goai.Model) error {
		if got != model {
			t.Errorf("observer model = %p, want %p", got, model)
		}
		rawTypes = append(rawTypes, fmt.Sprintf("%T", event))
		return nil
	}
	opts.OnResponse = func(status int, headers map[string]string, got *goai.Model) {
		responses++
		if status != http.StatusOK || got != model {
			t.Errorf("response status/model = %d/%p", status, got)
		}
		for name, want := range map[string]string{
			"X-Amzn-Requestid": "req-123", "X-Bifrost-Provider": "bedrock", "X-Bifrost-Resolved-Model": modelID,
		} {
			if got := headers[name]; got != want {
				t.Errorf("header %s = %q, want %q", name, got, want)
			}
		}
	}

	message, err := bedrockV170Result(t, t.Context(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantRawTypes := []string{
		"*types.ConverseStreamOutputMemberMessageStart",
		"*types.ConverseStreamOutputMemberContentBlockDelta",
		"*types.ConverseStreamOutputMemberContentBlockDelta",
		"*types.ConverseStreamOutputMemberContentBlockStop",
		"*types.ConverseStreamOutputMemberContentBlockDelta",
		"*types.ConverseStreamOutputMemberContentBlockStop",
		"*types.ConverseStreamOutputMemberMessageStop",
	}
	if responses != 1 || !reflect.DeepEqual(rawTypes, wantRawTypes) {
		t.Fatalf("response/raw sequence = %d/%v; want 1/%v", responses, rawTypes, wantRawTypes)
	}
	if len(message.Content) != 2 || message.Content[0].Type != "thinking" || message.Content[1].Type != "text" {
		t.Fatalf("content = %#v", message.Content)
	}
	thinking := message.Content[0]
	if !thinking.Redacted || thinking.Thinking != "[Reasoning redacted]" || thinking.ThinkingSignature != base64.StdEncoding.EncodeToString(opaque) {
		t.Fatalf("redacted thinking = %#v", thinking)
	}
	serialized, err := json.Marshal(thinking)
	if err != nil || strings.Contains(string(serialized), "redactedChunks") || strings.Contains(string(serialized), "index") {
		t.Fatalf("persisted thinking = %s, %v", serialized, err)
	}
	if message.StopReason != goai.StopReasonStop || message.RawStopReason != "end_turn" || message.ErrorMessage != "" {
		t.Fatalf("stop = %s raw=%q error=%q", message.StopReason, message.RawStopReason, message.ErrorMessage)
	}
}

func TestBedrockV170PublicRedactedEOF(t *testing.T) {
	opaque := []byte("encrypted without stop")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		writeBedrockV170Event(t, w, "messageStart", map[string]any{"role": "assistant"})
		writeBedrockV170Event(t, w, "contentBlockDelta", map[string]any{
			"contentBlockIndex": 0,
			"delta":             map[string]any{"reasoningContent": map[string]any{"redactedContent": opaque}},
		})
		writeBedrockV170Event(t, w, "messageStop", map[string]any{"stopReason": "end_turn"})
	}))
	defer server.Close()
	model := bedrockV170Model(server.URL)
	message, err := bedrockV170Result(t, t.Context(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, bedrockV170Options(model))
	if err != nil || len(message.Content) != 1 || message.Content[0].ThinkingSignature != base64.StdEncoding.EncodeToString(opaque) {
		t.Fatalf("EOF redacted result = %#v, %v", message, err)
	}
}

func TestBedrockV170PublicEmptyStreamHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		w.Header().Set("X-Amzn-Requestid", "req-123")
		w.Header().Set("X-Bifrost-Provider", "bedrock")
		w.Header().Set("X-Bifrost-Resolved-Model", "us.anthropic.claude-opus-4-8")
	}))
	defer server.Close()
	model := bedrockV170Model(server.URL)
	opts := bedrockV170Options(model)
	responses := 0
	opts.OnResponse = func(status int, headers map[string]string, _ *goai.Model) {
		responses++
		if status != http.StatusOK || headers["X-Amzn-Requestid"] != "req-123" || headers["X-Bifrost-Provider"] != "bedrock" {
			t.Errorf("response hook = %d %#v", status, headers)
		}
	}
	message, err := bedrockV170Result(t, t.Context(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, opts)
	if err == nil || message.StopReason != goai.StopReasonError || responses != 1 {
		t.Fatalf("empty stream = %#v, %v; responses=%d", message, err, responses)
	}
}

func TestBedrockV170PublicProviderStopReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		writeBedrockV170Event(t, w, "messageStart", map[string]any{"role": "assistant"})
		writeBedrockV170Event(t, w, "messageStop", map[string]any{"stopReason": "guardrail_intervened"})
	}))
	defer server.Close()
	model := bedrockV170Model(server.URL)
	message, err := bedrockV170Result(t, t.Context(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, bedrockV170Options(model))
	if err != nil {
		t.Fatal(err)
	}
	if message.StopReason != goai.StopReasonError || message.RawStopReason != "guardrail_intervened" || message.ErrorMessage != "Provider stopped with: guardrail_intervened" {
		t.Fatalf("provider stop = %#v", message)
	}
}

func TestBedrockV170PublicFailureMetadataAndAbort(t *testing.T) {
	const requestID = "11111111-2222-3333-4444-555555555555"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Amzn-Errortype", "ValidationException")
		w.Header().Set("X-Amzn-Requestid", requestID)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"The provided model identifier is invalid."}`))
	}))
	defer server.Close()
	model := bedrockV170Model(server.URL)
	conv := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}

	message, err := bedrockV170Result(t, t.Context(), model, conv, bedrockV170Options(model))
	if err == nil || message == nil || message.StopReason != goai.StopReasonError {
		t.Fatalf("failure result = %#v, %v", message, err)
	}
	want := map[string]any{"status": http.StatusBadRequest, "errorCode": "ValidationException", "requestId": requestID}
	if len(message.Diagnostics) != 1 || message.Diagnostics[0].Type != "bedrock_response_failure" || !reflect.DeepEqual(message.Diagnostics[0].Details, want) {
		t.Fatalf("diagnostics = %#v, want %v", message.Diagnostics, want)
	}
	if message.ErrorMessage == "" {
		t.Fatal("provider error message was erased")
	}
	if message.Diagnostics[0].Error != (goai.DiagnosticError{}) {
		t.Fatalf("unexpected diagnostic error: %#v", message.Diagnostics[0].Error)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	aborted, abortErr := bedrockV170Result(t, ctx, model, conv, bedrockV170Options(model))
	if abortErr == nil || aborted.StopReason != goai.StopReasonAborted || len(aborted.Diagnostics) != 0 {
		t.Fatalf("aborted = %#v, %v", aborted, abortErr)
	}
}

func TestBedrockV170PublicRedactedReplay(t *testing.T) {
	opaque := []byte("encrypted reasoning")
	encoded := base64.StdEncoding.EncodeToString(opaque)
	model := bedrockV170Model("http://127.0.0.1:1")
	opts := bedrockV170Options(model)
	var input *bedrockruntime.ConverseStreamInput
	opts.OnPayload = func(payload any, _ *goai.Model) (any, error) {
		input = payload.(*bedrockruntime.ConverseStreamInput)
		return nil, context.Canceled
	}
	ctx := &goai.Context{Messages: []goai.Message{
		goai.UserMessage("read it"),
		{Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{
			{Type: "thinking", Redacted: true, ThinkingSignature: encoded},
			{Type: "toolCall", ID: "tool-1", Name: "read", Arguments: map[string]any{"path": "/tmp/a.txt"}},
		}},
	}}
	_, err := bedrockV170Result(t, t.Context(), model, ctx, opts)
	if err == nil {
		t.Fatal("payload abort did not fail the request")
	}
	if input == nil || len(input.Messages) < 2 || len(input.Messages[1].Content) != 2 {
		t.Fatalf("captured input = %#v", input)
	}
	block, ok := input.Messages[1].Content[0].(*types.ContentBlockMemberReasoningContent)
	if !ok {
		t.Fatalf("first assistant member = %T", input.Messages[1].Content[0])
	}
	redacted, ok := block.Value.(*types.ReasoningContentBlockMemberRedactedContent)
	if !ok || !reflect.DeepEqual(redacted.Value, opaque) {
		t.Fatalf("redacted replay = %#v, want %x", block.Value, opaque)
	}
	if _, ok := input.Messages[1].Content[1].(*types.ContentBlockMemberToolUse); !ok {
		t.Fatalf("second assistant member = %T", input.Messages[1].Content[1])
	}
}

func TestBedrockV170RegisteredCatalog(t *testing.T) {
	goai.RegisterBuiltinModels()
	models := goai.ListModels(goai.ProviderAmazonBedrock)
	if len(models) == 0 {
		t.Fatal("Bedrock catalog is empty")
	}
	global := goai.GetModel(goai.ProviderAmazonBedrock, "global.anthropic.claude-opus-5")
	plain := goai.GetModel(goai.ProviderAmazonBedrock, "anthropic.claude-opus-5")
	if global == nil || plain != nil {
		t.Fatalf("Claude Opus 5 catalog = global:%#v plain:%#v", global, plain)
	}
}
