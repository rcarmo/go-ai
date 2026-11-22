# Native fetch-option translations

The six [pinned originals](fetch-option.md) have public native transport probes in [`TestSourceV160FetchOption`](../../../tests/fetch_option_v160_test.go). Streaming uses the public `StreamSimple` adapter and `StreamOptions.HTTPTransport`; image generation uses public `GenerateImages` and `ImagesOptions.HTTPClient`.

| Original line | Native subtest | Exact checks |
| --- | --- | --- |
| 63 | `anthropic` | One request-local call, no ambient or loopback calls. |
| 73 | `openai-adapters` | One local call each for Completions, Responses and Azure Responses; three total, no ambient or loopback calls. |
| 103 | `direct-http` | One local call each for Mistral, Codex SSE and pi-messages; three total, no ambient or loopback calls. |
| 125 | `google-custom` | One local call for each Google adapter under the test base URL, the provider rejection text in both results, no ambient or loopback calls. |
| 149 | `google-ambient` | Without a request-local override, both adapters invoke the same ambient Go transport once, with no loopback call. Go exposes no separate Undici request dispatcher field. |
| 167 | `image-generation` | One request-local client call for OpenRouter images, no ambient or loopback calls. |

The fixture confirms that each request is a POST under the chosen test base URL, and that the ambient client and transport are unchanged after the request. Before the fix, the `openai-adapters` group used loopback three times and never called the local transport. The `direct-http` group used loopback twice and the ambient transport once, again with no local calls. Anthropic, both Google paths and image generation already passed after translating the Google error to its Go-native field.

The missing HTTP adapters now set their own client transport when the option is present. Pi-messages copies its ambient client before overriding the transport. With no override, all affected adapters keep their existing client path; WebSocket dialing is unchanged. Removing the Responses transport assignment again failed its OpenAI and Azure native comparisons: both went to loopback and the three-adapter group saw only one local call.

The six unchanged originals reproduced the exact offline receipt. The focused native and existing related tests passed twice with Go `1.27.3`, `CGO_ENABLED=0` and `-p 1`:

```sh
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES='./tests ./inference/provider/google' \
  TEST_FLAGS='-run "TestSourceV160FetchOption|TestSourceV160GoogleContinuationAndHTTPTransport|TestProviderErrorBodyPassthrough|TestGenerateImagesOpenRouterHooksAndResponse" -count=2 -shuffle=42 -p 1 -timeout 90s'
```

Focused vet, Staticcheck `0.8.2` and diff checks passed. These comparisons cover only the listed request paths. Live credentials, platform variants and unrelated transport behavior remain separate; no exclusions or package-wide acceptance are implied.
