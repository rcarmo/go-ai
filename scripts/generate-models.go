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
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func main() {
	defaultInput := findModelsJS()
	input := flag.String("input", defaultInput, "path to models.generated.js")
	output := flag.String("output", "models_generated.go", "output Go file path")
	flag.Parse()

	if *input == "" {
		fmt.Fprintln(os.Stderr, "ERROR: could not find models.generated.js")
		fmt.Fprintln(os.Stderr, "Specify with -input /path/to/models.generated.js")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Input:  %s\n", *input)
	fmt.Fprintf(os.Stderr, "Output: %s\n", *output)

	// Read the JS file
	data, err := os.ReadFile(*input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	// Extract the object literal
	jsText := string(data)
	jsonText := jsObjectToJSON(jsText)

	// Parse as JSON
	var models map[string]map[string]modelEntry
	if err := json.Unmarshal([]byte(jsonText), &models); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: JSON parse failed: %v\n", err)
		// Write debug file
		os.WriteFile("models_debug.json", []byte(jsonText), 0644)
		fmt.Fprintln(os.Stderr, "Debug JSON written to models_debug.json")
		os.Exit(1)
	}

	// Count
	total := 0
	for _, providerModels := range models {
		total += len(providerModels)
	}
	fmt.Fprintf(os.Stderr, "Found %d models across %d providers\n", total, len(models))

	// Generate Go source
	goSource := generateGoSource(models, total)
	if err := os.WriteFile(*output, []byte(goSource), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Wrote %s (%d bytes)\n", *output, len(goSource))
}

type modelEntry struct {
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	Api              string             `json:"api"`
	Provider         string             `json:"provider"`
	BaseURL          string             `json:"baseUrl"`
	Headers          map[string]string  `json:"headers"`
	Compat           compatEntry        `json:"compat"`
	Reasoning        bool               `json:"reasoning"`
	ThinkingLevelMap map[string]*string `json:"thinkingLevelMap"`
	Input            []string           `json:"input"`
	Cost             costEntry          `json:"cost"`
	ContextWindow    int                `json:"contextWindow"`
	MaxTokens        int                `json:"maxTokens"`
}

type compatEntry struct {
	SupportsStore                               *bool                  `json:"supportsStore"`
	SupportsDeveloperRole                       *bool                  `json:"supportsDeveloperRole"`
	SupportsReasoningEffort                     *bool                  `json:"supportsReasoningEffort"`
	SupportsUsageInStreaming                    *bool                  `json:"supportsUsageInStreaming"`
	MaxTokensField                              string                 `json:"maxTokensField"`
	RequiresToolResultName                      *bool                  `json:"requiresToolResultName"`
	RequiresAssistantAfterToolResult            *bool                  `json:"requiresAssistantAfterToolResult"`
	RequiresThinkingAsText                      *bool                  `json:"requiresThinkingAsText"`
	RequiresReasoningContentOnAssistantMessages *bool                  `json:"requiresReasoningContentOnAssistantMessages"`
	ThinkingFormat                              string                 `json:"thinkingFormat"`
	OpenRouterRouting                           map[string]interface{} `json:"openRouterRouting"`
	VercelGatewayRouting                        map[string]interface{} `json:"vercelGatewayRouting"`
	ZaiToolStream                               *bool                  `json:"zaiToolStream"`
	SupportsStrictMode                          *bool                  `json:"supportsStrictMode"`
	CacheControlFormat                          string                 `json:"cacheControlFormat"`
	SendSessionAffinityHeaders                  *bool                  `json:"sendSessionAffinityHeaders"`
	SupportsLongCacheRetention                  *bool                  `json:"supportsLongCacheRetention"`
	SendSessionIdHeader                         *bool                  `json:"sendSessionIdHeader"`
	SupportsEagerToolInputStreaming             *bool                  `json:"supportsEagerToolInputStreaming"`
}

type costEntry struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
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

func generateGoSource(models map[string]map[string]modelEntry, total int) string {
	var b strings.Builder

	b.WriteString("// Code generated by scripts/generate-models.go from @mariozechner/pi-ai. DO NOT EDIT.\n")
	b.WriteString("//\n")
	b.WriteString(fmt.Sprintf("// Source: models.generated.js (%d models, %d providers)\n", total, len(models)))
	b.WriteString(fmt.Sprintf("// Generated: %s\n", time.Now().UTC().Format(time.RFC3339)))
	b.WriteString("\n")
	b.WriteString("package goai\n\n")
	b.WriteString("import \"encoding/json\"\n\n")
	b.WriteString("// RegisterBuiltinModels registers all known models from pi-ai's model registry.\n")
	b.WriteString("// Call this during init() or at program startup to populate the model registry.\n")
	b.WriteString("func RegisterBuiltinModels() {\n")
	b.WriteString("\tfor i := range builtinModels {\n")
	b.WriteString("\t\tRegisterModel(&builtinModels[i])\n")
	b.WriteString("\t}\n")
	b.WriteString("}\n\n")
	b.WriteString("var builtinModels = []Model{\n")

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
			b.WriteString(fmt.Sprintf("\t\tName:          %q,\n", m.Name))
			b.WriteString(fmt.Sprintf("\t\tApi:           %q,\n", m.Api))
			b.WriteString(fmt.Sprintf("\t\tProvider:      %q,\n", m.Provider))
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
			writeCompat(&b, m.Api, m.Compat)
			b.WriteString(fmt.Sprintf("\t\tReasoning:     %v,\n", m.Reasoning))
			if len(m.ThinkingLevelMap) > 0 {
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
			b.WriteString(fmt.Sprintf("\t\tCost:          ModelCost{Input: %v, Output: %v, CacheRead: %v, CacheWrite: %v},\n",
				m.Cost.Input, m.Cost.Output, m.Cost.CacheRead, m.Cost.CacheWrite))
			b.WriteString(fmt.Sprintf("\t\tContextWindow: %d,\n", m.ContextWindow))
			b.WriteString(fmt.Sprintf("\t\tMaxTokens:     %d,\n", m.MaxTokens))
			b.WriteString("\t},\n")
		}
	}

	b.WriteString("}\n\n")
	b.WriteString("func strPtr(v string) *string { return &v }\n")
	b.WriteString("func boolPtr(v bool) *bool { return &v }\n")
	b.WriteString("func mustMap(data string) map[string]interface{} { var out map[string]interface{}; _ = json.Unmarshal([]byte(data), &out); return out }\n")
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
		writeStringField(b, "MaxTokensField", c.MaxTokensField)
		writeBoolField(b, "RequiresToolResultName", c.RequiresToolResultName)
		writeBoolField(b, "RequiresAssistantAfterToolResult", c.RequiresAssistantAfterToolResult)
		writeBoolField(b, "RequiresThinkingAsText", c.RequiresThinkingAsText)
		writeBoolField(b, "RequiresReasoningContentOnAssistantMessages", c.RequiresReasoningContentOnAssistantMessages)
		writeStringField(b, "ThinkingFormat", c.ThinkingFormat)
		writeMapField(b, "OpenRouterRouting", c.OpenRouterRouting)
		writeMapField(b, "VercelGatewayRouting", c.VercelGatewayRouting)
		writeBoolField(b, "ZaiToolStream", c.ZaiToolStream)
		writeBoolField(b, "SupportsStrictMode", c.SupportsStrictMode)
		writeStringField(b, "CacheControlFormat", c.CacheControlFormat)
		writeBoolField(b, "SendSessionAffinityHeaders", c.SendSessionAffinityHeaders)
		writeBoolField(b, "SupportsLongCacheRetention", c.SupportsLongCacheRetention)
		b.WriteString("},\n")
	case "openai-responses", "azure-openai-responses":
		b.WriteString("\t\tResponsesCompat: &OpenAIResponsesCompat{")
		writeBoolField(b, "SendSessionIdHeader", c.SendSessionIdHeader)
		writeBoolField(b, "SupportsLongCacheRetention", c.SupportsLongCacheRetention)
		b.WriteString("},\n")
	case "anthropic-messages":
		b.WriteString("\t\tAnthropicCompat: &AnthropicMessagesCompat{")
		writeBoolField(b, "SupportsEagerToolInputStreaming", c.SupportsEagerToolInputStreaming)
		writeBoolField(b, "SupportsLongCacheRetention", c.SupportsLongCacheRetention)
		b.WriteString("},\n")
	}
}

func hasCompat(c compatEntry) bool {
	return c.SupportsStore != nil || c.SupportsDeveloperRole != nil || c.SupportsReasoningEffort != nil || c.SupportsUsageInStreaming != nil || c.MaxTokensField != "" || c.RequiresToolResultName != nil || c.RequiresAssistantAfterToolResult != nil || c.RequiresThinkingAsText != nil || c.RequiresReasoningContentOnAssistantMessages != nil || c.ThinkingFormat != "" || c.OpenRouterRouting != nil || c.VercelGatewayRouting != nil || c.ZaiToolStream != nil || c.SupportsStrictMode != nil || c.CacheControlFormat != "" || c.SendSessionAffinityHeaders != nil || c.SupportsLongCacheRetention != nil || c.SendSessionIdHeader != nil || c.SupportsEagerToolInputStreaming != nil
}

func writeBoolField(b *strings.Builder, name string, value *bool) {
	if value != nil {
		b.WriteString(fmt.Sprintf("%s: boolPtr(%v), ", name, *value))
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
		"node_modules/@mariozechner/pi-ai/dist/models.generated.js",
		"/usr/local/lib/bun/install/global/node_modules/@mariozechner/pi-ai/dist/models.generated.js",
	}

	// Also check relative to the script location
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
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
