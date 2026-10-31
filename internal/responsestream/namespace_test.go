package responsestream

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestNamespacePresenceAndCompatibility(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{{`{}`, "old"}, {`{"namespace":""}`, ""}, {`{"namespace":"new"}`, "new"}, {`{"namespace":false}`, "old"}, {`{"namespace":null}`, "old"}, {`{`, "old"}} {
		block := goai.ContentBlock{Namespace: "old", NamespacePresent: true}
		ApplyToolNamespace([]byte(tc.raw), &block)
		if block.Namespace != tc.want || !block.NamespacePresent {
			t.Fatal(tc.raw, block.Namespace, block.NamespacePresent)
		}
	}
	model := &goai.Model{ID: "m", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses}
	source := goai.Message{Model: model.ID, Provider: model.Provider, Api: model.Api}
	for _, block := range []goai.ContentBlock{{Namespace: "legacy"}, {NamespacePresent: true}} {
		value, present := ReplayToolNamespace(block, source, model)
		if !present || value != block.Namespace {
			t.Fatal("native manually constructed namespace lost", value, present)
		}
	}
	if _, present := ReplayToolNamespace(goai.ContentBlock{}, source, model); present {
		t.Fatal("absent namespace replayed")
	}
}
