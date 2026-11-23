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
	reg := ToolRegistration{Definition: goai.Tool{Name: "grammar_tool", Parameters: json.RawMessage(`{"type":"object"}`), ConstrainedSampling: constraint}, Implementation: "grammar", Version: 1, Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) { return ToolResult{}, nil }}
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
func TestSourceV160ToolDisabledConstraintSurvivesRegistry(t *testing.T) {
	var tool goai.Tool
	if err := json.Unmarshal([]byte(`{"name":"plain","parameters":{"type":"object"},"constrainedSampling":false}`), &tool); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if err := registry.Register(ToolRegistration{Definition: tool, Implementation: "plain.native", Version: 1, Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) { return ToolResult{}, nil }}); err != nil {
		t.Fatal(err)
	}
	registered, found := registry.current("plain")
	if !found {
		t.Fatal("registered tool missing")
	}
	object, err := dtoObject(registered.offer, DefaultLimits())
	if err != nil || object["constrainedSampling"] != false {
		t.Fatalf("stored offer lost disabled sampling: %+v, err=%v", object, err)
	}
	var restored toolOffer
	if err := fromObject(object, &restored, DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	tools, err := protocolTools([]toolOffer{restored}, DefaultLimits())
	if err != nil || len(tools) != 1 {
		t.Fatalf("replayed tools: %+v, err=%v", tools, err)
	}
	encoded, err := json.Marshal(tools[0].ConstrainedSampling)
	if err != nil || string(encoded) != "false" {
		t.Fatalf("replayed sampling: %s, err=%v; want false", encoded, err)
	}
	if _, err := ownJSONValue(JSON{"sampling": tool.ConstrainedSampling}, DefaultLimits()); err == nil {
		t.Fatal("typed tool constraint gained marshal authority in caller JSON")
	}
}

func TestTool120InvalidConstraintsReject(t *testing.T) {
	for _, value := range []*goai.ToolConstrainedSampling{{Type: goai.ToolConstrainedSamplingDisabled, Strict: "require"}, {Type: "grammar"}, {Type: "grammar", Variants: map[string]string{"unknown": "source"}}, {Type: "grammar", Strict: "require", Variants: map[string]string{"openai_lark": "source"}}, {Type: "json_schema", Strict: "bad"}, {Type: "json_schema", Variants: map[string]string{"openai_lark": "source"}}} {
		if _, err := copyToolConstraints(value, DefaultLimits()); err == nil {
			t.Fatal("invalid constraint", value)
		}
	}
}
