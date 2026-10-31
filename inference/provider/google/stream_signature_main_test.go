package google

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestGooglePartTextPresenceMain(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		present bool
	}{{`{"text":""}`, true}, {`{"text":"visible"}`, true}, {`{"functionCall":{"name":"tool","args":{}},"thoughtSignature":"c2ln"}`, false}, {`{"thoughtSignature":"c2ln"}`, false}, {`{"inlineData":{"mimeType":"image/png","data":"AQID"}}`, false}} {
		var part geminiPart
		if err := json.Unmarshal([]byte(tc.raw), &part); err != nil {
			t.Fatal(err)
		}
		if part.TextPresent != tc.present {
			t.Fatal("text presence lost", tc.raw, part)
		}
		encoded, err := json.Marshal(part)
		if err != nil {
			t.Fatal(err)
		}
		var wire map[string]any
		json.Unmarshal(encoded, &wire)
		if _, present := wire["text"]; present != tc.present {
			t.Fatal("wire text presence changed", string(encoded))
		}
		if err := json.Unmarshal([]byte(`{}`), &part); err != nil || part.TextPresent || part.Text != "" || part.FunctionCall != nil {
			t.Fatal("reused part retained stale state", part, err)
		}
	}
}

func TestGoogleStreamSignedEmptyPartsMain(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		t.Run(string(api), func(t *testing.T) {
			model := &goai.Model{ID: "gemini-3-pro", Provider: goai.ProviderGoogle, Api: api}
			chunks := []string{
				`{"thought":true,"text":"","thoughtSignature":"AAAA"}`,
				`{"thought":true,"text":"reason"}`,
				`{"text":"","thoughtSignature":"BBBB"}`,
				`{"functionCall":{"id":"call","name":"tool","args":{}},"thoughtSignature":"CCCC"}`,
				`{"text":"tail","thoughtSignature":"DDDD"}`,
				`{"text":"","thoughtSignature":"EEEE"}`,
			}
			var stream strings.Builder
			for _, part := range chunks {
				stream.WriteString(`data: {"candidates":[{"content":{"parts":[` + part + `]}}]}` + "\n\n")
			}
			stream.WriteString("data: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n")
			events := make(chan goai.Event, 64)
			processStream(strings.NewReader(stream.String()), model, goai.NewAssistantEventSender(events))
			close(events)
			var done *goai.DoneEvent
			thinkingStarts, textStarts, thinkingEnds, textEnds, emptyDeltas := 0, 0, 0, 0, 0
			for event := range events {
				switch e := event.(type) {
				case *goai.ThinkingStartEvent:
					thinkingStarts++
				case *goai.TextStartEvent:
					textStarts++
				case *goai.ThinkingEndEvent:
					thinkingEnds++
				case *goai.TextEndEvent:
					textEnds++
				case *goai.ThinkingDeltaEvent:
					if e.Delta == "" {
						emptyDeltas++
					}
				case *goai.TextDeltaEvent:
					if e.Delta == "" {
						emptyDeltas++
					}
				case *goai.DoneEvent:
					if done != nil {
						t.Fatal("multiple completions")
					}
					done = e
				case *goai.ErrorEvent:
					t.Fatal(e.Err)
				}
			}
			if done == nil || done.Message == nil {
				t.Fatal("missing completion")
			}
			blocks := done.Message.Content
			if len(blocks) != 4 || thinkingStarts != 1 || thinkingEnds != 1 || textStarts != 2 || textEnds != 2 || emptyDeltas != 3 {
				t.Fatal("wrong part lifecycle", blocks, thinkingStarts, thinkingEnds, textStarts, textEnds, emptyDeltas)
			}
			if blocks[0].Thinking != "reason" || blocks[0].ThinkingSignature != "AAAA" || blocks[1].Text != "" || blocks[1].TextSignature != "BBBB" || blocks[2].ThoughtSignature != "CCCC" || blocks[3].Text != "tail" || blocks[3].TextSignature != "EEEE" {
				t.Fatal("lost/moved signature", blocks)
			}
			replay := convertMessages(model, &goai.Context{Messages: []goai.Message{*done.Message}})
			if len(replay) == 0 || len(replay[0].Parts) != 4 {
				t.Fatal("stream signatures not replayable", replay)
			}
			encoded, err := json.Marshal(replay[0].Parts[1])
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]any
			json.Unmarshal(encoded, &wire)
			if text, exists := wire["text"]; !exists || text != "" || wire["thoughtSignature"] != "BBBB" {
				t.Fatal("signed empty stream part lost on replay", wire)
			}
		})
	}
}
