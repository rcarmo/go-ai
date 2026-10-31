package google

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestGoogleSignedEmptyPartsMain(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		for _, kind := range []string{"text", "thinking"} {
			for _, text := range []string{"", " \t\n", "visible"} {
				for _, signature := range []string{"", "invalid", "c2ln"} {
					for _, same := range []bool{false, true} {
						t.Run(fmt.Sprint(api, "/", kind, "/", text, "/", signature, "/", same), func(t *testing.T) {
							model := &goai.Model{ID: "gemini-3-pro", Provider: goai.ProviderGoogle, Api: api}
							block := goai.ContentBlock{Type: kind, Text: text, Thinking: text, TextSignature: signature, ThinkingSignature: signature}
							msg := goai.Message{Role: goai.RoleAssistant, Provider: model.Provider, Api: model.Api, Model: model.ID, Content: []goai.ContentBlock{block}}
							if !same {
								msg.Model = "another-model"
							}
							contents := convertMessages(model, &goai.Context{Messages: []goai.Message{msg}})
							keep := strings.TrimSpace(text) != "" || (same && signature == "c2ln")
							if kind == "thinking" && !same {
								keep = false
							} // Preserve native cross-model thinking isolation.
							if !keep {
								if len(contents) != 0 {
									t.Fatal("unusable empty block retained", contents)
								}
								return
							}
							if len(contents) != 1 || len(contents[0].Parts) != 1 {
								t.Fatal("signed block lost", contents)
							}
							encoded, err := json.Marshal(contents[0].Parts[0])
							if err != nil {
								t.Fatal(err)
							}
							var part map[string]any
							if json.Unmarshal(encoded, &part) != nil {
								t.Fatal("invalid part")
							}
							if part["text"] != text {
								t.Fatal("visible text field lost", part)
							}
							if same && signature == "c2ln" {
								if part["thoughtSignature"] != "c2ln" {
									t.Fatal("signature lost", part)
								}
							} else if _, exists := part["thoughtSignature"]; exists {
								t.Fatal("unusable signature retained", part)
							}
							if thought, _ := part["thought"].(bool); thought != (kind == "thinking" && same) {
								t.Fatal("wrong thought marker", part)
							}
						})
					}
				}
			}
		}
	}
}
func TestGoogleToolCallSignaturesAndIDsMain(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		for _, id := range []string{"gemini-2.5-pro", "gemini-3-pro", "gemini-live-3-pro", "gemini-4-pro", "claude-model", "gpt-oss-model"} {
			for _, signature := range []string{"", "c2ln"} {
				model := &goai.Model{ID: id, Provider: goai.ProviderGoogle, Api: api}
				callID := "call/" + strings.Repeat("x", 70)
				ctx := &goai.Context{Messages: []goai.Message{{Role: goai.RoleAssistant, Provider: model.Provider, Api: model.Api, Model: model.ID, Content: []goai.ContentBlock{{Type: "toolCall", ID: callID, Name: "tool", ThoughtSignature: signature}}}, {Role: goai.RoleToolResult, ToolName: "tool", ToolCallID: callID, Content: []goai.ContentBlock{{Type: "text", Text: "ok"}}}}}
				parts := convertMessages(model, ctx)
				call := parts[0].Parts[0]
				response := parts[1].Parts[0].FunctionResponse
				expected := ""
				if id != "gemini-2.5-pro" {
					expected = normalizeGoogleToolCallID(callID)
				}
				if call.FunctionCall.ID != expected || response.ID != expected {
					t.Fatal("call/response ID mismatch", id, call, response)
				}
				if call.ThoughtSignature != signature || call.ThoughtSignature == "skip_thought_signature_validator" {
					t.Fatal("invented/lost signature", call)
				}
				if call.FunctionCall.Args == nil {
					t.Fatal("nil arguments not converted to object")
				}
				data, err := json.Marshal(call)
				if err != nil {
					t.Fatal(err)
				}
				var wire map[string]any
				json.Unmarshal(data, &wire)
				if _, exists := wire["text"]; exists {
					t.Fatal("tool call gained text field", wire)
				}
			}
		}
	}
}
func TestGoogleToolResultImagesMain(t *testing.T) {
	for _, id := range []string{"gemini-2.5-pro", "gemini-3-pro", "gemini-live-3-pro", "gemini-4-pro", "claude-model"} {
		for _, images := range []bool{false, true} {
			for _, isError := range []bool{false, true} {
				for _, text := range []string{"", " \t", "real"} {
					t.Run(fmt.Sprint(id, images, isError, text), func(t *testing.T) {
						model := &goai.Model{ID: id, Provider: goai.ProviderGoogle, Input: []string{"text"}}
						if images {
							model.Input = append(model.Input, "image")
						}
						ctx := &goai.Context{Messages: []goai.Message{{Role: goai.RoleToolResult, ToolName: "tool", ToolCallID: "call", IsError: isError, Content: []goai.ContentBlock{{Type: "text", Text: text}, {Type: "image", MimeType: "image/png", Data: "AQID"}}}}}
						contents := convertMessages(model, ctx)
						response := contents[0].Parts[0].FunctionResponse
						key := "output"
						if isError {
							key = "error"
						}
						want := ""
						if text == "real" {
							want = text
						} else if images {
							want = "(see attached image)"
						}
						if !images {
							want = text + "\n(tool image omitted: model does not support images)"
						}
						if response.Response[key] != want {
							t.Fatal("wrong response value", response.Response, want)
						}
						nested := images && id != "gemini-2.5-pro"
						if (len(response.Parts) > 0) != nested {
							t.Fatal("wrong nested images", response)
						}
						wantTurns := 1
						if images && !nested {
							wantTurns = 2
						}
						if len(contents) != wantTurns {
							t.Fatal("wrong synthetic image turns", contents)
						}
						if wantTurns == 2 && (contents[1].Parts[0].Text != "Tool result image:" || contents[1].Parts[1].InlineData.Data != "AQID") {
							t.Fatal("legacy image turn lost", contents)
						}
					})
				}
			}
		}
	}
	model := &goai.Model{ID: "gemini-3-pro"}
	contents := convertMessages(model, &goai.Context{Messages: []goai.Message{{Role: goai.RoleToolResult, ToolName: "tool", Content: []goai.ContentBlock{{Type: "text", Text: "one"}, {Type: "text", Text: "two"}}}}})
	if contents[0].Parts[0].FunctionResponse.Response["output"] != "one\ntwo" {
		t.Fatal("text parts not newline-joined")
	}
}

func TestGoogleSignatureSyntaxMain(t *testing.T) {
	for _, signature := range []string{"====", "a===", "=abc", "a=b=", "@@@@", "c2l", ""} {
		if isValidBase64Signature(signature) {
			t.Fatal("invalid signature accepted", signature)
		}
	}
	for _, signature := range []string{"c2ln", "YQ==", "YWI=", "AB==", "abcdAAAA"} {
		if !isValidBase64Signature(signature) {
			t.Fatal("source-valid signature rejected", signature)
		}
	}
}
