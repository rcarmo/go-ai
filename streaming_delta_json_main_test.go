package goai

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/rcarmo/go-ai/internal/jsonparse"
)

func TestToolCallDeltaJSONMainCodeUnits(t *testing.T) {
	var delta jsonparse.ArgumentText
	if err := json.Unmarshal([]byte(`"prefix\ud800"`), &delta); err != nil {
		t.Fatal(err)
	}
	scalar, err := jsonparse.ParseJSONValue(`"\udfff"`, true)
	if err != nil {
		t.Fatal(err)
	}
	block := ContentBlock{Type: "toolCall", ID: "c", Name: "leaf"}
	block.SetStreamingArguments(map[string]any{"low": scalar}, "")
	event := ToolCallDeltaEvent{ContentIndex: 2, Delta: string(delta), Partial: &Message{Role: RoleAssistant, Content: []ContentBlock{block}}}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var restored ToolCallDeltaEvent
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	units, err := jsonparse.StringCodeUnits(restored.Delta)
	if err != nil || !reflect.DeepEqual(units, []uint16{'p', 'r', 'e', 'f', 'i', 'x', 0xd800}) {
		t.Fatal("delta units lost", units, err)
	}
	got, ok := restored.Partial.Content[0].Arguments["low"].(UTF16String)
	if !ok || !reflect.DeepEqual(got.CodeUnits(), []uint16{0xdfff}) {
		t.Fatal("partial argument units lost", restored.Partial)
	}
	for _, bad := range []string{`{"Delta":42}`, `{"Delta":{}}`, `{"Delta":[1]}`} {
		if err = json.Unmarshal([]byte(bad), &restored); err == nil {
			t.Fatal("nonstring delta accepted")
		}
	}
	prior := restored.Partial
	if err = json.Unmarshal([]byte(`{"Delta":null}`), &restored); err != nil || restored.ContentIndex != 2 || restored.Partial != prior || restored.Delta != string(delta) {
		t.Fatal("omitted/null fields changed existing event", err)
	}
	if _, err = json.Marshal(ToolCallDeltaEvent{Delta: string([]byte{255})}); err == nil {
		t.Fatal("invalid non-WTF-8 bytes accepted")
	}
}
