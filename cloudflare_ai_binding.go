package goai

import "fmt"

// CloudflareGatewayBindingAuthSentinel mirrors pi-ai's placeholder value for
// pre-authenticated Workers AI binding requests routed through AI Gateway. Use
// it as the bearer token for cf-aig-authorization while suppressing SDK auth
// headers so no BYOK provider key is sent over the binding transport.
const CloudflareGatewayBindingAuthSentinel = "cloudflare-gateway-binding"

// CloudflareAIBinding is the minimal Go structural equivalent of pi-ai's
// Workers AI binding fetch surface. It deliberately avoids depending on
// Cloudflare worker-specific types while still allowing early validation.
type CloudflareAIBinding interface {
	Fetch(input any, init any) (any, error)
}

// CreateCloudflareAIBindingFetch validates and returns a binding-backed fetch
// adapter. Go does not have JavaScript's global FetchFunction type, so callers
// wire the returned closure into their own Worker/wasm interop layer; requests
// are passed through untouched.
func CreateCloudflareAIBindingFetch(binding CloudflareAIBinding) (func(input any, init any) (any, error), error) {
	if binding == nil {
		return nil, fmt.Errorf("create Cloudflare AI binding fetch: the AI binding does not expose fetch()")
	}
	return binding.Fetch, nil
}
