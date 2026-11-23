package durable

import (
	"context"
	"encoding/json"
	"errors"
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

const codemodeToolDescription = "Run JavaScript that calls other tools. The input is raw JavaScript (not JSON or a code fence), run as an async function body in a QuickJS sandbox. No ambient filesystem, network, or timers.\n" +
	"- `await tools.<name>({ ...args })` resolves to text or a value described by the tool's declaration.\n" +
	"- `text(value)`, `image(dataUrlOrImageBlock)`, `console.log(...)`, and top-level `return` add output. `store(key, value)` and `load(key)` keep JSON values across calls.\n" +
	"- When the sandbox provides discovery globals, `await searchTools(query, { limit?, namespace? })`, `await describeTool(name)`, and `await describeNamespace(name)` find declarations.\n\n" +
	"The `codemode` section of the system prompt lists the tools scripts can call, by namespace, as far as space allows."

const codemodeModelsDescription = "`models` provides the model catalog (`getModelsOfType`, `getAvailableOfType`, `getModelOfType`) and can run `models.classify(...)` and `models.generateImages(...)` when the requested model is configured."

// CodemodeExtensionOptions controls model-facing declaration rendering.
// Nil InlineBudget uses 3000 estimated tokens; explicit zero lists no tools.
// Nil Namespaces inherits the harness list; an explicit empty list disables grouping.
// Execution authority and globals remain harness-scoped.
type CodemodeExtensionOptions struct {
	InlineBudget *int
	Namespaces   *[]CodemodeNamespace
	// Models may restrict harness paid authority, never expand it.
	Models *bool
	// Nil inherits host callbacks; explicit empty revokes all custom globals.
	Globals *[]CodemodeGlobal
	// Nil/zero keeps deadline-only execution; positive values cap compute polls.
	InterruptBudget *int
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
	var interruptBudget *int
	if len(options) == 1 && options[0].InterruptBudget != nil {
		copy := *options[0].InterruptBudget
		if copy < 0 || copy > 1000000 {
			return Extension{}, reject("invalid codemode interrupt budget")
		}
		interruptBudget = &copy
	}
	var globals *[]CodemodeGlobal
	if len(options) == 1 && options[0].Globals != nil {
		copy, err := cloneCodemodeGlobals(*options[0].Globals, DefaultLimits())
		if err != nil {
			return Extension{}, err
		}
		globals = &copy
	}
	var models *bool
	if len(options) == 1 && options[0].Models != nil {
		copy := *options[0].Models
		models = &copy
	}
	var namespaces *[]CodemodeNamespace
	if len(options) == 1 && options[0].Namespaces != nil {
		copy := copyCodemodeNamespaces(*options[0].Namespaces)
		if err := validateCodemodeNamespaces(copy, DefaultLimits()); err != nil {
			return Extension{}, err
		}
		namespaces = &copy
	}
	if budget < 0 {
		return Extension{}, reject("negative codemode inline budget")
	}
	if sandbox == nil {
		return Extension{}, reject("codemode requires sandbox")
	}
	parallel := false
	if capability, ok := sandbox.(CodemodeSandboxParallel); ok {
		parallel = capability.SupportsParallelCalls()
	}
	callGuidance := "Await tool/global calls serially."
	if parallel {
		callGuidance = "Independent tool/global calls may run concurrently with Promise.all; await required results before returning."
	}
	// Only the inline sandbox is known to install the paid model globals.
	// Model-facing selection also checks the actual Harness's permission.
	_, inline := sandbox.(*QuickJSSandbox)
	tool := ToolRegistration{describeCodemodeModels: inline && (models == nil || *models), Definition: goai.Tool{Name: "codemode", Description: codemodeToolDescription + "\n\n" + callGuidance, Parameters: json.RawMessage(`{"type":"object","properties":{"code":{"type":"string"}},"required":["code"],"additionalProperties":false}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": CodemodeSourceGrammar}}}, Implementation: "pi.codemode", Version: 1, ReplaySafe: true, Callers: []string{"model"}, Execute: func(ctx context.Context, rawToolArguments any, api *ToolAPI) (ToolResult, error) {
		args := rawToolArguments.(JSON)
		code, ok := args["code"].(string)
		if !ok {
			return ToolResult{}, reject("codemode source missing")
		}
		result, err := api.runtime.runCodemode(ctx, "script", code, sandbox, namespaces, models, globals, interruptBudget)
		if errors.Is(err, errCodemodeHostCutoff) {
			// The host closed this guest, not the script's durable call. Leave
			// the call undecided until its owner closes or explicitly aborts it.
			api.runtime.reportError(err)
			<-ctx.Done()
			return ToolResult{}, ctx.Err()
		}
		if err != nil {
			return ToolResult{}, err
		}
		status := "completed"
		if result.Error != "" {
			status = "failed"
		}
		header := fmt.Sprintf("Script %s\nWall time %.1f seconds\nOutput:\n", status, float64(result.WallMs)/1000)
		blocks := []goai.ContentBlock{{Type: "text", Text: header}}
		if result.Output != "" {
			blocks = append(blocks, goai.ContentBlock{Type: "text", Text: result.Output})
		}
		for _, image := range result.Images {
			blocks = append(blocks, goai.ContentBlock{Type: "image", Data: image.Data, MimeType: image.MimeType})
		}
		// Keep image bytes visible to model-facing hooks. Tool settlement chunks
		// the final blocks after those hooks have fitted or replaced them.
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
		selectedNamespaces := runtime.harness.options.CodemodeNamespaces
		if namespaces != nil {
			selectedNamespaces = *namespaces
		}
		selectedNamespaces, err = runtime.codemodeNamespaces(ctx, tools, selectedNamespaces)
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
		catalogue, err := CodemodeSection(inline, selectedNamespaces, budget)
		if err != nil {
			return nil, err
		}
		paid := runtime.harness.options.PaidModels
		if models != nil {
			paid = paid && *models
		}
		start := CodemodeStart{PaidModels: paid}
		selectedGlobals := runtime.harness.options.CodemodeGlobals
		if globals != nil {
			selectedGlobals = *globals
		}
		// Omit an empty catalogue section. Keep declarations for configured
		// native model/custom globals even when there are no nested tools.
		if catalogue == "" && !paid && len(selectedGlobals) == 0 {
			return nil, nil
		}
		for _, global := range selectedGlobals {
			start.Globals = append(start.Globals, codemodeGlobalDescription(global))
		}
		text := "Use codemode to execute an async JavaScript function body. " + callGuidance + " Use text(), image(), console, store()/load(); no ambient filesystem, network or timers. Tool discovery does not grant additional authority.\n\n" + catalogue + "\n\n```typescript\n" + start.CodemodeDeclarations() + "\n```"
		return &text, nil
	}}
	return Extension{Name: "codemode", Tools: []ToolRegistration{tool}, Sections: []PromptSection{section}}, nil
}
