package google

// Source-derived production-path cases, referenced against the locked SDK's
// actual parser. SDK object transformations/reference identity are not modelled.
import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestMainSourceGoogleSDKFraming(t *testing.T) {
	var fixture struct {
		Upstream, SDK string
		Cases         []struct {
			Name, Wire, Failure, Terminal string
			Chunks                        []any
		}
	}
	raw, err := os.ReadFile("testdata/sdk_framing_main.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Upstream != "81f3cd412758ebdd92324c7d63b669a189408d0a" || fixture.SDK != "@google/genai@2.21.0" || len(fixture.Cases) != 18 {
		t.Fatal("unexpected reference provenance/corpus")
	}
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		for _, tc := range fixture.Cases {
			t.Run(string(api)+"/"+tc.Name, func(t *testing.T) {
				var received []any
				message, _, events := captureMainGoogleWireEvents(t, api, tc.Wire, func(opts *goai.StreamOptions, _ *goai.Model) {
					opts.OnProviderStreamEvent = func(value any, _ *goai.Model) error { received = append(received, value); return nil }
				})
				if !reflect.DeepEqual(received, tc.Chunks) {
					t.Errorf("observer count=%d want %d, decoded chunks differ", len(received), len(tc.Chunks))
				}
				if message.ResponseID != "first" || len(message.Content) != 1 || message.Content[0].Text != "helloπ🙂" {
					t.Errorf("partial/id lost: id=%s content=%#v", message.ResponseID, message.Content)
				}
				failures, done := 0, 0
				for _, event := range events {
					switch e := event.(type) {
					case *goai.ErrorEvent:
						failures++
						if e.Err == nil {
							t.Error("missing failure diagnostic")
						}
					case *goai.DoneEvent:
						done++
					}
				}
				if tc.Failure != "" || tc.Terminal != "STOP" {
					if failures != 1 || done != 0 || message.StopReason != goai.StopReasonError || message.ErrorMessage == "" {
						t.Errorf("errors=%d done=%d reason=%s diagnostic=%q", failures, done, message.StopReason, message.ErrorMessage)
					}
					if tc.Failure == "incomplete" && message.ErrorMessage != "Incomplete JSON segment at the end" {
						t.Errorf("incomplete diagnostic %q", message.ErrorMessage)
					}
				} else if failures != 0 || done != 1 || message.StopReason != goai.StopReasonStop {
					t.Errorf("errors=%d done=%d reason=%s", failures, done, message.StopReason)
				}
			})
		}
	}
}
