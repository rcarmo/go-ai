package openaicodex

import (
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/responsestream"
	"testing"
)

func TestCodexMessagePhaseFinalisationMain(t *testing.T) {
	for _, transport := range []string{"sse", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			frames := []string{`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"m0","phase":"commentary"}}`, `{"type":"response.output_text.delta","output_index":0,"delta":"interim"}`, `{"type":"response.output_item.added","output_index":1,"item":{"type":"message","id":"m1","phase":"final_answer"}}`, `{"type":"response.output_text.delta","output_index":1,"delta":"draft"}`, `{"type":"response.output_item.done","output_index":1,"item":{"type":"message","id":"m1","phase":"final_answer","content":[{"type":"output_text","text":"final"}]}}`, `{"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"m0","phase":"commentary","content":[{"type":"output_text","text":"interim"}]}}`, `{"type":"response.completed","response":{"id":"r","status":"completed","usage":{}}}`}
			var final *goai.Message
			starts, ends := 0, 0
			send := func(event goai.Event) {
				switch e := goai.SnapshotEvent(event).(type) {
				case *goai.TextStartEvent:
					starts++
					if e.ContentIndex == 1 && e.Partial.StopReason != goai.StopReasonStop {
						t.Error("final phase missing in partial")
					}
				case *goai.TextEndEvent:
					ends++
					id, phase := responsestream.ParseTextSignature(e.Partial.Content[e.ContentIndex].TextSignature)
					if id == "" || phase == "" {
						t.Error("end snapshot missing phase signature")
					}
				case *goai.DoneEvent:
					final = e.Message
				case *goai.ErrorEvent:
					t.Error(e.Err)
				}
			}
			runCodexArgumentFrames(t, transport, frames, send)
			model := codexGrammarModel()
			if final == nil || starts != 2 || ends != 2 || final.Content[1].Text != "final" {
				t.Fatal("message finalisation lost", final, starts, ends)
			}
			items := buildCodexAssistantItems(*final, model)
			if len(items) != 2 {
				t.Fatal(items)
			}
			for i, phase := range []string{"commentary", "final_answer"} {
				item := items[i].(map[string]interface{})
				if item["phase"] != phase || item["id"] != []string{"m0", "m1"}[i] {
					t.Fatal("replay lost message phase", item)
				}
			}
		})
	}
}
func TestCodexFinalPhaseNeedsTerminalMain(t *testing.T) {
	done, failures := 0, 0
	send := func(event goai.Event) {
		switch event.(type) {
		case *goai.DoneEvent:
			done++
		case *goai.ErrorEvent:
			failures++
		}
	}
	frames := []string{`{"type":"response.output_item.added","item":{"type":"message","id":"m","phase":"final_answer"}}`, `{"type":"response.output_item.done","item":{"type":"message","id":"m","phase":"final_answer","content":[]}}`}
	if err := streamCodexArgumentFrames(t, "sse", frames, send); err != nil {
		t.Fatal(err)
	}
	if done != 0 || failures != 1 {
		t.Fatal("phase bypassed terminal guard", done, failures)
	}
}
