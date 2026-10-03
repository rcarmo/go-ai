// Command generate-models reads pi-ai's models.generated.js and emits
// models_generated.go with all known models registered.
//
// Usage:
//
//	go run ./scripts/generate-models.go [-input /path/to/models.generated.js] [-output models_generated.go]
//
// The input file is a JavaScript ESM module with the shape:
//
//	export const MODELS = { "provider": { "model-id": { id: "...", ... }, ... }, ... };
//
// Property keys inside model objects are unquoted JS identifiers (id, name, api, etc.)
// which this tool converts to valid JSON before parsing.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

func main() {
	defaultInput := findModelsJS()
	input := flag.String("input", defaultInput, "path to models.generated.js")
	output := flag.String("output", "models_generated.go", "output Go file path")
	kind := flag.String("kind", "chat", "model kind to generate from schema-v6 exports: chat, image, or classifier")
	dataDir := flag.String("data-dir", "", "offline schema-v6 provider JSON directory")
	flag.Parse()
	if *kind != "chat" && *kind != "image" && *kind != "classifier" {
		fmt.Fprintln(os.Stderr, "invalid catalog kind")
		os.Exit(1)
	}

	if *input == "" && *dataDir == "" {
		fmt.Fprintln(os.Stderr, "ERROR: could not find models.generated.js")
		fmt.Fprintln(os.Stderr, "Specify with -input /path/to/models.generated.js")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Input:  %s\n", *input)
	fmt.Fprintf(os.Stderr, "Output: %s\n", *output)

	var parsed map[string]map[string]modelEntry
	if *dataDir != "" {
		var err error
		parsed, err = readOfflineCatalog(*dataDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	} else {
		data, err := os.ReadFile(*input)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		jsText := inlineModularModels(*input, string(data), *kind)
		if err := json.Unmarshal([]byte(jsObjectToJSON(jsText)), &parsed); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	models := filterModelsByKind(parsed, *kind)

	// Count
	total := 0
	for _, providerModels := range models {
		total += len(providerModels)
	}
	fmt.Fprintf(os.Stderr, "Found %d %s models across %d providers\n", total, *kind, len(models))

	// Generate Go source
	goSource := generateGoSource(models, total, *kind)
	if err := writeAtomicCatalog(*output, []byte(goSource)); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Wrote %s (%d bytes)\n", *output, len(goSource))
}

type modelEntry struct {
	ID               string                 `json:"id"`
	Type             string                 `json:"type"`
	Name             string                 `json:"name"`
	Api              string                 `json:"api"`
	Provider         string                 `json:"provider"`
	BaseURL          string                 `json:"baseUrl"`
	Headers          map[string]string      `json:"headers"`
	Compat           compatEntry            `json:"compat"`
	Reasoning        bool                   `json:"reasoning"`
	ThinkingLevelMap map[string]*string     `json:"thinkingLevelMap"`
	Input            []string               `json:"input"`
	InputLimits      *inputLimitsEntry      `json:"inputLimits"`
	PromptCache      *promptCacheEntry      `json:"promptCache"`
	Enabled          *bool                  `json:"enabled"`
	Lab              string                 `json:"lab"`
	Providers        []providerInfoEntry    `json:"providers"`
	Cost             costEntry              `json:"cost"`
	ContextWindow    int                    `json:"contextWindow"`
	MaxTokens        int                    `json:"maxTokens"`
	SamplingParams   map[string]interface{} `json:"samplingParams"`
}

type compatEntry struct {
	SupportsStore                               *bool                        `json:"supportsStore"`
	SupportsDeveloperRole                       *bool                        `json:"supportsDeveloperRole"`
	SupportsReasoningEffort                     *bool                        `json:"supportsReasoningEffort"`
	SupportsUsageInStreaming                    *bool                        `json:"supportsUsageInStreaming"`
	SupportsFinishReason                        *bool                        `json:"supportsFinishReason"`
	MaxTokensField                              string                       `json:"maxTokensField"`
	RequiresToolResultName                      *bool                        `json:"requiresToolResultName"`
	RequiresAssistantAfterToolResult            *bool                        `json:"requiresAssistantAfterToolResult"`
	RequiresThinkingAsText                      *bool                        `json:"requiresThinkingAsText"`
	RequiresReasoningContentOnAssistantMessages *bool                        `json:"requiresReasoningContentOnAssistantMessages"`
	ThinkingFormat                              string                       `json:"thinkingFormat"`
	ChatTemplateKwargs                          map[string]chatTemplateKwarg `json:"chatTemplateKwargs"`
	ChatTemplateArgs                            map[string]chatTemplateKwarg `json:"chatTemplateArgs"`
	ThinkingTokenBudgetField                    string                       `json:"thinkingTokenBudgetField"`
	SupportsThinkingTokenBudget                 *bool                        `json:"supportsThinkingTokenBudget"`
	OpenRouterRouting                           map[string]interface{}       `json:"openRouterRouting"`
	VercelGatewayRouting                        map[string]interface{}       `json:"vercelGatewayRouting"`
	ZaiToolStream                               *bool                        `json:"zaiToolStream"`
	SupportsStrictMode                          *bool                        `json:"supportsStrictMode"`
	SupportsOpenAIGrammarTools                  *bool                        `json:"supportsOpenAIGrammarTools"`
	CacheControlFormat                          string                       `json:"cacheControlFormat"`
	SendSessionAffinityHeaders                  *bool                        `json:"sendSessionAffinityHeaders"`
	DeferredToolsMode                           string                       `json:"deferredToolsMode"`
	SupportsLongCacheRetention                  *bool                        `json:"supportsLongCacheRetention"`
	SupportsTemperature                         *bool                        `json:"supportsTemperature"`
	VLLMPriority                                *int                         `json:"vllmPriority"`
	ForceAdaptiveThinking                       *bool                        `json:"forceAdaptiveThinking"`
	AllowedFallbackModels                       []allowedFallbackModel       `json:"allowedFallbackModels"`
	AllowEmptySignature                         *bool                        `json:"allowEmptySignature"`
	SendSessionIdHeader                         *bool                        `json:"sendSessionIdHeader"`
	SupportsEagerToolInputStreaming             *bool                        `json:"supportsEagerToolInputStreaming"`
	SupportsToolReferences                      *bool                        `json:"supportsToolReferences"`
	SupportsAdditionalTools                     *bool                        `json:"supportsAdditionalTools"`
	SupportsToolSearch                          *bool                        `json:"supportsToolSearch"`
	SupportsCacheControlOnTools                 *bool                        `json:"supportsCacheControlOnTools"`
	SupportsStrictTools                         *bool                        `json:"supportsStrictTools"`
	SessionAffinityFormat                       string                       `json:"sessionAffinityFormat"`
	SupportsExplicitPromptCacheMode             *bool                        `json:"supportsExplicitPromptCacheMode"`
	SupportsMaxOutputTokens                     *bool                        `json:"supportsMaxOutputTokens"`
	SupportsMidConvoEffort                      *bool                        `json:"supportsMidConvoEffort"`
	SupportsMidConvoSystemMessages              *bool                        `json:"supportsMidConvoSystemMessages"`
	SupportsMidConvoToolAdditions               *bool                        `json:"supportsMidConvoToolAdditions"`
	SupportsMidConvoToolChanges                 *bool                        `json:"supportsMidConvoToolChanges"`
}

type allowedFallbackModel struct {
	Provider string    `json:"provider"`
	Model    string    `json:"model"`
	Cost     costEntry `json:"cost"`
}

type chatTemplateKwarg struct {
	Var         string      `json:"$var"`
	OmitWhenOff bool        `json:"omitWhenOff"`
	Value       interface{} `json:"-"`
}

func (c *chatTemplateKwarg) UnmarshalJSON(data []byte) error {
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err == nil {
		if v, ok := obj["$var"].(string); ok {
			c.Var = v
		}
		if v, ok := obj["omitWhenOff"].(bool); ok {
			c.OmitWhenOff = v
		}
		if c.Var != "" || c.OmitWhenOff {
			return nil
		}
	}
	var literal interface{}
	if err := json.Unmarshal(data, &literal); err != nil {
		return err
	}
	c.Value = literal
	return nil
}

type inputLimitsEntry struct {
	MaxRequestBytes int                    `json:"maxRequestBytes"`
	Images          *imageInputLimitsEntry `json:"images"`
}

type imageInputLimitsEntry struct {
	Resize        *imageResizeEntry `json:"resize"`
	MaxPerMessage int               `json:"maxPerMessage"`
	MaxPerRequest int               `json:"maxPerRequest"`
}

type imageResizeEntry struct {
	MaxWidth    int `json:"maxWidth"`
	MaxHeight   int `json:"maxHeight"`
	MaxBytes    int `json:"maxBytes"`
	JPEGQuality int `json:"jpegQuality"`
}

type promptCacheEntry struct {
	Short int `json:"short"`
	Long  int `json:"long"`
}

type providerInfoEntry struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Credential string `json:"credential"`
	Source     string `json:"source"`
}

type costEntry struct {
	Input      float64         `json:"input"`
	Output     float64         `json:"output"`
	CacheRead  float64         `json:"cacheRead"`
	CacheWrite float64         `json:"cacheWrite"`
	Tiers      []costTierEntry `json:"tiers"`
}

type costTierEntry struct {
	InputTokensAbove int     `json:"inputTokensAbove"`
	Input            float64 `json:"input"`
	Output           float64 `json:"output"`
	CacheRead        float64 `json:"cacheRead"`
	CacheWrite       float64 `json:"cacheWrite"`
}

func inlineModularModels(inputPath, js string, kind string) string {
	importRe := regexp.MustCompile(`(?m)^import \{ ([A-Z0-9_, ]+) \} from "([^"]+\.models\.(?:js|ts))";`)
	imports := map[string]string{}
	baseDir := filepath.Dir(inputPath)
	for _, match := range importRe.FindAllStringSubmatch(js, -1) {
		modulePath := filepath.Join(baseDir, filepath.FromSlash(strings.TrimPrefix(match[2], "./")))
		data, err := os.ReadFile(modulePath)
		if err != nil {
			continue
		}
		for _, symbol := range strings.Split(match[1], ",") {
			symbol = strings.TrimSpace(symbol)
			if symbol == "" {
				continue
			}
			imports[symbol] = extractProviderModelsObject(modulePath, string(data), symbol)
		}
	}
	if len(imports) == 0 {
		return js
	}

	exportName := "MODELS"
	if kind == "image" {
		exportName = "IMAGE_MODELS"
	} else if kind == "classifier" {
		exportName = "CLASSIFIER_MODELS"
	}
	sectionRe := regexp.MustCompile(`(?s)export const ` + exportName + `\s*=\s*\{(.*?)\};`)
	section := sectionRe.FindStringSubmatch(js)
	if len(section) < 2 {
		return js
	}
	var b strings.Builder
	b.WriteString("export const MODELS = {\n")
	entryRe := regexp.MustCompile(`(?m)^\s*"([^"]+)":\s*([A-Z0-9_]+),?\s*$`)
	for _, match := range entryRe.FindAllStringSubmatch(section[1], -1) {
		obj, ok := imports[match[2]]
		if !ok || obj == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("  %q: %s,\n", match[1], obj))
	}
	b.WriteString("};")
	return b.String()
}

func extractProviderModelsObject(modulePath string, js string, symbol string) string {
	jsonImportRe := regexp.MustCompile(`(?s)import\s+values\s+from\s+"([^"]+\.json)"`)
	if match := jsonImportRe.FindStringSubmatch(js); len(match) >= 2 {
		jsonPath := filepath.Join(filepath.Dir(modulePath), filepath.FromSlash(strings.TrimPrefix(match[1], "./")))
		if data, err := os.ReadFile(jsonPath); err == nil {
			return flattenProviderDataJSON(data, symbol)
		}
		if dataDir := os.Getenv("PI_AI_MODEL_DATA_DIR"); dataDir != "" {
			if data, err := os.ReadFile(filepath.Join(dataDir, filepath.Base(match[1]))); err == nil {
				return flattenProviderDataJSON(data, symbol)
			}
		}
	}
	re := regexp.MustCompile(`(?s)export const [A-Z0-9_]+\s*=\s*(\{.*\})\s*(?:as const)?;?`)
	if match := re.FindStringSubmatch(js); len(match) >= 2 {
		return match[1]
	}
	start := strings.Index(js, "{")
	end := strings.LastIndex(js, "}")
	if start < 0 || end < 0 || end <= start {
		return "{}"
	}
	return js[start : end+1]
}

func flattenProviderDataJSON(data []byte, symbol string) string {
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(data, &outer); err != nil || len(outer) == 0 {
		return string(data)
	}
	wantType := "chat"
	if strings.Contains(symbol, "IMAGE") {
		wantType = "image"
	} else if strings.Contains(symbol, "CLASSIFIER") {
		wantType = "classifier"
	}
	flat := map[string]json.RawMessage{}
	for _, groupRaw := range outer {
		var group map[string]json.RawMessage
		if err := json.Unmarshal(groupRaw, &group); err != nil {
			return string(data)
		}
		for key, raw := range group {
			var probe struct {
				Type     string `json:"type"`
				ID       string `json:"id"`
				Provider string `json:"provider"`
			}
			_ = json.Unmarshal(raw, &probe)
			if probe.Type == "" {
				probe.Type = "chat"
			}
			if probe.ID != "" && probe.Provider != "" && probe.Type == wantType {
				flat[strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(key, "chat:"), "image:"), "classifier:")] = raw
			}
		}
	}
	out, err := json.Marshal(flat)
	if err != nil {
		return string(data)
	}
	return string(out)
}

func filterModelsByKind(models map[string]map[string]modelEntry, kind string) map[string]map[string]modelEntry {
	if kind == "" {
		kind = "chat"
	}
	out := map[string]map[string]modelEntry{}
	for provider, providerModels := range models {
		for key, model := range providerModels {
			modelKind := model.Type
			if modelKind == "" {
				modelKind = "chat"
			}
			if modelKind != kind {
				continue
			}
			if out[provider] == nil {
				out[provider] = map[string]modelEntry{}
			}
			out[provider][key] = model
		}
	}
	return out
}

// jsObjectToJSON converts the JS module to a JSON object.
// Handles: export const MODELS = {...}; wrapper, unquoted keys, trailing commas.
func jsObjectToJSON(js string) string {
	// Strip comments and source map
	lines := strings.Split(js, "\n")
	var clean []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		clean = append(clean, line)
	}
	js = strings.Join(clean, "\n")
	js = regexp.MustCompile(`\}\s+satisfies\s+Model<[^>]+>`).ReplaceAllString(js, "}")
	js = regexp.MustCompile(`\s+as const`).ReplaceAllString(js, "")

	// Extract object between first { and last }
	start := strings.Index(js, "{")
	end := strings.LastIndex(js, "}")
	if start < 0 || end < 0 || end <= start {
		return "{}"
	}
	obj := js[start : end+1]

	// Quote unquoted JS property keys
	// Matches: whitespace + identifier + colon (but not inside strings)
	// This regex handles the common case: "    key: value"
	keyRe := regexp.MustCompile(`(?m)^(\s+)([a-zA-Z_][a-zA-Z0-9_]*)\s*:`)
	obj = keyRe.ReplaceAllString(obj, `$1"$2":`)

	// Remove trailing commas before } or ]
	trailingCommaRe := regexp.MustCompile(`,\s*([}\]])`)
	obj = trailingCommaRe.ReplaceAllString(obj, "$1")

	return obj
}

func generateGoSource(models map[string]map[string]modelEntry, total int, kind string) string {
	var b strings.Builder

	b.WriteString("// Code generated by scripts/generate-models.go from @earendil-works/pi-ai. DO NOT EDIT.\n")
	b.WriteString("//\n")
	b.WriteString(fmt.Sprintf("// Source: models.generated.js (%d %s models, %d providers)\n", total, kind, len(models)))
	b.WriteString("// Generated: deterministic\n")
	b.WriteString("\n")
	b.WriteString("package goai\n\n")
	if kind == "chat" {
		b.WriteString("import \"encoding/json\"\n\n")
	}
	if kind == "classifier" {
		b.WriteString("// RegisterBuiltinClassifierModels registers all known classifier models from pi-ai's model registry.\n")
		b.WriteString("func RegisterBuiltinClassifierModels() {\n")
		b.WriteString("\tfor i := range builtinClassifierModels {\n")
		b.WriteString("\t\tRegisterClassifierModel(&builtinClassifierModels[i])\n")
		b.WriteString("\t}\n")
		b.WriteString("}\n\n")
		b.WriteString("var builtinClassifierModels = []ClassifierModel{\n")
	} else if kind == "image" {
		b.WriteString("// RegisterBuiltinImageModels registers all known image models from pi-ai's model registry.\n")
		b.WriteString("func RegisterBuiltinImageModels() {\n")
		b.WriteString("\tfor i := range builtinImageModels {\n")
		b.WriteString("\t\tRegisterImageModel(&builtinImageModels[i])\n")
		b.WriteString("\t}\n")
		b.WriteString("}\n\n")
		b.WriteString("var builtinImageModels = []ImageModel{\n")
	} else {
		b.WriteString("// RegisterBuiltinModels registers all known models from pi-ai's model registry.\n")
		b.WriteString("// Call this during init() or at program startup to populate the model registry.\n")
		b.WriteString("func RegisterBuiltinModels() {\n")
		b.WriteString("\tbyProvider := map[Provider][]*Model{}\n")
		b.WriteString("\tfor i := range builtinModels {\n")
		b.WriteString("\t\tmodel := cloneModel(&builtinModels[i])\n")
		b.WriteString("\t\tbyProvider[model.Provider] = append(byProvider[model.Provider], model)\n")
		b.WriteString("\t}\n")
		b.WriteString("\tfor provider, models := range byProvider {\n")
		b.WriteString("\t\tRegisterDynamicModelProvider(StaticModelProvider{Provider: provider, Models: models})\n")
		b.WriteString("\t}\n")
		b.WriteString("}\n\n")
		b.WriteString("var builtinModels = []Model{\n")
	}

	// Sort providers for deterministic output
	providerNames := sortedKeys(models)
	for _, provider := range providerNames {
		providerModels := models[provider]
		modelIDs := sortedModelKeys(providerModels)
		for _, id := range modelIDs {
			m := providerModels[id]
			if m.ID == "" {
				m.ID = id
			}
			if m.Provider == "" {
				m.Provider = provider
			}

			inputArr := `[]string{"text"}`
			if len(m.Input) > 0 {
				parts := make([]string, len(m.Input))
				for i, inp := range m.Input {
					parts[i] = fmt.Sprintf("%q", inp)
				}
				inputArr = "[]string{" + strings.Join(parts, ", ") + "}"
			}

			b.WriteString("\t{\n")
			b.WriteString(fmt.Sprintf("\t\tID:            %q,\n", m.ID))
			if m.Type != "" {
				b.WriteString(fmt.Sprintf("\t\tType:          %q,\n", m.Type))
			}
			b.WriteString(fmt.Sprintf("\t\tName:          %q,\n", m.Name))
			if kind == "image" {
				b.WriteString(fmt.Sprintf("\t\tApi:           ImageApi(%q),\n", m.Api))
				b.WriteString(fmt.Sprintf("\t\tProvider:      ImageProvider(%q),\n", m.Provider))
			} else if kind == "classifier" {
				b.WriteString(fmt.Sprintf("\t\tApi:           ClassifierApi(%q),\n", m.Api))
				b.WriteString(fmt.Sprintf("\t\tProvider:      ClassifierProvider(%q),\n", m.Provider))
			} else {
				b.WriteString(fmt.Sprintf("\t\tApi:           %q,\n", m.Api))
				b.WriteString(fmt.Sprintf("\t\tProvider:      %q,\n", m.Provider))
			}
			b.WriteString(fmt.Sprintf("\t\tBaseURL:       %q,\n", m.BaseURL))
			if len(m.Headers) > 0 {
				b.WriteString("\t\tHeaders:       map[string]string{")
				keys := make([]string, 0, len(m.Headers))
				for k := range m.Headers {
					keys = append(keys, k)
				}
				sortStrings(keys)
				for i, k := range keys {
					if i > 0 {
						b.WriteString(", ")
					}
					b.WriteString(fmt.Sprintf("%q: %q", k, m.Headers[k]))
				}
				b.WriteString("},\n")
			}
			if kind == "chat" {
				writeCompat(&b, m.Api, m.Compat)
				b.WriteString(fmt.Sprintf("\t\tReasoning:     %v,\n", m.Reasoning))
			}
			if kind == "chat" && len(m.ThinkingLevelMap) > 0 {
				b.WriteString("\t\tThinkingLevelMap: map[ModelThinkingLevel]*string{")
				keys := make([]string, 0, len(m.ThinkingLevelMap))
				for k := range m.ThinkingLevelMap {
					keys = append(keys, k)
				}
				sortStrings(keys)
				for i, k := range keys {
					if i > 0 {
						b.WriteString(", ")
					}
					if m.ThinkingLevelMap[k] == nil {
						b.WriteString(fmt.Sprintf("%q: nil", k))
					} else {
						b.WriteString(fmt.Sprintf("%q: strPtr(%q)", k, *m.ThinkingLevelMap[k]))
					}
				}
				b.WriteString("},\n")
			}
			b.WriteString(fmt.Sprintf("\t\tInput:         %s,\n", inputArr))
			writeInputLimitsField(&b, m.InputLimits)
			writePromptCacheField(&b, m.PromptCache)
			writeEnabledField(&b, m.Enabled)
			if m.Lab != "" {
				b.WriteString(fmt.Sprintf("\t\tLab:           %q,\n", m.Lab))
			}
			writeProvidersField(&b, m.Providers)
			b.WriteString(fmt.Sprintf("\t\tCost:          ModelCost{Input: %v, Output: %v, CacheRead: %v, CacheWrite: %v",
				m.Cost.Input, m.Cost.Output, m.Cost.CacheRead, m.Cost.CacheWrite))
			if len(m.Cost.Tiers) > 0 {
				b.WriteString(", Tiers: []ModelCostTier{")
				for _, tier := range m.Cost.Tiers {
					b.WriteString(fmt.Sprintf("{InputTokensAbove: %d, Input: %v, Output: %v, CacheRead: %v, CacheWrite: %v}, ", tier.InputTokensAbove, tier.Input, tier.Output, tier.CacheRead, tier.CacheWrite))
				}
				b.WriteString("}")
			}
			b.WriteString("},\n")
			b.WriteString(fmt.Sprintf("\t\tContextWindow: %d,\n", m.ContextWindow))
			b.WriteString(fmt.Sprintf("\t\tMaxTokens:     %d,\n", m.MaxTokens))
			writeMapField(&b, "SamplingParams", m.SamplingParams)
			b.WriteString("\t},\n")
		}
	}

	b.WriteString("}\n\n")
	if kind == "chat" {
		b.WriteString("func strPtr(v string) *string { return &v }\n")
		b.WriteString("func boolPtr(v bool) *bool { return &v }\n")
		b.WriteString("func mustMap(data string) map[string]interface{} { var out map[string]interface{}; _ = json.Unmarshal([]byte(data), &out); return out }\n")
		b.WriteString("func mustValue(data string) interface{} { var out interface{}; _ = json.Unmarshal([]byte(data), &out); return out }\n")
	}
	return b.String()
}

func writeCompat(b *strings.Builder, api string, c compatEntry) {
	if !hasCompat(c) {
		return
	}
	switch api {
	case "openai-completions":
		b.WriteString("\t\tCompletionsCompat: &OpenAICompletionsCompat{")
		writeBoolField(b, "SupportsStore", c.SupportsStore)
		writeBoolField(b, "SupportsDeveloperRole", c.SupportsDeveloperRole)
		writeBoolField(b, "SupportsReasoningEffort", c.SupportsReasoningEffort)
		writeBoolField(b, "SupportsUsageInStreaming", c.SupportsUsageInStreaming)
		writeBoolField(b, "SupportsFinishReason", c.SupportsFinishReason)
		writeStringField(b, "MaxTokensField", c.MaxTokensField)
		writeBoolField(b, "RequiresToolResultName", c.RequiresToolResultName)
		writeBoolField(b, "RequiresAssistantAfterToolResult", c.RequiresAssistantAfterToolResult)
		writeBoolField(b, "RequiresThinkingAsText", c.RequiresThinkingAsText)
		writeBoolField(b, "RequiresReasoningContentOnAssistantMessages", c.RequiresReasoningContentOnAssistantMessages)
		writeStringField(b, "ThinkingFormat", c.ThinkingFormat)
		writeChatTemplateKwargsField(b, "ChatTemplateKwargs", c.ChatTemplateKwargs)
		writeChatTemplateKwargsField(b, "ChatTemplateArgs", c.ChatTemplateArgs)
		writeStringField(b, "ThinkingTokenBudgetField", c.ThinkingTokenBudgetField)
		writeBoolField(b, "SupportsThinkingTokenBudget", c.SupportsThinkingTokenBudget)
		writeMapField(b, "OpenRouterRouting", c.OpenRouterRouting)
		writeMapField(b, "VercelGatewayRouting", c.VercelGatewayRouting)
		writeBoolField(b, "ZaiToolStream", c.ZaiToolStream)
		writeBoolField(b, "SupportsStrictMode", c.SupportsStrictMode)
		writeBoolField(b, "SupportsMidConvoSystemMessages", c.SupportsMidConvoSystemMessages)
		writeBoolField(b, "SupportsMidConvoToolAdditions", c.SupportsMidConvoToolAdditions)
		writeBoolField(b, "SupportsOpenAIGrammarTools", c.SupportsOpenAIGrammarTools)
		writeStringField(b, "CacheControlFormat", c.CacheControlFormat)
		writeBoolField(b, "SendSessionAffinityHeaders", c.SendSessionAffinityHeaders)
		writeStringField(b, "DeferredToolsMode", c.DeferredToolsMode)
		writeBoolField(b, "SupportsLongCacheRetention", c.SupportsLongCacheRetention)
		writeBoolField(b, "SupportsTemperature", c.SupportsTemperature)
		writeIntField(b, "VLLMPriority", c.VLLMPriority)
		writeBoolField(b, "AllowEmptySignature", c.AllowEmptySignature)
		b.WriteString("},\n")
	case "openai-responses", "azure-openai-responses", "openai-codex-responses":
		b.WriteString("\t\tResponsesCompat: &OpenAIResponsesCompat{")
		writeBoolField(b, "SendSessionIdHeader", c.SendSessionIdHeader)
		writeBoolField(b, "SupportsMidConvoSystemMessages", c.SupportsMidConvoSystemMessages)
		writeBoolField(b, "SupportsLongCacheRetention", c.SupportsLongCacheRetention)
		writeBoolField(b, "SupportsAdditionalTools", c.SupportsAdditionalTools)
		writeBoolField(b, "SupportsToolSearch", c.SupportsToolSearch)
		writeBoolField(b, "SupportsOpenAIGrammarTools", c.SupportsOpenAIGrammarTools)
		writeBoolField(b, "SupportsStrictMode", c.SupportsStrictMode)
		writeStringField(b, "SessionAffinityFormat", c.SessionAffinityFormat)
		writeBoolField(b, "SupportsExplicitPromptCacheMode", c.SupportsExplicitPromptCacheMode)
		writeBoolField(b, "SupportsMaxOutputTokens", c.SupportsMaxOutputTokens)
		b.WriteString("},\n")
	case "anthropic-messages":
		b.WriteString("\t\tAnthropicCompat: &AnthropicMessagesCompat{")
		writeBoolField(b, "SupportsEagerToolInputStreaming", c.SupportsEagerToolInputStreaming)
		writeBoolField(b, "SupportsLongCacheRetention", c.SupportsLongCacheRetention)
		writeBoolField(b, "SupportsTemperature", c.SupportsTemperature)
		writeBoolField(b, "ForceAdaptiveThinking", c.ForceAdaptiveThinking)
		writeAllowedFallbackModelsField(b, c.AllowedFallbackModels)
		writeBoolField(b, "SupportsMidConvoEffort", c.SupportsMidConvoEffort)
		writeBoolField(b, "SupportsMidConvoSystemMessages", c.SupportsMidConvoSystemMessages)
		writeBoolField(b, "SupportsMidConvoToolChanges", c.SupportsMidConvoToolChanges)
		writeBoolField(b, "AllowEmptySignature", c.AllowEmptySignature)
		writeBoolField(b, "SupportsStrictTools", c.SupportsStrictTools)
		writeBoolField(b, "SupportsCacheControlOnTools", c.SupportsCacheControlOnTools)
		writeBoolField(b, "SendSessionAffinityHeaders", c.SendSessionAffinityHeaders)
		writeBoolField(b, "SupportsToolReferences", c.SupportsToolReferences)
		b.WriteString("},\n")
	case "bedrock-converse-stream":
		b.WriteString("\t\tBedrockCompat: &BedrockCompat{")
		writeBoolField(b, "SupportsStrictMode", c.SupportsStrictMode)
		b.WriteString("},\n")
	}
}

func hasCompat(c compatEntry) bool {
	return c.SupportsStore != nil || c.SupportsDeveloperRole != nil || c.SupportsReasoningEffort != nil || c.SupportsUsageInStreaming != nil || c.SupportsFinishReason != nil || c.MaxTokensField != "" || c.RequiresToolResultName != nil || c.RequiresAssistantAfterToolResult != nil || c.RequiresThinkingAsText != nil || c.RequiresReasoningContentOnAssistantMessages != nil || c.ThinkingFormat != "" || len(c.ChatTemplateKwargs) > 0 || len(c.ChatTemplateArgs) > 0 || c.ThinkingTokenBudgetField != "" || c.SupportsThinkingTokenBudget != nil || c.OpenRouterRouting != nil || c.VercelGatewayRouting != nil || c.ZaiToolStream != nil || c.SupportsStrictMode != nil || c.SupportsOpenAIGrammarTools != nil || c.CacheControlFormat != "" || c.SendSessionAffinityHeaders != nil || c.DeferredToolsMode != "" || c.SupportsLongCacheRetention != nil || c.SupportsTemperature != nil || c.VLLMPriority != nil || c.ForceAdaptiveThinking != nil || len(c.AllowedFallbackModels) > 0 || c.SupportsMidConvoEffort != nil || c.SupportsMidConvoSystemMessages != nil || c.SupportsMidConvoToolAdditions != nil || c.SupportsMidConvoToolChanges != nil || c.AllowEmptySignature != nil || c.SendSessionIdHeader != nil || c.SupportsAdditionalTools != nil || c.SupportsToolSearch != nil || c.SupportsMaxOutputTokens != nil || c.SupportsEagerToolInputStreaming != nil || c.SupportsToolReferences != nil || c.SupportsStrictTools != nil || c.SupportsCacheControlOnTools != nil || c.SessionAffinityFormat != "" || c.SupportsExplicitPromptCacheMode != nil
}

func writeInputLimitsField(b *strings.Builder, value *inputLimitsEntry) {
	if value == nil {
		return
	}
	b.WriteString("\t\tInputLimits:  &ModelInputLimits{")
	if value.MaxRequestBytes != 0 {
		b.WriteString(fmt.Sprintf("MaxRequestBytes: %d, ", value.MaxRequestBytes))
	}
	if value.Images != nil {
		b.WriteString("Images: &ModelImageInputLimits{")
		if value.Images.Resize != nil {
			b.WriteString(fmt.Sprintf("Resize: &ModelImageResizeOptions{MaxWidth: %d, MaxHeight: %d, MaxBytes: %d, JPEGQuality: %d}, ", value.Images.Resize.MaxWidth, value.Images.Resize.MaxHeight, value.Images.Resize.MaxBytes, value.Images.Resize.JPEGQuality))
		}
		if value.Images.MaxPerMessage != 0 {
			b.WriteString(fmt.Sprintf("MaxPerMessage: %d, ", value.Images.MaxPerMessage))
		}
		if value.Images.MaxPerRequest != 0 {
			b.WriteString(fmt.Sprintf("MaxPerRequest: %d, ", value.Images.MaxPerRequest))
		}
		b.WriteString("}, ")
	}
	b.WriteString("},\n")
}

func writePromptCacheField(b *strings.Builder, value *promptCacheEntry) {
	if value == nil {
		return
	}
	b.WriteString(fmt.Sprintf("\t\tPromptCache:  &ModelPromptCache{Short: %d, Long: %d},\n", value.Short, value.Long))
}

func writeEnabledField(b *strings.Builder, value *bool) {
	if value != nil {
		b.WriteString(fmt.Sprintf("\t\tEnabled:      boolPtr(%v),\n", *value))
	}
}

func writeProvidersField(b *strings.Builder, values []providerInfoEntry) {
	if len(values) == 0 {
		return
	}
	b.WriteString("\t\tProviders:    []ModelProviderInfo{")
	for _, value := range values {
		b.WriteString("{")
		if value.ID != "" {
			b.WriteString(fmt.Sprintf("ID: %q, ", value.ID))
		}
		if value.Name != "" {
			b.WriteString(fmt.Sprintf("Name: %q, ", value.Name))
		}
		if value.Credential != "" {
			b.WriteString(fmt.Sprintf("Credential: %q, ", value.Credential))
		}
		if value.Source != "" {
			b.WriteString(fmt.Sprintf("Source: %q, ", value.Source))
		}
		b.WriteString("}, ")
	}
	b.WriteString("},\n")
}

func writeBoolField(b *strings.Builder, name string, value *bool) {
	if value != nil {
		b.WriteString(fmt.Sprintf("%s: boolPtr(%v), ", name, *value))
	}
}

func writeIntField(b *strings.Builder, name string, value *int) {
	if value != nil {
		b.WriteString(fmt.Sprintf("%s: intPtr(%d), ", name, *value))
	}
}

func writeStringField(b *strings.Builder, name string, value string) {
	if value != "" {
		b.WriteString(fmt.Sprintf("%s: %q, ", name, value))
	}
}

func writeMapField(b *strings.Builder, name string, value map[string]interface{}) {
	if len(value) == 0 {
		return
	}
	data, _ := json.Marshal(value)
	b.WriteString(fmt.Sprintf("%s: mustMap(%q), ", name, string(data)))
}

func writeChatTemplateKwargsField(b *strings.Builder, fieldName string, value map[string]chatTemplateKwarg) {
	if len(value) == 0 {
		return
	}
	keys := make([]string, 0, len(value))
	for k := range value {
		keys = append(keys, k)
	}
	sortStrings(keys)
	b.WriteString(fieldName + ": map[string]ChatTemplateKwargValue{")
	for _, k := range keys {
		v := value[k]
		b.WriteString(fmt.Sprintf("%q: {", k))
		if v.Var != "" {
			b.WriteString(fmt.Sprintf("Var: %q, ", v.Var))
			if v.OmitWhenOff {
				b.WriteString("OmitWhenOff: true, ")
			}
		} else {
			data, _ := json.Marshal(v.Value)
			b.WriteString(fmt.Sprintf("Value: mustValue(%q), ", string(data)))
		}
		b.WriteString("}, ")
	}
	b.WriteString("}, ")
}

func writeAllowedFallbackModelsField(b *strings.Builder, value []allowedFallbackModel) {
	if len(value) == 0 {
		return
	}
	b.WriteString("AllowedFallbackModels: []AnthropicAllowedFallbackModel{")
	for _, fallback := range value {
		b.WriteString("{")
		b.WriteString(fmt.Sprintf("Provider: Provider(%q), Model: %q, ", fallback.Provider, fallback.Model))
		b.WriteString(fmt.Sprintf("Cost: ModelCost{Input: %g, Output: %g, CacheRead: %g, CacheWrite: %g}, ", fallback.Cost.Input, fallback.Cost.Output, fallback.Cost.CacheRead, fallback.Cost.CacheWrite))
		b.WriteString("}, ")
	}
	b.WriteString("}, ")
}

func sortedKeys(m map[string]map[string]modelEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}

func sortedModelKeys(m map[string]modelEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// findModelsJS searches common locations for models.generated.js
func findModelsJS() string {
	candidates := []string{
		"node_modules/@earendil-works/pi-ai/dist/models.generated.js",
		"/usr/local/lib/bun/install/global/node_modules/@earendil-works/pi-ai/dist/models.generated.js",
		// legacy fallback paths
		"node_modules/@mariozechner/pi-ai/dist/models.generated.js",
		"/usr/local/lib/bun/install/global/node_modules/@mariozechner/pi-ai/dist/models.generated.js",
	}

	// Also check relative to the script location
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "..", "node_modules", "@earendil-works", "pi-ai", "dist", "models.generated.js"),
			// legacy fallback path
			filepath.Join(dir, "..", "node_modules", "@mariozechner", "pi-ai", "dist", "models.generated.js"),
		)
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func readOfflineCatalog(dir string) (map[string]map[string]modelEntry, error) {
	var manifest struct {
		SchemaVersion int               `json:"schemaVersion"`
		GeneratedAt   string            `json:"generatedAt"`
		StructureHash string            `json:"structureHash"`
		Files         map[string]string `json:"files"`
	}
	data, err := os.ReadFile(filepath.Join(dir, ".manifest.json"))
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	if manifest.SchemaVersion != 6 || len(manifest.Files) != 42 || manifest.StructureHash != "03d2e1aeeee6eb16959d4f727b47b9b187efaf863c688a47889fb90d200e6812" {
		return nil, fmt.Errorf("invalid v1.0.1 catalog manifest")
	}
	if _, err := time.Parse(time.RFC3339Nano, manifest.GeneratedAt); err != nil {
		return nil, fmt.Errorf("invalid catalog generation timestamp")
	}
	structure := map[string]map[string]string{}
	out := map[string]map[string]modelEntry{}
	names := []string{}
	for name := range manifest.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	actual := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") && e.Name() != ".manifest.json" {
			actual++
			if _, ok := manifest.Files[e.Name()]; !ok {
				return nil, fmt.Errorf("unexpected provider file %s", e.Name())
			}
		}
	}
	if actual != len(names) {
		return nil, fmt.Errorf("missing provider files")
	}
	for _, name := range names {
		if filepath.Base(name) != name || !strings.HasSuffix(name, ".json") || strings.ContainsAny(name, "/\\") {
			return nil, fmt.Errorf("unsafe catalog filename %s", name)
		}
		provider := strings.TrimSuffix(name, ".json")
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		if fmt.Sprintf("%x", sha256.Sum256(raw)) != manifest.Files[name] {
			return nil, fmt.Errorf("provider hash mismatch: %s", name)
		}
		var groups map[string]map[string]json.RawMessage
		if err = json.Unmarshal(raw, &groups); err != nil {
			return nil, err
		}
		if groups == nil {
			return nil, fmt.Errorf("invalid provider object")
		}
		out[provider] = map[string]modelEntry{}
		structure[provider] = map[string]string{}
		for api, models := range groups {
			if models == nil {
				return nil, fmt.Errorf("invalid API group %s", api)
			}
			for key, value := range models {
				var m modelEntry
				if err = json.Unmarshal(value, &m); err != nil {
					return nil, err
				}
				if key != m.Type+":"+m.ID || m.Provider != provider || m.Api != api || m.ID == "" || m.Name == "" {
					return nil, fmt.Errorf("invalid model identity: %s/%s", provider, key)
				}
				if _, exists := structure[provider][key]; exists {
					return nil, fmt.Errorf("duplicate model: %s/%s", provider, key)
				}
				if m.Type != "chat" && m.Type != "image" && m.Type != "classifier" {
					return nil, fmt.Errorf("invalid model kind")
				}
				if len(m.Input) == 0 {
					return nil, fmt.Errorf("invalid input modalities")
				}
				for _, modality := range m.Input {
					if modality != "text" && modality != "image" {
						return nil, fmt.Errorf("invalid input modality %s", modality)
					}
				}
				if (m.Type == "chat" && (m.ContextWindow <= 0 || m.MaxTokens <= 0)) || (m.Type == "classifier" && m.ContextWindow <= 0) {
					return nil, fmt.Errorf("invalid model capacity")
				}
				var metadata map[string]any
				_ = json.Unmarshal(value, &metadata)
				if m.Type == "image" {
					outputs, ok := metadata["output"].([]any)
					image := false
					for _, v := range outputs {
						if v == "image" {
							image = true
						}
						if v != "text" && v != "image" {
							return nil, fmt.Errorf("invalid output modality")
						}
					}
					if !ok || !image {
						return nil, fmt.Errorf("invalid image outputs")
					}
				} else if _, ok := metadata["output"]; ok {
					return nil, fmt.Errorf("unexpected output modalities")
				}
				if _, ok := metadata["baseUrl"].(string); !ok {
					return nil, fmt.Errorf("invalid baseUrl")
				}
				if m.Type == "chat" {
					if _, ok := metadata["reasoning"].(bool); !ok {
						return nil, fmt.Errorf("missing reasoning metadata")
					}
				}
				costs, ok := metadata["cost"].(map[string]any)
				if !ok {
					return nil, fmt.Errorf("invalid costs")
				}
				for _, field := range []string{"input", "output", "cacheRead", "cacheWrite"} {
					f, ok := costs[field].(float64)
					if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
						return nil, fmt.Errorf("invalid cost %s", field)
					}
				}
				structure[provider][key] = api
				out[provider][key] = m
			}
		}
	}
	encoded, err := json.Marshal(structure)
	if err != nil {
		return nil, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(encoded)) != manifest.StructureHash {
		return nil, fmt.Errorf("catalog structure hash mismatch")
	}
	return out, nil
}

func writeAtomicCatalog(path string, data []byte) error {
	formatted, err := format.Source(data)
	if err != nil {
		return fmt.Errorf("format generated catalog: %w", err)
	}
	data = formatted
	temp, err := os.CreateTemp(filepath.Dir(path), ".model-catalog-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err = temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Chmod(0644); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
