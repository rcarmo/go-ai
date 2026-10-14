package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestTool120ConstraintsPersistAndDetach(t *testing.T) {
	registry := NewRegistry()
	constraint := &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": "start: /.+/"}}
	reg := ToolRegistration{Definition: goai.Tool{Name: "grammar_tool", Parameters: json.RawMessage(`{"type":"object"}`), ConstrainedSampling: constraint}, Implementation: "grammar", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) { return ToolResult{}, nil }}
	if err := registry.Register(reg); err != nil {
		t.Fatal(err)
	}
	constraint.Variants["openai_lark"] = "changed"
	registered, ok := registry.current("grammar_tool")
	if !ok {
		t.Fatal("registration missing")
	}
	offers := []toolOffer{registered.offer}
	tools, err := protocolTools(offers, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].ConstrainedSampling.Variants["openai_lark"] != "start: /.+/" {
		t.Fatal(tools)
	}
	copy := copyProtocolTools(tools)
	copy[0].ConstrainedSampling.Variants["openai_lark"] = "mutated"
	if tools[0].ConstrainedSampling.Variants["openai_lark"] != "start: /.+/" {
		t.Fatal("constraint alias")
	}
	object, err := dtoObject(offers[0], DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	var restored toolOffer
	if err = fromObject(object, &restored, DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if restored.ConstrainedSampling.Variants["openai_lark"] != "start: /.+/" {
		t.Fatal(restored)
	}
}
func TestTool120InvalidConstraintsReject(t *testing.T) {
	for _, value := range []*goai.ToolConstrainedSampling{{Type: "grammar"}, {Type: "grammar", Variants: map[string]string{"unknown": "source"}}, {Type: "grammar", Strict: "require", Variants: map[string]string{"openai_lark": "source"}}, {Type: "json_schema", Strict: "bad"}, {Type: "json_schema", Variants: map[string]string{"openai_lark": "source"}}} {
		if _, err := copyToolConstraints(value, DefaultLimits()); err == nil {
			t.Fatal("invalid constraint", value)
		}
	}
}
