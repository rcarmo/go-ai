package google

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// TestSourceV161GoogleToolReplay observes the JSON emitted by both adapters.
// Raw fields distinguish an omitted property from an explicit empty or null value.
func TestSourceV161GoogleToolReplay(t *testing.T) {
	const genAI = goai.ApiGoogleGenerativeAI
	const vertex = goai.ApiGoogleVertex
	const pro = "gemini-3-pro-preview"
	const flash = "gemini-2.5-flash"
	const signature = "AAAAAAAAAAAAAAAAAAAAAA=="
	for _, tc := range []struct {
		name, model, previous, signature string
		api                              goai.Api
		ids                              bool
	}{
		{"history/genai-pro", pro, pro, "", genAI, true},
		{"history/genai-3.6", "gemini-3.6-flash", "gemini-3.6-flash", "", genAI, true},
		{"history/vertex-pro", pro, pro, "", vertex, true},
		{"unsigned-genai", pro, "other-model", "", genAI, true},
		{"unsigned-vertex", pro, pro, "", vertex, true},
		{"same-model-signature", pro, pro, signature, genAI, true},
		{"gemini-2.5", flash, "other-model", "", genAI, false},
		{"id-rule/gemini-2.5", flash, flash, "", genAI, false},
		{"id-rule/gemini-3.6", "gemini-3.6-flash", "gemini-3.6-flash", "", genAI, true},
		{"id-rule/prefixed-4", "randomprefix-gemini-4-argon", "randomprefix-gemini-4-argon", "", genAI, true},
		{"id-rule/prefixed-2.5", "vendor-gemini-2.5-flash", "vendor-gemini-2.5-flash", "", genAI, false},
		{"id-rule/claude", "claude-sonnet-4-5", "claude-sonnet-4-5", "", genAI, true},
		{"id-rule/gpt-oss", "gpt-oss-120b", "gpt-oss-120b", "", genAI, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := goai.ProviderGoogle
			if tc.api == vertex {
				provider = goai.ProviderGoogleVertex
			}
			model := makeGemini3UpstreamModel(tc.api, provider, tc.model)
			model.BaseURL = "https://upstream.test/v1"
			previous := *model
			previous.ID = tc.previous
			transcript := makeGemini3UnsignedToolCallContext(&previous, tc.signature)
			for i, text := range []string{"hi", "files"} {
				transcript.Messages = append(transcript.Messages, goai.Message{
					Role: goai.RoleToolResult, ToolName: "bash", ToolCallID: fmt.Sprintf("call_%d", i+1),
					Content: []goai.ContentBlock{{Type: "text", Text: text}},
				})
			}
			type wirePart struct {
				Text             string                     `json:"text"`
				Signature        json.RawMessage            `json:"thoughtSignature"`
				FunctionCall     map[string]json.RawMessage `json:"functionCall"`
				FunctionResponse map[string]json.RawMessage `json:"functionResponse"`
			}
			var wire struct {
				Contents []json.RawMessage `json:"contents"`
			}
			requests := 0
			transport := vertexTokenWireFunc(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				requests++
				if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
					Body: io.NopCloser(strings.NewReader("data: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n"))}, nil
			})
			zero := 0
			message, err := goai.Complete(t.Context(), model, transcript, &goai.StreamOptions{APIKey: "test-key", MaxRetries: &zero, HTTPTransport: transport})
			if err != nil || message == nil || message.StopReason != goai.StopReasonStop || requests != 1 {
				t.Fatalf("scripted request: %d requests, message %#v, err %v", requests, message, err)
			}
			var callIDs, responseIDs, signatures []string
			modelTurn := false
			for _, raw := range wire.Contents {
				var content struct {
					Role  string     `json:"role"`
					Parts []wirePart `json:"parts"`
				}
				if err := json.Unmarshal(raw, &content); err != nil {
					t.Fatal(err)
				}
				if content.Role == "model" {
					modelTurn = true
					if strings.Contains(string(raw), "skip_thought_signature_validator") {
						t.Error("unexpected synthetic thought signature")
					}
				}
				for _, part := range content.Parts {
					if content.Role == "model" && strings.Contains(part.Text, "Historical context") {
						t.Errorf("unexpected synthetic history: %q", part.Text)
					}
					if part.FunctionCall != nil {
						callIDs = append(callIDs, string(part.FunctionCall["id"]))
						signatures = append(signatures, string(part.Signature))
					}
					if part.FunctionResponse != nil {
						responseIDs = append(responseIDs, string(part.FunctionResponse["id"]))
					}
				}
			}
			if !modelTurn {
				t.Fatal("no model turn in request")
			}
			wantIDs := []string{"", ""}
			if tc.ids {
				wantIDs = []string{`"call_1"`, `"call_2"`}
			}
			if !slices.Equal(callIDs, wantIDs) {
				t.Errorf("call IDs = %q; want %q", callIDs, wantIDs)
			}
			if !slices.Equal(responseIDs, wantIDs) {
				t.Errorf("response IDs = %q; want %q", responseIDs, wantIDs)
			}
			wantSignatures := []string{"", ""}
			if tc.signature != "" {
				wantSignatures[0] = fmt.Sprintf("%q", tc.signature)
			}
			if !slices.Equal(signatures, wantSignatures) {
				t.Errorf("signatures = %q; want %q", signatures, wantSignatures)
			}
		})
	}
}
