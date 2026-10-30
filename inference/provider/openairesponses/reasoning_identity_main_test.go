package openairesponses

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
)

func TestResponsesDuplicateReasoningIdentityMain(t *testing.T) {
	for _, transport := range []string{"responses", "azure"} {
		for _, mode := range []string{"unfinished", "completed"} {
			t.Run(transport+"/"+mode, func(t *testing.T) {
				frames := []string{`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"r"}}`, `{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"r","summary":[{"type":"summary_text","text":"old"}]}}`, `{"type":"response.output_item.added","output_index":1,"item":{"type":"reasoning","id":"r"}}`}
				if mode == "completed" {
					frames = append(frames, `{"type":"response.output_item.done","output_index":1,"item":{"type":"reasoning","id":"r","summary":[{"type":"summary_text","text":"new"}]}}`)
				}
				frames = append(frames, `{"type":"response.completed","response":{"id":"response_id","status":"completed","output":[{"type":"reasoning","id":"r","encrypted_content":"latest"}],"usage":{}}}`)
				var final *goai.Message
				send := func(event goai.Event) {
					switch e := goai.SnapshotEvent(event).(type) {
					case *goai.DoneEvent:
						final = e.Message
					case *goai.ErrorEvent:
						t.Error(e.Err)
					}
				}
				api := goai.ApiOpenAIResponses
				if transport == "azure" {
					api = goai.ApiAzureOpenAIResponses
				}
				processStream(strings.NewReader("data: "+strings.Join(frames, "\n\ndata: ")+"\n\n"), &goai.Model{Api: api}, send)
				if final == nil || len(final.Content) != 2 {
					t.Fatal("missing duplicate reasoning blocks", final)
				}
				if strings.Contains(final.Content[0].ThinkingSignature, "encrypted_content") {
					t.Fatal("backfill resurrected older reasoning", final.Content)
				}
				latest := final.Content[1]
				if mode == "unfinished" {
					if latest.ThinkingSignature != "" {
						t.Fatal("unfinished reasoning fabricated signature", latest)
					}
				} else {
					var item map[string]json.RawMessage
					if err := json.Unmarshal([]byte(latest.ThinkingSignature), &item); err != nil {
						t.Fatal(err)
					}
					if string(item["encrypted_content"]) != `"latest"` || !strings.Contains(string(item["summary"]), "new") {
						t.Fatal("latest reasoning lost identity", latest)
					}
				}
			})
		}
	}
}
