# Report each failed HTTP attempt to response hooks

The five pinned adapters report both HTTP 429 responses when one retry is allowed. OpenAI Completions, OpenAI Responses, Azure Responses and Anthropic include `Retry-After: 0` and `X-Request-Id: req-1`. The standard Google Generative AI path reports the status with an empty header map, matching the upstream SDK error object. The standard GenAI HTTP transport still uses the actual headers to make retry decisions.

## Pinned original

All five variants of `packages/ai/test/provider-error-response.test.ts` passed at `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe`. The test SHA-256 was `1d3a280bac6c203f8f207153fc540434919c70b6fd9d3a3f985037fd9a02e927`. Twelve staged files, including the test, participating adapters, retry helper and root lockfile, were byte-compared with pinned Git. Installed versions matched the lock: OpenAI 7.59.0, Anthropic 0.129.0, Google GenAI 3.6.1 and Vitest 4.1.12.

The unchanged Node 26.13.0 run used a scrubbed environment, disposable home and a network namespace with only loopback enabled. It ran `node ../../node_modules/vitest/vitest.mjs run test/provider-error-response.test.ts --pool=forks --maxWorkers=1 --reporter=json` from the staged `packages/ai` directory.

## Native response hooks

On base `5c86bc760aabb93fe20e9310d610931ad7c5a016`, Completions, both Responses APIs and Anthropic made one request and delivered one callback despite `MaxRetries=1`. Google made two requests, delivered only the final callback, and exposed the headers. All five returned the error stop. Four of the five no-retry controls already passed; Google still exposed headers.

The scoped retry entry point uses an explicit `MaxRetries` on these adapters, preserves native `RetryConfig` precedence, and reports responses discarded by the retry loop. The provider remains responsible for the final response hook. Existing retry classification, the native Google `RetryConfig` path, Vertex, and other callers do not change. The Go hook keeps its canonical HTTP header spelling. Native error stops are observed on the public stream's terminal event.

The restored suite used Go 1.27.3 and the shared verification lock. The exact `unshare -Un go test ./tests -run '^TestSourceV161ProviderErrorResponse$' -count=2 -shuffle=42 -p 1 -timeout 45s` covered all five adapter rows, zero-retry controls and native-config precedence. The inert transport required the expected POST, byte-identical retry body and a hook before the next request. Four compiled controls independently skipped the explicit retry count, lost the intermediate callback, exposed Google error headers and overrode native config precedence. Each failed only its intended leaves. Full OpenAI, Responses, Anthropic and Google provider packages passed in loopback-only isolation. Focused retry and hook tests passed twice; vet and Staticcheck 0.8.2 passed for the affected packages.

This covers scripted HTTP 429 responses and the accompanying header observations. It does not establish parity for every provider status, SDK error representation, live service or other retry policy.
