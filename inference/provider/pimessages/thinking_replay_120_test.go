package pimessages

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestPiMessages120TerminalPreservesProviderThinkingLevel(t *testing.T) {
	for _, terminal := range []string{"done", "error"} {
		for _, level := range []string{"high", ""} {
			t.Run(terminal+"/"+level, func(t *testing.T) {
				convert := newConverter(&goai.Model{ID: "claude-opus-5-5", Provider: "radius", Api: goai.ApiPiMessages})
				for _, raw := range []string{`{"type":"thinking_start","contentIndex":0}`, `{"type":"thinking_end","contentIndex":0,"content":"reasoning","contentSignature":"opaque-signature"}`} {
					if _, _, err := convert([]byte(raw)); err != nil {
						t.Fatal(err)
					}
				}
				wire := map[string]any{"type": terminal, "reason": "stop", "responseId": "response"}
				if level != "" {
					wire["providerThinkingLevel"] = level
				}
				if terminal == "error" {
					wire["reason"] = "error"
					wire["errorMessage"] = "fixture"
				}
				raw, err := json.Marshal(wire)
				if err != nil {
					t.Fatal(err)
				}
				event, done, err := convert(raw)
				if err != nil || !done {
					t.Fatal(event, done, err)
				}
				var message *goai.Message
				switch event := event.(type) {
				case *goai.DoneEvent:
					message = event.Message
				case *goai.ErrorEvent:
					message = event.Error
				default:
					t.Fatalf("wrong terminal %T", event)
				}
				if message.ProviderThinkingLevel != level || len(message.Content) != 1 || message.Content[0].ThinkingSignature != "opaque-signature" {
					t.Fatal("thinking replay metadata lost", message)
				}
				raw, err = json.Marshal(message)
				if err != nil {
					t.Fatal(err)
				}
				var replay goai.Message
				if err := json.Unmarshal(raw, &replay); err != nil {
					t.Fatal(err)
				}
				if replay.ProviderThinkingLevel != level {
					t.Fatal("persisted replay marker lost", replay)
				}
			})
		}
	}
}
