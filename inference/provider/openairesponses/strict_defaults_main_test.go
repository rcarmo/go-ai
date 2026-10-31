package openairesponses

import (
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestResponsesStrictCapabilityDefaultsMain(t *testing.T) {
	enabled, disabled := true, false
	for _, api := range []goai.Api{goai.ApiOpenAIResponses, goai.ApiAzureOpenAIResponses} {
		for _, provider := range []goai.Provider{goai.ProviderOpenAI, goai.ProviderCloudflareAIGateway, goai.ProviderOpenRouter} {
			for _, override := range []*bool{nil, &disabled, &enabled} {
				t.Run(fmt.Sprintf("%s/%s/%v", api, provider, override), func(t *testing.T) {
					model := &goai.Model{ID: "test", Api: api, Provider: provider, ResponsesCompat: &goai.OpenAIResponsesCompat{SupportsStrictMode: override}}
					want := api == goai.ApiAzureOpenAIResponses
					if override != nil {
						want = *override
					}
					compat := getResponsesCompat(model)
					if compat.supportsStrictMode != want {
						t.Fatal("capability default/override", compat, want)
					}
					tool := deferredTool("sample")
					declaration, err := convertResponsesTool(tool, compat.supportsStrictMode, false)
					if err != nil {
						t.Fatal(err)
					}
					if (declaration.Strict != nil) != want || (declaration.Strict != nil && *declaration.Strict) {
						t.Fatal("ordinary strict wire field", declaration.Strict, want)
					}
					tool.Parameters = json.RawMessage(`{"type":"object","properties":{"optional":{"type":"string"}}}`)
					tool.ConstrainedSampling = &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "require"}
					declaration, err = convertResponsesTool(tool, compat.supportsStrictMode, false)
					if !want {
						if err == nil {
							t.Fatal("required strict unsupported capability accepted")
						}
						return
					}
					if err != nil || declaration.Strict == nil || !*declaration.Strict {
						t.Fatal("required strict capability rejected", err, declaration)
					}
					var parameters map[string]any
					if err = json.Unmarshal(declaration.Parameters, &parameters); err != nil {
						t.Fatal(err)
					}
					if parameters["additionalProperties"] != false {
						t.Fatal("schema not tightened", parameters)
					}
				})
			}
		}
	}
}
