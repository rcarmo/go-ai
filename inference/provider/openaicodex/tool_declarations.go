package openaicodex

import (
	goai "github.com/rcarmo/go-ai"
	"strings"
)

func convertCodexToolDeclaration(model *goai.Model, tool goai.Tool) (codexTool, error) {
	if codexSupportsGrammar(model) && tool.ConstrainedSampling != nil && tool.ConstrainedSampling.Type == "grammar" {
		definition, syntax := tool.ConstrainedSampling.Variants["openai_lark"], "lark"
		if strings.TrimSpace(definition) == "" {
			definition = tool.ConstrainedSampling.Variants["openai_regex"]
			syntax = "regex"
		}
		if strings.TrimSpace(definition) != "" {
			return codexTool{Type: "custom", Name: tool.Name, Description: tool.Description, Format: map[string]interface{}{"type": "grammar", "syntax": syntax, "definition": definition}}, nil
		}
	}
	supportsStrict := model.ResponsesCompat == nil || model.ResponsesCompat.SupportsStrictMode == nil || *model.ResponsesCompat.SupportsStrictMode
	strict, err := goai.ResolveJSONSchemaStrictSampling(tool, supportsStrict)
	if err != nil {
		return codexTool{}, err
	}
	parameters := tool.Parameters
	if strict != nil && *strict {
		parameters, err = goai.JSONSchemaToolParameters(tool, true)
		if err != nil {
			return codexTool{}, err
		}
	}
	var strictValue *bool
	if supportsStrict {
		value := strict != nil && *strict
		strictValue = &value
	}
	return codexTool{Type: "function", Name: tool.Name, Description: tool.Description, Parameters: parameters, Strict: strictValue}, nil
}

// Only declarations actually projected onto this request are sampled/validated.
// Old definitions folded out of a snapshot must not reject a later valid one.
func validateCodexRequestToolDeclarations(model *goai.Model, input *goai.Context) error {
	projected := codexTranscriptContext(model, input)
	declarations := append([]goai.Tool(nil), projected.Tools...)
	if codexAnchorsTools(model) {
		declarations = nil
	}
	if codexAnchorsTools(model) || codexUsesToolSearch(model) {
		folded := 0
		if codexUsesToolSearch(model) {
			_, folded = codexToolSearchPlan(projected.Messages)
		}
		system := 0
		for _, message := range projected.Messages {
			if message.Role != goai.RoleSystem {
				continue
			}
			if system >= folded {
				declarations = append(declarations, message.ToolsAdded...)
			}
			system++
		}
	}
	for _, tool := range declarations {
		if _, err := convertCodexToolDeclaration(model, tool); err != nil {
			return err
		}
	}
	return nil
}
