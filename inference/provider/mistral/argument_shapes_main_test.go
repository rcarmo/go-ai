package mistral

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

func TestMistralArgumentShapesMain(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want       string
		bad, unicode, omitted bool
	}{
		{name: "object", raw: `{"n":7,"s":"value"}`, want: `{"n":7,"s":"value"}`},
		{name: "string", raw: `"{\"s\":\"value\"}"`, want: `{"s":"value"}`},
		{name: "empty-object", raw: `{}`, want: `{}`},
		{name: "null", raw: `null`, want: `{}`},
		{name: "false", raw: `false`, want: `{}`},
		{name: "zero", raw: `0`, want: `{}`},
		{name: "negative-zero", raw: `-0`, want: `{}`},
		{name: "true", raw: `true`, bad: true},
		{name: "number", raw: `13`, bad: true},
		{name: "array", raw: `[]`, bad: true},
		{name: "rounded-integer", raw: `{"n":9007199254740993}`, want: `{"n":9007199254740992}`},
		{name: "nonfinite-member", raw: `{"n":1e400}`, want: `{"n":null}`},
		{name: "nonfinite-root", raw: `1e400`, bad: true},
		{name: "utf16-key-value", raw: `{"\udfff":"\ud800"}`, unicode: true},
		{name: "empty-string", raw: `""`, want: `{}`},
		{name: "omitted", want: `{}`, omitted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			property := `,"arguments":` + tc.raw
			if tc.omitted {
				property = ""
			}
			wire := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"one","function":{"name":"sample"` + property + `}}]},"finish_reason":null}]}` + "\n\n" + `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n"
			var done *goai.Message
			deltas := 0
			processSSEStream(strings.NewReader(wire), &goai.Model{ID: "test", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}, func(event goai.Event) {
				switch event := event.(type) {
				case *goai.ToolCallDeltaEvent:
					deltas++
					mistralSnapshotMain(t, event.Partial)
					if tc.name == "empty-string" && event.Delta != "" {
						t.Fatal("empty fragment changed", event.Delta)
					}
				case *goai.DoneEvent:
					done = event.Message
				case *goai.ErrorEvent:
					t.Fatal(event.Err)
				}
			})
			wantDeltas := 1
			if tc.omitted {
				wantDeltas = 0
			}
			if done == nil || len(done.Content) != 1 || deltas != wantDeltas {
				t.Fatal("chunk dropped or lifecycle changed", done, deltas)
			}
			mistralSnapshotMain(t, done)
			block := done.Content[0]
			if tc.bad {
				if block.ArgumentsError == "" || len(block.Arguments) != 0 {
					t.Fatal("non-object accepted", block)
				}
				return
			}
			if block.ArgumentsError != "" {
				t.Fatal("valid shape rejected", block)
			}
			if tc.unicode {
				if len(block.Arguments) != 1 {
					t.Fatal(block.Arguments)
				}
				for key, value := range block.Arguments {
					units, err := jsonparse.StringCodeUnits(key)
					text, ok := value.(goai.UTF16String)
					if err != nil || !reflect.DeepEqual(units, []uint16{0xdfff}) || !ok || !reflect.DeepEqual(text.CodeUnits(), []uint16{0xd800}) {
						t.Fatal("UTF16 changed", units, value, err)
					}
				}
				return
			}
			data, err := json.Marshal(block.Arguments)
			if err != nil {
				t.Fatal(err)
			}
			var got, want any
			if err = json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatal("semantic argument mismatch", got, want)
			}
		})
	}
}
