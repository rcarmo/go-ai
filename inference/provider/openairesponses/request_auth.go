package openairesponses

import (
	goai "github.com/rcarmo/go-ai"
	"net/http"
	"strings"
)

func hasResponsesAuthHeader(model *goai.Model, headers map[string]string) bool {
	if goai.HasOpenAIAuthHeader(headers) {
		return true
	}
	if model.Api == goai.ApiAzureOpenAIResponses {
		for key, value := range headers {
			if strings.EqualFold(key, "api-key") && strings.TrimSpace(value) != "" {
				return true
			}
		}
	}
	return false
}

// SDK-generated authentication precedes client default/model headers. Caller
// headers and suppression are applied later by the request builder.
func applyResponsesAuthHeaders(req *http.Request, model *goai.Model, apiKey string) {
	if apiKey != "" {
		switch {
		case model.Api == goai.ApiAzureOpenAIResponses:
			req.Header.Set("api-key", apiKey)
		case model.Provider == goai.ProviderCloudflareAIGateway:
			req.Header.Set("cf-aig-authorization", "Bearer "+apiKey)
		default:
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}
	for key, value := range model.Headers {
		if strings.EqualFold(key, "Authorization") || strings.EqualFold(key, "cf-aig-authorization") || (model.Api == goai.ApiAzureOpenAIResponses && strings.EqualFold(key, "api-key")) {
			req.Header.Set(key, value)
		}
	}
}

// Azure SDK 7.19.0 disables redirect following for api-key requests. Go's
// standard client does not classify api-key as a sensitive redirect header.
func protectAzureAPIKeyRedirects(client *http.Client, req *http.Request, model *goai.Model) {
	if model.Api == goai.ApiAzureOpenAIResponses {
		if _, present := req.Header[http.CanonicalHeaderKey("api-key")]; present {
			client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
		}
	}
}
