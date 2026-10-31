package mistral

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Expected wire messages come from pinned transformMessages and Mistral conversion.
// Compare JSON values, not serialized property order.
func TestMistralMessageConversionSourceMain(t *testing.T) {
	raw, err := os.ReadFile("testdata/message_conversion_main.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name           string
		SupportsImages bool
		Messages       []goai.Message
		Expected       json.RawMessage
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			model := &goai.Model{ID: "test", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations, Input: []string{"text"}}
			if tc.SupportsImages {
				model.Input = append(model.Input, "image")
			}
			before, err := json.Marshal(tc.Messages)
			if err != nil {
				t.Fatal(err)
			}
			request := buildRequest(model, &goai.Context{Messages: tc.Messages}, &goai.StreamOptions{})
			encoded, err := json.Marshal(request.Messages)
			if err != nil {
				t.Fatal(err)
			}
			var got, want any
			if err = json.Unmarshal(encoded, &got); err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(tc.Expected, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("wire conversion mismatch\ngot %s\nwant %s", encoded, tc.Expected)
			}
			after, err := json.Marshal(tc.Messages)
			if err != nil {
				t.Fatal(err)
			}
			var original, current any
			if err = json.Unmarshal(before, &original); err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(after, &current); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(original, current) {
				t.Fatal("conversion mutated context")
			}
		})
	}
}

func TestMistralMalformedContentSourceMain(t *testing.T) {
	raw, err := os.ReadFile("testdata/malformed_content_main.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name    string
		Content json.RawMessage
		Partial string
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			wire := "data: {\"choices\":[{\"delta\":{\"content\":\"before\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":" + string(tc.Content) + "}}]}\n\ndata: [DONE]\n\n"
			failed, done := 0, 0
			processSSEStream(strings.NewReader(wire), &goai.Model{}, func(event goai.Event) {
				switch e := event.(type) {
				case *goai.ErrorEvent:
					failed++
					if e.Err == nil || len(e.Error.Content) != 1 || e.Error.Content[0].Text != tc.Partial {
						t.Fatal("partial content mismatch", e)
					}
				case *goai.DoneEvent:
					done++
				}
			})
			if failed != 1 || done != 0 {
				t.Fatal("malformed content silently skipped", failed, done)
			}
		})
	}
}
