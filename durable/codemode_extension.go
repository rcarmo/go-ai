package durable

import (
	"context"
	"encoding/json"
	"fmt"
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

// CodemodeExtensionOptions controls model-facing declaration rendering.
// Nil InlineBudget uses 3000 estimated tokens; explicit zero lists no tools.
// Execution authority, namespaces and globals remain harness-scoped.
type CodemodeExtensionOptions struct {
	InlineBudget *int
}

// NewCodemodeExtension installs a model-only, replay-safe serial candidate.
// The host owns sandbox lifetime. Parallel scripts still reject explicitly.
func NewCodemodeExtension(sandbox CodemodeSandbox, options ...CodemodeExtensionOptions) (Extension, error) {
	if len(options) > 1 {
		return Extension{}, reject("multiple codemode extension options")
	}
	budget := DefaultCodemodeInlineBudget
	if len(options) == 1 && options[0].InlineBudget != nil {
		budget = *options[0].InlineBudget
	}
	if budget < 0 {
		return Extension{}, reject("negative codemode inline budget")
	}
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
		status := "completed"
		if result.Error != "" {
			status = "failed"
		}
		text := fmt.Sprintf("Script %s\nWall time %.1f seconds\nOutput:\n%s", status, float64(result.WallMs)/1000, result.Output)
		blocks := []goai.ContentBlock{{Type: "text", Text: text}}
		for _, image := range result.Images {
			blocks = append(blocks, goai.ContentBlock{Type: "image", Data: image.Data, MimeType: image.MimeType})
		}
		return ToolResult{Blocks: blocks, IsError: result.Error != "", Details: JSON{"fullOutputPath": result.FullOutputPath}}, nil
	}}
	section := PromptSection{Key: "codemode", Render: func(ctx context.Context, input PromptInput) (*string, error) {
		enabled := false
		for _, tool := range input.Tools {
			if tool.Name == "codemode" {
				enabled = true
				break
			}
		}
		if !enabled {
			return nil, nil
		}
		if input.Read == nil || input.Read.runtime == nil {
			return nil, reject("codemode section requires invocation reader")
		}
		runtime := input.Read.runtime
		tools, err := runtime.CodemodeTools(ctx)
		if err != nil {
			return nil, err
		}
		// Deferred tools remain callable/discoverable but are omitted inline.
		inline := tools[:0]
		for _, tool := range tools {
			if !tool.Deferred {
				inline = append(inline, tool)
			}
		}
		catalogue, err := CodemodeSection(inline, runtime.harness.options.CodemodeNamespaces, budget)
		if err != nil {
			return nil, err
		}
		start := CodemodeStart{PaidModels: runtime.harness.options.PaidModels}
		for _, global := range runtime.harness.options.CodemodeGlobals {
			start.Globals = append(start.Globals, codemodeGlobalDescription(global))
		}
		text := "Use codemode to execute an async JavaScript function body. Await tool/global calls serially in this native candidate. Use text(), image(), console, store()/load(); no ambient filesystem, network or timers. Tool discovery does not grant additional authority.\n\n" + catalogue + "\n\n```typescript\n" + start.CodemodeDeclarations() + "\n```"
		return &text, nil
	}}
	return Extension{Name: "codemode", Tools: []ToolRegistration{tool}, Sections: []PromptSection{section}}, nil
}
