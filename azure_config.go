package goai

import (
	"fmt"
	"net/url"
	"strings"
)

// ResolveAzureConfig resolves the per-request resource endpoint; catalog models
// deliberately carry no tenant resource. Deployment changes affect wire model
// identity only, leaving catalog/response attribution unchanged.
func ResolveAzureConfig(model *Model, opts *StreamOptions) (baseURL, deployment, version string, err error) {
	env := ProviderEnvFromOptions(opts)
	version = "v1"
	deployment = model.ID
	if opts != nil && opts.AzureAPIVersion != "" {
		version = opts.AzureAPIVersion
	} else if value := GetProviderEnvValue("AZURE_OPENAI_API_VERSION", env); value != "" {
		version = value
	}
	if opts != nil && opts.AzureDeploymentName != "" {
		deployment = opts.AzureDeploymentName
	} else {
		for _, entry := range strings.Split(GetProviderEnvValue("AZURE_OPENAI_DEPLOYMENT_NAME_MAP", env), ",") {
			pair := strings.SplitN(strings.TrimSpace(entry), "=", 2)
			if len(pair) == 2 && strings.TrimSpace(pair[0]) == model.ID && strings.TrimSpace(pair[1]) != "" {
				deployment = strings.TrimSpace(pair[1])
			}
		}
	}
	if opts != nil {
		baseURL = strings.TrimSpace(opts.AzureBaseURL)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(GetProviderEnvValue("AZURE_OPENAI_BASE_URL", env))
	}
	resource := ""
	if opts != nil {
		resource = opts.AzureResourceName
	}
	if resource == "" {
		resource = GetProviderEnvValue("AZURE_OPENAI_RESOURCE_NAME", env)
	}
	if baseURL == "" && resource != "" {
		baseURL = "https://" + resource + ".openai.azure.com/openai/v1"
	}
	if baseURL == "" {
		baseURL = model.BaseURL
	}
	if baseURL == "" {
		//lint:ignore ST1005 Exact upstream Azure configuration diagnostic.
		return "", "", "", fmt.Errorf("Azure OpenAI base URL is required. Set AZURE_OPENAI_BASE_URL or AZURE_OPENAI_RESOURCE_NAME, or pass azureBaseUrl, azureResourceName, or model.baseUrl.")
	}
	parsed, e := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if e != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", "", "", fmt.Errorf("invalid Azure OpenAI base URL: %s", baseURL)
	}
	azure := strings.HasSuffix(parsed.Hostname(), ".openai.azure.com") || strings.HasSuffix(parsed.Hostname(), ".cognitiveservices.azure.com") || strings.HasSuffix(parsed.Hostname(), ".ai.azure.com")
	path := strings.TrimRight(parsed.Path, "/")
	if azure && (path == "" || path == "/openai" || path == "/openai/v1/responses") {
		parsed.Path = "/openai/v1"
		parsed.RawQuery = ""
		parsed.RawPath = ""
	}
	return strings.TrimRight(parsed.String(), "/"), deployment, version, nil
}
