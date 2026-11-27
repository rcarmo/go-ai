# OpenAI Completions tool-history replay

The unchanged pi v1.7.0 `packages/ai/test/openai-completions-empty-tools.test.ts` matched Git blob bytes and SHA-256 `852e18e7433439f3cef792cbc129774839fa6f2e9e24d73c8523bb12dd246020`. Vitest 4.1.12 on Node 26.13.0 ran all 12 cases: 12 passed, with no failures or skips. The checkout remained byte-clean after execution. The release delta itself only replaces three Cloudflare Gateway model locators; the selected tool-history cases are unchanged executable contracts.

The native audit ported the two tool-history cases through the OpenAI Completions production request builder. On the unchanged native base, the required compatibility field did not exist and the builder emitted `tools: []` for any prior tool history. The new explicit compatibility flag defaults to false, is merged from model metadata, and emits an empty array only when `requiresToolsWithToolHistory` is true.

`TestOpenAICompletionsEmptyToolsUpstream` passed twice in the full `./inference/provider/openai` package. Focused `go vet` and Staticcheck v0.8.2 passed. A compiled scratch control restored unconditional tool-history emission; the default-omit case failed on `tools should be omitted`, proving the assertion rejects the original semantic overreach. The source tree was restored before further work.

This comparison is limited to OpenAI Completions request serialization for an empty active tool list. It does not run provider credentials or claim coverage of the other changed v1.7.0 test-file deltas, catalog-only locator updates, Durable storage, Radius, Bedrock, Pi Image, Pi Decision, OpenRouter, or Ant Ling.
