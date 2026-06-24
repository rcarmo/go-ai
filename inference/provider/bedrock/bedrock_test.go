package bedrock

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	goai "github.com/rcarmo/go-ai"
)

func TestExtractRegionFromURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://bedrock-runtime.us-east-1.amazonaws.com", "us-east-1"},
		{"https://bedrock-runtime.eu-west-1.amazonaws.com", "eu-west-1"},
		{"https://bedrock-runtime-fips.us-gov-west-1.amazonaws.com", "us-gov-west-1"},
		{"https://bedrock-runtime.cn-north-1.amazonaws.com.cn", "cn-north-1"},
		{"https://example.com", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := extractRegionFromURL(tt.url); got != tt.want {
			t.Fatalf("extractRegionFromURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestShouldUseExplicitBedrockEndpoint(t *testing.T) {
	if !shouldUseExplicitBedrockEndpoint("https://bedrock-runtime.us-east-1.amazonaws.com", "", false) {
		t.Fatal("expected standard endpoint to be pinned when no region/profile is configured")
	}
	if shouldUseExplicitBedrockEndpoint("https://bedrock-runtime.us-east-1.amazonaws.com", "eu-west-1", false) {
		t.Fatal("expected standard endpoint not to be pinned when a region is configured")
	}
	if !shouldUseExplicitBedrockEndpoint("https://custom-bedrock-proxy.example.com", "eu-west-1", true) {
		t.Fatal("expected custom endpoints to remain explicit")
	}
}

func TestBedrockCustomHeaderReservation(t *testing.T) {
	for _, key := range []string{"authorization", "Authorization", "host", "Host", "x-amz-date", "X-Amz-Security-Token"} {
		if !isReservedBedrockHeader(key) {
			t.Fatalf("expected %q to be reserved", key)
		}
	}
	for _, key := range []string{"x-trace-id", "anthropic-beta"} {
		if isReservedBedrockHeader(key) {
			t.Fatalf("expected %q to be custom-allowed", key)
		}
	}
}

func TestBedrockCustomHeadersMiddlewareSemantics(t *testing.T) {
	req := &smithyhttp.Request{Request: &http.Request{Header: http.Header{
		"Authorization": []string{"real-auth"},
		"Host":          []string{"real-host"},
		"X-Amz-Date":    []string{"real-date"},
		"X-Remove":      []string{"gone"},
	}}}
	applyBedrockCustomHeaders(req, map[string]string{
		"authorization": "evil",
		"Authorization": "evil2",
		"HOST":          "evil3",
		"x-amz-date":    "evil4",
		"X-Amz-Date":    "evil5",
		"x-allowed":     "ok",
	}, []string{"x-remove", "authorization", "x-amz-date"})

	if got := req.Header.Get("Authorization"); got != "real-auth" {
		t.Fatalf("reserved Authorization overwritten: %q", got)
	}
	if got := req.Header.Get("Host"); got != "real-host" {
		t.Fatalf("reserved Host overwritten: %q", got)
	}
	if got := req.Header.Get("X-Amz-Date"); got != "real-date" {
		t.Fatalf("reserved X-Amz-Date overwritten: %q", got)
	}
	if got := req.Header.Get("X-Allowed"); got != "ok" {
		t.Fatalf("allowed custom header missing: %q", got)
	}
	if got := req.Header.Get("X-Remove"); got != "" {
		t.Fatalf("non-reserved suppress header not removed: %q", got)
	}

	applyBedrockCustomHeaders(nil, map[string]string{"x-custom": "v"}, nil)
}

func TestBedrockOptionPrecedenceAndRequestMetadata(t *testing.T) {
	model := &goai.Model{ID: "anthropic.claude-3-5-sonnet", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream}
	if got := getConfiguredBedrockRegion(model, &goai.StreamOptions{Region: "eu-central-1", Env: goai.ProviderEnv{"AWS_REGION": "us-west-2"}}, nil); got != "eu-central-1" {
		t.Fatalf("expected explicit region option, got %q", got)
	}
	input := buildConverseInput(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{RequestMetadata: map[string]string{"trace": "abc"}})
	if input.RequestMetadata["trace"] != "abc" {
		t.Fatalf("request metadata not propagated: %#v", input.RequestMetadata)
	}
}

func TestBuildConverseInputIncludesSystemToolsAndThinking(t *testing.T) {
	level := goai.ThinkingHigh
	customBudget := 7777
	model := &goai.Model{ID: "anthropic.claude-3-7-sonnet", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream, Reasoning: true}
	ctx := &goai.Context{
		SystemPrompt: "You are helpful.",
		Messages:     []goai.Message{goai.UserMessage("hello")},
		Tools:        []goai.Tool{{Name: "search", Description: "Search docs", Parameters: []byte(`{"type":"object"}`)}},
	}
	opts := &goai.StreamOptions{
		MaxTokens:       &[]int{1234}[0],
		Reasoning:       &level,
		ThinkingBudgets: &goai.ThinkingBudgets{High: &customBudget},
	}

	input := buildConverseInput(model, ctx, opts)
	if aws.ToString(input.ModelId) != model.ID {
		t.Fatalf("unexpected model id: %q", aws.ToString(input.ModelId))
	}
	if len(input.System) != 2 { // text + cache point
		t.Fatalf("expected 2 system blocks (text + cache point), got %d", len(input.System))
	}
	if len(input.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(input.Messages))
	}
	if input.InferenceConfig == nil || aws.ToInt32(input.InferenceConfig.MaxTokens) != 1234 {
		t.Fatal("expected inference config max tokens")
	}
	if input.ToolConfig == nil || len(input.ToolConfig.Tools) != 1 {
		t.Fatal("expected one tool in tool config")
	}
	if input.AdditionalModelRequestFields == nil {
		t.Fatal("expected additional model request fields for thinking config")
	}
}

func TestBuildConverseInputUsesNativeXhighForClaudeOpus47(t *testing.T) {
	level := goai.ThinkingXHigh
	model := &goai.Model{ID: "us.anthropic.claude-opus-4-7-20260115-v1:0", Name: "Claude Opus 4.7", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream, Reasoning: true}
	input := buildConverseInput(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{Reasoning: &level})
	if input.AdditionalModelRequestFields == nil {
		t.Fatal("expected additional model request fields")
	}
	data, err := input.AdditionalModelRequestFields.MarshalSmithyDocument()
	if err != nil {
		t.Fatalf("marshal additional fields: %v", err)
	}
	var fields map[string]interface{}
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("decode additional fields: %v", err)
	}
	outputConfig, _ := fields["output_config"].(map[string]interface{})
	if outputConfig["effort"] != "xhigh" {
		t.Fatalf("expected native xhigh effort, got %#v in %#v", outputConfig["effort"], fields)
	}
}

func TestConvertMessagesCoalescesConsecutiveToolResults(t *testing.T) {
	model := &goai.Model{ID: "anthropic.claude-3-7-sonnet", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream, Input: []string{"text"}}
	ctx := &goai.Context{Messages: []goai.Message{
		goai.UserMessage("start"),
		{Role: goai.RoleToolResult, ToolCallID: "tc1", ToolName: "a", Content: []goai.ContentBlock{{Type: "text", Text: "one"}}},
		{Role: goai.RoleToolResult, ToolCallID: "tc2", ToolName: "b", Content: []goai.ContentBlock{{Type: "text", Text: "two"}}, IsError: true},
	}}

	msgs := convertMessages(ctx, model, "short", nil)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 bedrock messages, got %d", len(msgs))
	}
	if msgs[1].Role != types.ConversationRoleUser {
		t.Fatalf("expected tool results to become user message, got %v", msgs[1].Role)
	}
	if len(msgs[1].Content) != 3 { // 2 tool results + cache point
		t.Fatalf("expected 3 blocks (2 tool results + cache point), got %d", len(msgs[1].Content))
	}
}

func TestCreateImageBlockDecodesBase64(t *testing.T) {
	block := createImageBlock("image/png", "aGVsbG8=")
	img, ok := block.(*types.ContentBlockMemberImage)
	if !ok {
		t.Fatalf("expected image block, got %T", block)
	}
	if img.Value.Format != types.ImageFormatPng {
		t.Fatalf("expected png format, got %v", img.Value.Format)
	}
}

func TestBedrockPayloadHookCanReplaceInput(t *testing.T) {
	orig := &bedrockruntime.ConverseStreamInput{ModelId: aws.String("a")}
	replaced := &bedrockruntime.ConverseStreamInput{ModelId: aws.String("b")}
	payload, err := goai.InvokeOnPayload(&goai.StreamOptions{OnPayload: func(payload interface{}, model *goai.Model) (interface{}, error) {
		return replaced, nil
	}}, orig, &goai.Model{})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := payload.(*bedrockruntime.ConverseStreamInput)
	if !ok || aws.ToString(got.ModelId) != "b" {
		t.Fatalf("expected replaced input, got %#v", payload)
	}
}
