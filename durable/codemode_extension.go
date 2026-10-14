package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
)

// CodemodeSourceGrammar is pinned from pi-codemode 1.2.0 source.ts.
const CodemodeSourceGrammar = `
start: options_source | plain_source
options_source: OPTIONS_LINE NEWLINE SOURCE
plain_source: SOURCE

OPTIONS_LINE: /[ \t]*\/\/ @options:[^\r\n]*/
NEWLINE: /\r?\n/
SOURCE: /[\s\S]+/
`

// NewCodemodeExtension installs a model-only, replay-safe serial candidate.
// The host owns sandbox lifetime. Parallel scripts still reject explicitly.
func NewCodemodeExtension(sandbox CodemodeSandbox) (Extension, error) {
	if sandbox == nil {
		return Extension{}, reject("codemode requires sandbox")
	}
	tool := ToolRegistration{Definition: goai.Tool{Name: "codemode", Description: "Run an async JavaScript function body in an isolated QuickJS sandbox. Use tools, text, image, console, store/load and discovery globals. No ambient filesystem, network or timers. This native candidate currently supports serial calls only.", Parameters: json.RawMessage(`{"type":"object","properties":{"code":{"type":"string"}},"required":["code"],"additionalProperties":false}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": CodemodeSourceGrammar}}}, Implementation: "pi.codemode", Version: 1, ReplaySafe: true, Callers: []string{"model"}, Execute: func(ctx context.Context, args JSON, api *ToolAPI) (ToolResult, error) {
		code, ok := args["code"].(string)
		if !ok {
			return ToolResult{}, reject("codemode source missing")
		}
		result, err := api.runtime.RunCodemode(ctx, "script", code, sandbox)
		if err != nil {
			return ToolResult{}, err
		}
		blocks := []goai.ContentBlock{}
		if result.Output != "" {
			blocks = append(blocks, goai.ContentBlock{Type: "text", Text: result.Output})
		}
		for _, image := range result.Images {
			blocks = append(blocks, goai.ContentBlock{Type: "image", Data: image.Data, MimeType: image.MimeType})
		}
		return ToolResult{Blocks: blocks, IsError: result.Error != "", Details: JSON{"fullOutputPath": result.FullOutputPath}}, nil
	}}
	return Extension{Name: "codemode", Tools: []ToolRegistration{tool}}, nil
}
