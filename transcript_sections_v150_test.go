package goai

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Pinned pi-ai utils/text.ts and utils/transcript.ts: section values are already
// rendered by their owner. Transcript collapse does not add tags around them.
func TestTranscript150ReplaysRenderedSectionsWithoutWrapping(t *testing.T) {
	for _, value := range []string{"plain", "<policy>\nrendered\n</policy>", ""} {
		messages := []Message{{Role: RoleSystem, Content: []ContentBlock{{Type: "text", Text: "base"}}, Sections: map[string]*string{"policy": &value}}}
		want := "base"
		if value != "" {
			want += "\n\n" + value
		}
		got := ResolveContext(&Context{Messages: messages}, false)
		if got.SystemPrompt != want {
			t.Fatalf("section %q: got %q, want %q", value, got.SystemPrompt, want)
		}
	}
}

func TestTranscript150RemovalReaddPreservesRenderedValueOrder(t *testing.T) {
	first, second, kept := "first", "second", "kept"
	messages := []Message{{Role: RoleSystem, Sections: map[string]*string{"a": &first}}, {Role: RoleSystem, Sections: map[string]*string{"b": &kept}}, {Role: RoleSystem, Sections: map[string]*string{"a": nil}}, {Role: RoleSystem, Sections: map[string]*string{"a": &second}}}
	got := ResolveContext(&Context{Messages: messages}, false)
	if got.SystemPrompt != "kept\n\nsecond" {
		t.Fatal(got.SystemPrompt)
	}
}

func TestTranscript150SectionPropertyOrderSurvivesWireRoundtrip(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"role":"system","content":[],"sections":{"b":"B","a":"A"},"timestamp":0}`), &message); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if !reflect.DeepEqual(message.SectionOrder, []string{"b", "a"}) {
			t.Fatal("source property order", message.SectionOrder)
		}
		if got := ResolveContext(&Context{Messages: []Message{message}}, false).SystemPrompt; got != "B\n\nA" {
			t.Fatal(got)
		}
		wire, err := json.Marshal(message)
		if err != nil || strings.Contains(string(wire), "sectionOrder") {
			t.Fatal("representation metadata leaked", string(wire), err)
		}
		if err = json.Unmarshal(wire, &message); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTranscript150NumericSectionKeysUseJavaScriptPropertyOrder(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"role":"system","content":[],"sections":{"10":"ten","2":"two","01":"leading","a":"A"},"timestamp":0}`), &message); err != nil {
		t.Fatal(err)
	}
	if got := GetSystemMessageText(message); got != "two\n\nten\n\nleading\n\nA" {
		t.Fatal(got)
	}
	before := "before"
	if got := ResolveContext(&Context{Messages: []Message{{Role: RoleSystem, Sections: map[string]*string{"z": &before}}, message}}, false).SystemPrompt; got != "two\n\nten\n\nbefore\n\nleading\n\nA" {
		t.Fatal("numeric keys across patches", got)
	}
}

func TestTranscript150SystemContentPreservesTextSeparators(t *testing.T) {
	for _, tc := range []struct {
		blocks []ContentBlock
		want   string
	}{
		{[]ContentBlock{{Type: "text", Text: "  "}}, "  "},
		{[]ContentBlock{{Type: "text", Text: ""}, {Type: "text", Text: "x"}, {Type: "text", Text: ""}}, "\nx\n"},
		{[]ContentBlock{{Type: "thinking", Thinking: "hidden"}, {Type: "text", Text: "x"}}, "x"},
	} {
		got := ResolveContext(&Context{Messages: []Message{{Role: RoleSystem, Content: tc.blocks}}}, false)
		if got.SystemPrompt != tc.want {
			t.Fatalf("got %q, want %q", got.SystemPrompt, tc.want)
		}
	}
}
