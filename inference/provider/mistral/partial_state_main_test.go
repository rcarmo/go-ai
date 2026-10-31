package mistral

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

type mistralPartialSnapshot struct {
	Text         string
	Reason       goai.StopReason
	Raw, Message string
}

// Source-derived: pinned consumeChatStream applies finish reasons before
// content events, and a later successful finish does not clear an earlier diagnostic.
func TestMistralPartialStateSourceMain(t *testing.T) {
	raw, err := os.ReadFile("testdata/partial_state_main.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name      string
		Events    []json.RawMessage
		Snapshots []mistralPartialSnapshot
		Reason    goai.StopReason
		Message   string
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			model := upstreamMistralHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				events := make([]any, len(tc.Events))
				for i, event := range tc.Events {
					events[i] = event
				}
				upstreamMistralWrite(w, events...)
			})
			var snapshots []mistralPartialSnapshot
			var terminal *goai.Message
			done, failed := 0, 0
			for event := range streamMistral(context.Background(), model, makeMistralContext(), &goai.StreamOptions{APIKey: "test"}) {
				switch e := event.(type) {
				case *goai.TextDeltaEvent:
					p := e.Partial
					snapshots = append(snapshots, mistralPartialSnapshot{goai.GetTextContent(p), p.StopReason, p.RawStopReason, p.ErrorMessage})
				case *goai.DoneEvent:
					done++
					terminal = e.Message
				case *goai.ErrorEvent:
					failed++
					terminal = e.Error
				}
			}
			if !reflect.DeepEqual(snapshots, tc.Snapshots) {
				t.Errorf("snapshots %#v want %#v", snapshots, tc.Snapshots)
			}
			if terminal == nil || terminal.StopReason != tc.Reason || terminal.ErrorMessage != tc.Message {
				t.Errorf("terminal %#v want %s/%q", terminal, tc.Reason, tc.Message)
			}
			if tc.Reason == goai.StopReasonError {
				if failed != 1 || done != 0 {
					t.Fatal("terminal counts", failed, done)
				}
			} else if failed != 0 || done != 1 {
				t.Fatal("terminal counts", failed, done)
			}
		})
	}
}
