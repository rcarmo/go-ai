package durable

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"unicode/utf8"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

type callerStreamingMain struct{ goai.ToolCall }

func (callerStreamingMain) MarshalJSON() ([]byte, error) { panic("caller marshaler invoked") }

type inheritedStreamingMain struct{ goai.ContentBlock }
type inheritedUTF16Main struct{ goai.UTF16String }

func TestStreamingArgumentsMainDurableReplay(t *testing.T) {
	raw := []byte(`{"role":"assistant","content":[{"type":"toolCall","id":"c","name":"f","arguments":{"\ud800":"\udfff","n":9007199254740993},"argumentsError":"bad \ud800"}]}`)
	var message goai.Message
	if err := decodeStrict(raw, DefaultLimits(), 4096, &message); err != nil {
		t.Fatal(err)
	}
	block := message.Content[0]
	if !block.HasStreamingArguments() {
		t.Fatal("replay lost native argument mode")
	}
	if value, ok := block.Arguments["n"].(json.Number); !ok || value.String() != "9007199254740993" {
		t.Fatal("replay rounded exact integer")
	}
	var scalar goai.UTF16String
	found := false
	for key, value := range block.Arguments {
		units, err := jsonparse.StringCodeUnits(key)
		if err != nil {
			t.Fatal(err)
		}
		if reflect.DeepEqual(units, []uint16{0xd800}) {
			var ok bool
			scalar, ok = value.(goai.UTF16String)
			if !ok || !reflect.DeepEqual(scalar.CodeUnits(), []uint16{0xdfff}) {
				t.Fatal("replay value units lost")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("replay key units lost")
	}
	if _, err := json.Marshal(message); err != nil {
		t.Fatal("SDK cannot serialise durable numeric arguments", err)
	}
	wire, err := encodeBounded(message, DefaultLimits(), 4096)
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.Valid(wire) {
		t.Fatal("wire is not UTF-8")
	}
	var again goai.Message
	if err = decodeStrict(wire, DefaultLimits(), 4096, &again); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(message, again) {
		t.Fatal("typed replay changed message")
	}
	// Hidden metadata does not inflate old native struct member budgets.
	tool := goai.ToolCall{Type: "toolCall"}
	tool.SetStreamingArguments(map[string]any{}, "")
	l := DefaultLimits()
	l.MaxMembers = 9
	if _, err = encodeBounded(tool, l, 4096); err != nil {
		t.Fatal("hidden mode changed prior tool member budget", err)
	}
	// Native strings remain one bounded scalar, with UTF-8 replacement-byte cost.
	l = DefaultLimits()
	l.MaxStringBytes = 2
	if _, err = encodeBounded(scalar, l, 64); err == nil {
		t.Fatal("UTF-16 string cap bypass")
	}
	l = DefaultLimits()
	l.MaxNodes = 1
	budget := jsonBudget{l: l, utf16Text: true}
	if err = budget.walk(reflect.ValueOf(scalar), 1, false); err != nil {
		t.Fatal("native string counted as struct", err)
	}
	output := limitedJSON{max: 8}
	if err = encodeUTF16Value(&output, reflect.ValueOf(scalar)); err != nil {
		t.Fatal(err)
	}
	output = limitedJSON{max: 7}
	if err = encodeUTF16Value(&output, reflect.ValueOf(scalar)); err == nil {
		t.Fatal("escaped byte cap bypass")
	}
}

func TestStreamingArgumentsMainBoundaryIsolation(t *testing.T) {
	parser := jsonparse.StreamingJSONParser{}
	parser.Append(`{"\ud800":"\udfff"}`)
	value, _ := parser.Snapshot()
	args := value.(map[string]any)
	tool := goai.ToolCall{Type: "toolCall"}
	tool.SetStreamingArguments(args, "")
	if _, err := encodeBounded(tool, DefaultLimits(), 4096); err != nil {
		t.Fatal(err)
	}
	if _, err := encodeBounded(args, DefaultLimits(), 4096); err == nil {
		t.Fatal("ordinary map inherited UTF-16 key exemption")
	}
	object := struct {
		Call    goai.ToolCall `json:"call"`
		Control JSON          `json:"control"`
	}{Call: tool, Control: JSON{string([]byte{0xed, 0xa0, 0x80}): true}}
	if _, err := encodeBounded(object, DefaultLimits(), 4096); err == nil {
		t.Fatal("argument scope leaked into control keys")
	}
	tool.SetStreamingArguments(map[string]any{"n": math.Inf(1)}, "")
	if _, err := encodeBounded(tool, DefaultLimits(), 4096); err == nil {
		t.Fatal("native mode relaxed finite-number check")
	}
	for _, value := range []any{callerStreamingMain{}, &callerStreamingMain{}, inheritedStreamingMain{}, inheritedUTF16Main{}} {
		if _, err := encodeBounded(value, DefaultLimits(), 4096); err == nil {
			t.Fatal("caller wrapper inherited native privilege")
		}
	}
	var message goai.Message
	if err := decodeStrict([]byte(`{"content":[{"type":"toolCall","arguments":{},"unexpected":1}]}`), DefaultLimits(), 4096, &message); err == nil {
		t.Fatal("native bridge hid unknown fields")
	}
}

func TestStreamingArgumentsMainScopedScanner(t *testing.T) {
	for _, raw := range []string{
		`{"content":[{"type":"toolCall","id":"\ud800","arguments":{}}]}`,
		`{"content":[{"type":"text","arguments":{"x":"\ud800"}}]}`,
		`{"content":[{"type":"toolCall","arguments":{"\ud800":1,"\uD800":2}}]}`,
		`{"content":[{"type":"toolCall","arguments":{"x":"\ud800\uQQQQ"}}]}`,
	} {
		var message goai.Message
		if err := decodeStrict([]byte(raw), DefaultLimits(), 4096, &message); err == nil {
			t.Fatal("out-of-scope/invalid Unicode accepted", raw)
		}
	}
	var opaque JSON
	if err := decodeStrict([]byte(`{"arguments":{"x":"\ud800"}}`), DefaultLimits(), 4096, &opaque); err == nil {
		t.Fatal("opaque JSON inherited typed argument scope")
	}
	var message goai.Message
	if err := decodeStrict([]byte(`{"content":[{"type":"toolCall","arguments":{"\ud800":1,"\ud801":2,"text":"\ud800\u1234"}}]}`), DefaultLimits(), 4096, &message); err != nil {
		t.Fatal("distinct UTF-16 keys collapsed", err)
	}
	if len(message.Content[0].Arguments) != 3 {
		t.Fatal("key lost")
	}
}

func FuzzStreamingArgumentsMainDurableReplay(f *testing.F) {
	for _, raw := range []string{`{"content":[{"type":"toolCall","arguments":{"\ud800":"\udfff","n":9007199254740993}}]}`, `{"content":[{"type":"toolCall","arguments":{}}]}`, `{"content":[{"type":"toolCall","argumentsError":"bad \ud800"}]}`} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 4096 {
			return
		}
		limits := DefaultLimits()
		limits.MaxDepth = 32
		limits.MaxMembers = 128
		limits.MaxNodes = 512
		limits.MaxStringBytes = 1024
		var first goai.Message
		if decodeStrict([]byte(raw), limits, 4096, &first) != nil {
			return
		}
		wire, err := encodeBounded(first, limits, 4096)
		if err != nil {
			return
		} // DTO defaults/presence can exceed a deliberately small budget.
		var next goai.Message
		if err = decodeStrict(wire, limits, 4096, &next); err != nil {
			t.Fatal("encoded message not replayable", err)
		}
		if len(first.Content) != len(next.Content) {
			t.Fatal("content count changed")
		}
		for i, block := range first.Content {
			if block.Type != "toolCall" {
				continue
			}
			after := next.Content[i]
			if !reflect.DeepEqual(block.Arguments, after.Arguments) || block.ArgumentsError != after.ArgumentsError || block.HasStreamingArguments() != after.HasStreamingArguments() {
				t.Fatal("tool arguments changed on replay")
			}
		}

	})
}

type StreamingRecursiveMain struct {
	*StreamingRecursiveMain
	Message goai.Message `json:"message"`
}

func TestStreamingArgumentsMainSchemaCycles(t *testing.T) {
	raw := []byte(`{"message":{"content":[{"type":"toolCall","arguments":{"x":"\ud800"}}]}}`)
	ranges, err := streamingArgumentRanges(raw, reflect.TypeFor[StreamingRecursiveMain](), DefaultLimits())
	if err != nil || len(ranges) != 1 {
		t.Fatal("recursive schema annotation failed", err, len(ranges))
	}
	if _, err = streamingArgumentRanges(raw, nil, DefaultLimits()); err == nil {
		t.Fatal("nil schema accepted")
	}
}

func TestStreamingArgumentsMainLegacyNonToolOmission(t *testing.T) {
	var message goai.Message
	if err := decodeStrict([]byte(`{"Content":[{"Arguments":{}}]}`), DefaultLimits(), 4096, &message); err != nil {
		t.Fatal(err)
	}
	if message.Content[0].HasStreamingArguments() {
		t.Fatal("non-tool block acquired native argument mode")
	}
	wire, err := encodeBounded(message, DefaultLimits(), 4096)
	if err != nil {
		t.Fatal(err)
	}
	var next goai.Message
	if err = decodeStrict(wire, DefaultLimits(), 4096, &next); err != nil {
		t.Fatal(err)
	}
	if next.Content[0].Arguments != nil {
		t.Fatal("legacy omitempty behaviour changed")
	}
}
