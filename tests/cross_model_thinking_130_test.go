package goai_test

import (
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestThinking130CrossModelDropsInsteadOfText(t *testing.T) {
	model := &goai.Model{Provider: "provider", Api: "api", ID: "target", Input: []string{"text"}}
	original := goai.Message{Role: goai.RoleAssistant, Provider: model.Provider, Api: model.Api, Model: model.ID, Content: []goai.ContentBlock{{Type: "thinking", Thinking: "private reasoning"}, {Type: "thinking", ThinkingSignature: "signed"}, {Type: "thinking", Redacted: true}, {Type: "thinking", Thinking: " \n\t"}, {Type: "text", Text: "answer"}}}
	for _, field := range []string{"same", "model", "api", "provider"} {
		t.Run(field, func(t *testing.T) {
			msg := original
			msg.Content = append([]goai.ContentBlock(nil), original.Content...)
			switch field {
			case "model":
				msg.Model = "other"
			case "api":
				msg.Api = "other"
			case "provider":
				msg.Provider = "other"
			}
			before := append([]goai.ContentBlock(nil), msg.Content...)
			out := goai.TransformMessages([]goai.Message{msg}, model)
			if len(out) != 1 {
				t.Fatal(out)
			}
			if field == "same" {
				if len(out[0].Content) != 4 || out[0].Content[0].Type != "thinking" || out[0].Content[1].ThinkingSignature != "signed" || !out[0].Content[2].Redacted {
					t.Fatal(out)
				}
			} else if len(out[0].Content) != 1 || out[0].Content[0].Text != "answer" {
				t.Fatal(out)
			}
			if !reflect.DeepEqual(before, msg.Content) {
				t.Fatal("caller content mutated")
			}
		})
	}
}
