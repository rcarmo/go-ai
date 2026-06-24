package google

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func makeGoogleSharedTool(parameters string) goai.Tool {
	return goai.Tool{Name: "test_tool", Description: "A test tool", Parameters: json.RawMessage(parameters)}
}

func firstGoogleToolDecl(t *testing.T, tools []geminiToolDecl) geminiToolFunc {
	t.Helper()
	if len(tools) == 0 || len(tools[0].FunctionDeclarations) == 0 {
		t.Fatalf("expected function declaration: %#v", tools)
	}
	return tools[0].FunctionDeclarations[0]
}

func decodeRawJSON(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, string(raw))
	}
	return out
}

func TestGoogleSharedConvertToolsStripsJSONSchemaMetaKeysFromParametersWhenUseParametersTrue(t *testing.T) {
	tools := []goai.Tool{makeGoogleSharedTool(`{"$schema":"http://json-schema.org/draft-07/schema#","$id":"urn:bash-tool","$comment":"A bash tool for demonstration","$defs":{"commandDef":{"type":"string"}},"definitions":{"legacyDef":{"type":"number"}},"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`)}
	decl := firstGoogleToolDecl(t, convertTools(tools, true))
	want := map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}}, "required": []any{"command"}}
	if got := decodeRawJSON(t, decl.Parameters); !reflect.DeepEqual(got, want) {
		t.Fatalf("parameters=%#v, want %#v", got, want)
	}
	if s := string(decl.Parameters); strings.Contains(s, "$schema") || strings.Contains(s, "$id") || strings.Contains(s, "$comment") || strings.Contains(s, "$defs") || strings.Contains(s, "definitions") {
		t.Fatalf("parameters still contain stripped meta key: %s", s)
	}
}

func TestGoogleSharedConvertToolsRecursivelyStripsNestedJSONSchemaMetaKeys(t *testing.T) {
	tools := []goai.Tool{makeGoogleSharedTool(`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"deep":{"$schema":"http://json-schema.org/draft-07/schema#","$id":"urn:nested","type":"string"}}}`)}
	decl := firstGoogleToolDecl(t, convertTools(tools, true))
	want := map[string]any{"type": "object", "properties": map[string]any{"deep": map[string]any{"type": "string"}}}
	if got := decodeRawJSON(t, decl.Parameters); !reflect.DeepEqual(got, want) {
		t.Fatalf("parameters=%#v, want %#v", got, want)
	}
}

func TestGoogleSharedConvertToolsPreservesRefWhileStrippingMetaKeys(t *testing.T) {
	tools := []goai.Tool{makeGoogleSharedTool(`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"refProp":{"$ref":"#/$defs/someDef","type":"string"}}}`)}
	decl := firstGoogleToolDecl(t, convertTools(tools, true))
	want := map[string]any{"type": "object", "properties": map[string]any{"refProp": map[string]any{"$ref": "#/$defs/someDef", "type": "string"}}}
	if got := decodeRawJSON(t, decl.Parameters); !reflect.DeepEqual(got, want) {
		t.Fatalf("parameters=%#v, want %#v", got, want)
	}
}

func TestGoogleSharedConvertToolsDoesNotMutateOriginalToolParametersObject(t *testing.T) {
	original := json.RawMessage(`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`)
	before := append(json.RawMessage(nil), original...)
	convertTools([]goai.Tool{{Name: "test_tool", Description: "A test tool", Parameters: original}}, true)
	if !reflect.DeepEqual(original, before) {
		t.Fatalf("original parameters mutated: got %s want %s", string(original), string(before))
	}
}

func TestGoogleSharedConvertToolsPreservesSchemaInParametersJSONSchemaWhenUseParametersFalse(t *testing.T) {
	tools := []goai.Tool{makeGoogleSharedTool(`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`)}
	decl := firstGoogleToolDecl(t, convertTools(tools, false))
	want := map[string]any{"$schema": "http://json-schema.org/draft-07/schema#", "type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}}, "required": []any{"command"}}
	if got := decodeRawJSON(t, decl.ParametersJsonSchema); !reflect.DeepEqual(got, want) {
		t.Fatalf("parametersJsonSchema=%#v, want %#v", got, want)
	}
}

func TestGoogleSharedConvertToolsHandlesToolsWithoutSchemaGracefully(t *testing.T) {
	tools := []goai.Tool{makeGoogleSharedTool(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)}
	decl := firstGoogleToolDecl(t, convertTools(tools, true))
	want := map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []any{"path"}}
	if got := decodeRawJSON(t, decl.Parameters); !reflect.DeepEqual(got, want) {
		t.Fatalf("parameters=%#v, want %#v", got, want)
	}
}

func TestGoogleSharedConvertToolsReturnsUndefinedForEmptyToolList(t *testing.T) {
	if got := convertTools(nil); got != nil {
		t.Fatalf("convertTools(nil)=%#v, want nil", got)
	}
	if got := convertTools([]goai.Tool{}, true); got != nil {
		t.Fatalf("convertTools(empty,true)=%#v, want nil", got)
	}
}

func makeGemini3UpstreamModel(api goai.Api, provider goai.Provider, id string) *goai.Model {
	return &goai.Model{ID: id, Name: "Gemini 3 Pro Preview", Api: api, Provider: provider, BaseURL: "https://example.com", Reasoning: true, Input: []string{"text"}, Cost: goai.ModelCost{}, ContextWindow: 128000, MaxTokens: 8192}
}

func makeGemini3UnsignedToolCallContext(model *goai.Model, thoughtSignature string) *goai.Context {
	now := time.Now().UnixMilli()
	return &goai.Context{Messages: []goai.Message{
		{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "Hi"}}, Timestamp: now},
		{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "toolCall", ID: "call_1", Name: "bash", Arguments: map[string]any{"command": "echo hi"}, ThoughtSignature: thoughtSignature}, {Type: "toolCall", ID: "call_2", Name: "bash", Arguments: map[string]any{"command": "ls -la"}}}, Api: model.Api, Provider: model.Provider, Model: model.ID, Usage: &goai.Usage{}, StopReason: goai.StopReasonToolUse, Timestamp: now},
	}}
}

func findModelTurnForGoogleShared(t *testing.T, contents []geminiContent) geminiContent {
	t.Helper()
	for _, c := range contents {
		if c.Role == "model" {
			return c
		}
	}
	t.Fatalf("model turn not found: %#v", contents)
	return geminiContent{}
}

func functionCallParts(parts []geminiPart) []geminiPart {
	var out []geminiPart
	for _, p := range parts {
		if p.FunctionCall != nil {
			out = append(out, p)
		}
	}
	return out
}

func TestGoogleSharedGemini3UnsignedToolCallsDoesNotAddSkipThoughtSignatureValidatorForGoogleGenAI(t *testing.T) {
	model := makeGemini3UpstreamModel(goai.ApiGoogleGenerativeAI, goai.ProviderGoogle, "gemini-3-pro-preview")
	other := *model
	other.ID = "other-model"
	modelTurn := findModelTurnForGoogleShared(t, convertMessages(model, makeGemini3UnsignedToolCallContext(&other, "")))
	parts := functionCallParts(modelTurn.Parts)
	if len(parts) != 2 || parts[0].ThoughtSignature != "" || parts[1].ThoughtSignature != "" {
		t.Fatalf("function call parts=%#v, want two unsigned parts", parts)
	}
	if strings.Contains(toJSONForGoogleShared(t, modelTurn), "skip_thought_signature_validator") || strings.Contains(toJSONForGoogleShared(t, modelTurn), "Historical context") {
		t.Fatalf("model turn contains legacy validator text: %#v", modelTurn)
	}
}

func TestGoogleSharedGemini3UnsignedToolCallsDoesNotAddSkipThoughtSignatureValidatorForVertex(t *testing.T) {
	model := makeGemini3UpstreamModel(goai.ApiGoogleVertex, goai.ProviderGoogleVertex, "gemini-3-pro-preview")
	modelTurn := findModelTurnForGoogleShared(t, convertMessages(model, makeGemini3UnsignedToolCallContext(model, "")))
	parts := functionCallParts(modelTurn.Parts)
	if len(parts) != 2 || parts[0].ThoughtSignature != "" || parts[1].ThoughtSignature != "" || strings.Contains(toJSONForGoogleShared(t, modelTurn), "skip_thought_signature_validator") {
		t.Fatalf("function call parts/modelTurn=%#v/%s, want two unsigned parts without validator", parts, toJSONForGoogleShared(t, modelTurn))
	}
}

func TestGoogleSharedGemini3UnsignedToolCallsPreservesValidThoughtSignatureForSameProviderAndModel(t *testing.T) {
	model := makeGemini3UpstreamModel(goai.ApiGoogleGenerativeAI, goai.ProviderGoogle, "gemini-3-pro-preview")
	validSig := "AAAAAAAAAAAAAAAAAAAAAA=="
	parts := functionCallParts(findModelTurnForGoogleShared(t, convertMessages(model, makeGemini3UnsignedToolCallContext(model, validSig))).Parts)
	if len(parts) != 2 || parts[0].ThoughtSignature != validSig || parts[1].ThoughtSignature != "" {
		t.Fatalf("function call parts=%#v, want first signature %q and second unsigned", parts, validSig)
	}
}

func TestGoogleSharedGemini3UnsignedToolCallsDoesNotAddThoughtSignatureForNonGemini3Models(t *testing.T) {
	model := makeGemini3UpstreamModel(goai.ApiGoogleGenerativeAI, goai.ProviderGoogle, "gemini-2.5-flash")
	other := *model
	other.ID = "other-model"
	parts := functionCallParts(findModelTurnForGoogleShared(t, convertMessages(model, makeGemini3UnsignedToolCallContext(&other, ""))).Parts)
	if len(parts) == 0 || parts[0].ThoughtSignature != "" {
		t.Fatalf("first function call part=%#v, want unsigned", parts)
	}
}

func toJSONForGoogleShared(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func makeGoogleImageRoutingModel(id string) *goai.Model {
	return &goai.Model{ID: id, Name: id, Api: goai.ApiGoogleGenerativeAI, Provider: goai.ProviderGoogle, BaseURL: "https://example.com", Reasoning: true, Input: []string{"text", "image"}, Cost: goai.ModelCost{}, ContextWindow: 128000, MaxTokens: 8192}
}

func makeGoogleImageRoutingContext(model *goai.Model) *goai.Context {
	now := time.Now().UnixMilli()
	return &goai.Context{Messages: []goai.Message{
		{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "read the files"}}, Timestamp: now},
		{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "toolCall", ID: "call_a", Name: "read", Arguments: map[string]any{"path": "a.txt"}}, {Type: "toolCall", ID: "call_img", Name: "read", Arguments: map[string]any{"path": "image.png"}}, {Type: "toolCall", ID: "call_b", Name: "read", Arguments: map[string]any{"path": "b.txt"}}}, Api: model.Api, Provider: model.Provider, Model: model.ID, Usage: &goai.Usage{}, StopReason: goai.StopReasonToolUse, Timestamp: now},
		{Role: goai.RoleToolResult, ToolCallID: "call_a", ToolName: "read", Content: []goai.ContentBlock{{Type: "text", Text: "alpha text"}}, Timestamp: now},
		{Role: goai.RoleToolResult, ToolCallID: "call_img", ToolName: "read", Content: []goai.ContentBlock{{Type: "image", Data: "abc", MimeType: "image/png"}}, Timestamp: now},
		{Role: goai.RoleToolResult, ToolCallID: "call_b", ToolName: "read", Content: []goai.ContentBlock{{Type: "text", Text: "beta text"}}, Timestamp: now},
	}}
}

func TestGoogleSharedImageToolResultRoutingKeepsSeparateSyntheticImageTurnForGemini2GoogleAPIModels(t *testing.T) {
	contents := convertMessages(makeGoogleImageRoutingModel("gemini-2.5-flash"), makeGoogleImageRoutingContext(makeGoogleImageRoutingModel("gemini-2.5-flash")))
	if len(contents) != 5 {
		t.Fatalf("len(contents)=%d, want 5: %#v", len(contents), contents)
	}
	for _, part := range contents[2].Parts {
		if part.FunctionResponse == nil {
			t.Fatalf("contents[2] part missing functionResponse: %#v", contents[2])
		}
	}
	if contents[3].Parts[0].Text != "Tool result image:" || contents[3].Parts[1].InlineData == nil {
		t.Fatalf("contents[3]=%#v, want synthetic image turn", contents[3])
	}
	if contents[4].Parts[0].FunctionResponse == nil {
		t.Fatalf("contents[4]=%#v, want trailing functionResponse", contents[4])
	}
}

func TestGoogleSharedImageToolResultRoutingNestsImageToolResultsForGemini3GoogleAPIModels(t *testing.T) {
	model := makeGoogleImageRoutingModel("gemini-3-pro-preview")
	contents := convertMessages(model, makeGoogleImageRoutingContext(model))
	if len(contents) != 3 {
		t.Fatalf("len(contents)=%d, want 3: %#v", len(contents), contents)
	}
	toolResultTurn := contents[2]
	if len(toolResultTurn.Parts) != 3 {
		t.Fatalf("toolResultTurn parts=%d, want 3: %#v", len(toolResultTurn.Parts), toolResultTurn.Parts)
	}
	imageResponse := toolResultTurn.Parts[1].FunctionResponse
	if imageResponse == nil || len(imageResponse.Parts) != 1 || imageResponse.Parts[0].InlineData == nil {
		t.Fatalf("image functionResponse=%#v, want nested inlineData", imageResponse)
	}
}
