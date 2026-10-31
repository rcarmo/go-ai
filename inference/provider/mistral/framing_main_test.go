package mistral

import (
	"io"
	"strings"
	"testing"
	"testing/iotest"

	goai "github.com/rcarmo/go-ai"
)

func TestMistralFramingSourceMain(t *testing.T) {
	for _, boundary := range []string{"\n\n", "\r\r", "\r\n\r\n", "\r\n\r", "\r\n\n", "\n\r\n", "\n\r", "\r\r\n"} {
		t.Run(strings.NewReplacer("\r", "CR", "\n", "LF").Replace(boundary), func(t *testing.T) {
			wire := "\ufeffdata: {\"choices\":\r\ndata: [{\"delta\":{\"content\":\"hé😃\"}}]}" + boundary + "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}" + boundary
			text := ""
			done, failed := 0, 0
			processSSEStream(iotest.OneByteReader(strings.NewReader(wire)), &goai.Model{}, func(event goai.Event) {
				switch e := event.(type) {
				case *goai.TextDeltaEvent:
					text += e.Delta
				case *goai.DoneEvent:
					done++
				case *goai.ErrorEvent:
					failed++
					t.Errorf("framing error %v", e.Err)
				}
			})
			if text != "hé😃" || done != 1 || failed != 0 {
				t.Fatal("framing result", text, done, failed)
			}
		})
	}
}

func TestMistralReadFailureDiscardsUncommittedFrame(t *testing.T) {
	wire := "data: {\"choices\":[{\"delta\":{\"content\":\"uncommitted\"},\"finish_reason\":\"stop\"}]}\n"
	failed, done, deltas := 0, 0, 0
	body := io.MultiReader(strings.NewReader(wire), iotest.ErrReader(io.ErrUnexpectedEOF))
	processSSEStream(body, &goai.Model{}, func(event goai.Event) {
		switch e := event.(type) {
		case *goai.TextDeltaEvent:
			deltas++
		case *goai.DoneEvent:
			done++
		case *goai.ErrorEvent:
			failed++
			if len(e.Error.Content) != 0 {
				t.Fatal("incomplete frame became visible", e.Error.Content)
			}
		}
	})
	if failed != 1 || done != 0 || deltas != 0 {
		t.Fatal("read failure result", failed, done, deltas)
	}
}
