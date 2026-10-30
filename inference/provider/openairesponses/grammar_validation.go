package openairesponses

import (
	"fmt"
	goai "github.com/rcarmo/go-ai"
)

func responsesGrammarInput(block goai.ContentBlock, property string) (string, bool) {
	switch value := block.Arguments[property].(type) {
	case string:
		return value, true
	case goai.UTF16String:
		if block.HasStreamingArguments() {
			return value.WTF8(), true
		}
	}
	return "", false
}

func validateResponsesGrammarContext(context *goai.Context, supportsGrammar bool) error {
	if !supportsGrammar || context == nil {
		return nil
	}
	properties := map[string]string{}
	for _, tool := range responsesTranscriptTools(context) {
		grammar, err := resolveResponsesGrammarTool(tool, true)
		if err != nil {
			return err
		}
		if grammar != nil {
			properties[tool.Name] = grammar.inputProperty
		}
	}
	for _, message := range context.Messages {
		if message.Role != goai.RoleAssistant {
			continue
		}
		for _, block := range message.Content {
			if block.Type != "toolCall" {
				continue
			}
			if property, selected := properties[block.Name]; selected {
				if _, ok := responsesGrammarInput(block, property); !ok {
					return fmt.Errorf("grammar tool call %q requires string argument %q", block.Name, property)
				}
			}
		}
	}
	return nil
}
