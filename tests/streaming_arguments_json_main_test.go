package goai_test

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

func TestStreamingArgumentsMainSDKLosslessBoundary(t *testing.T) {
	raw := []byte(`{"role":"assistant","content":[{"type":"toolCall","id":"c","name":"f","arguments":{"\ud800":"\udfff","n":9007199254740993},"argumentsError":"bad \ud800"}],"timestamp":1}`)
	message, err := goai.UnmarshalStreamingMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	block := message.Content[0]
	if !block.HasStreamingArguments() {
		t.Fatal("missing explicit argument mode")
	}
	var found bool
	for key, value := range block.Arguments {
		units, err := jsonparse.StringCodeUnits(key)
		if err != nil {
			t.Fatal(err)
		}
		if reflect.DeepEqual(units, []uint16{0xd800}) {
			special, ok := value.(goai.UTF16String)
			if !ok || !reflect.DeepEqual(special.CodeUnits(), []uint16{0xdfff}) {
				t.Fatal("value units lost")
			}
			found = true
		}
	}
	if !found || block.Arguments["n"] != float64(9007199254740992) {
		t.Fatal("argument key or JS number lost")
	}
	wire, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	again, err := goai.UnmarshalStreamingMessage(wire)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(message.Content[0].Arguments, again.Content[0].Arguments) || message.Content[0].ArgumentsError != again.Content[0].ArgumentsError {
		t.Fatal("SDK round-trip changed arguments")
	}
	var ordinary goai.Message
	if err = json.Unmarshal(raw, &ordinary); err != nil {
		t.Fatal(err)
	}
	if ordinary.Content[0].HasStreamingArguments() {
		t.Fatal("ordinary Go decoder behaviour changed")
	}
}

func TestStreamingArgumentsMainSDKOverflowAndIsolation(t *testing.T) {
	message, err := goai.UnmarshalStreamingMessage([]byte(`{"role":"assistant","content":[{"type":"toolCall","arguments":{"n":1e400}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !math.IsInf(message.Content[0].Arguments["n"].(float64), 1) {
		t.Fatal("preview overflow lost")
	}
	if _, err = json.Marshal(message); err != nil {
		t.Fatal("explicit SDK JS serialisation failed", err)
	}
	stream := jsonparse.ToolCallArgumentsStream{}
	args := stream.Append(`{"value":"\ud800"}`)
	block := goai.ContentBlock{Type: "toolCall"}
	block.SetStreamingArguments(args, "")
	copy := goai.CloneContext(&goai.Context{Messages: []goai.Message{{Role: "assistant", Content: []goai.ContentBlock{block}}}})
	args["value"] = "changed"
	cloned := copy.Messages[0].Content[0]
	if !cloned.HasStreamingArguments() {
		t.Fatal("clone lost native mode")
	}
	if _, ok := cloned.Arguments["value"].(goai.UTF16String); !ok {
		t.Fatal("clone shared mutable arguments")
	}
	for _, data := range [][]byte{[]byte(`{} {}`), {0xff}} {
		if _, err := goai.UnmarshalStreamingMessage(data); err == nil {
			t.Fatal("invalid SDK JSON accepted")
		}
	}
}

func TestStreamingArgumentsMainPresenceFlags(t *testing.T) {
	value := goai.ContentBlock{Type: "toolCall", TextSignaturePresent: true, ThinkingSignaturePresent: true, ThoughtSignaturePresent: true, NamespacePresent: true, RedactedPresent: true}
	value.SetStreamingArguments(nil, "")
	wire, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err = json.Unmarshal(wire, &object); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"arguments", "textSignature", "thinkingSignature", "thoughtSignature", "namespace", "redacted"} {
		if _, ok := object[key]; !ok {
			t.Fatal("explicit presence lost", key)
		}
	}
	if object["redacted"] != false || len(object["arguments"].(map[string]any)) != 0 {
		t.Fatal("zero values changed")
	}
}
