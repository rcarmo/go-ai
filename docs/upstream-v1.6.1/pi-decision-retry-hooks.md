# Pi Decision reports retry response headers

Pi Decision calls `OnResponse` once for every HTTP response before reading its body, including responses discarded for retry. A refusing hook closes the body without reading it or replaying the request. The header-phase helper keeps callback errors out of retry classification and returns the original error when the retry context is still active. This also applies to llama.cpp completion responses, which already used the helper. Other classifiers retain their previous hook timing.

## Pinned evidence

Seven files matched official pi v1.6.1 `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe`: the Pi Decision test and adapter, shared classifier transport, retry and error-body helpers, Vitest config and AI package manifest. All five unchanged `test/pi-decision.test.ts` cases passed with Node 26.13.0 and Vitest 4.1.12, offline in a scrubbed scratch checkout. The [original receipt](pi-decision.md) lists the test digest and existing direct comparisons.

A separate instrumented copy of the test recorded `request:503 → hook:503 → read:503 → request:200 → hook:200 → read:200`. It used `retry-after-ms: 0`, identical replay bytes and final response ID `gen-dec-1`. A refusing hook recorded only `request:503 → hook:503` and returned an error. These captures supplement the original final-error hook case; there are no new direct assertion sites.

## Native checks

On helper base `7e4abce22cbea6a2ddb5912ab502ed87c260d002`, the compiled public Go test read and closed the 503 before any callback, then replayed even when the first callback refused. The fixed test records `request:503 → hook:503 → read:503 → close:503 → request:200 → hook:200 → read:200 → close:200`. It checks the retry header, identical replay, final response ID and approval probability `0.96`. Both a generic refusal and a refusal of type `*ProviderRequestError` stop after `request:503 → hook:503 → close:503`.

Three compiled controls detected a disabled header phase, a duplicate terminal callback and a retryable error escaping the refusal wrapper. Removing the wrapper failed both public typed-refusal tests (Pi Decision and llama.cpp) and the helper test: two requests, one retry callback and two closes instead of one request, no retry callback and one close. The helper test checks exact error identity and zero body reads. The restored tests passed with Go 1.27.3 in isolated network namespaces, as did the classifier and provider-retry tests, vet and Staticcheck 0.8.2:

```sh
unshare -Un go test . ./tests \
  -run '^(TestPiDecisionRetryResponseHookV161|TestSourceV161PiDecision.*|TestSourceV161LlamaCPPResponseHooks|TestProviderResponseHookPreservesTypedRefusalV161)$' \
  -count=3 -shuffle=42 -p 1 -timeout 45s
unshare -Urn sh -ec 'ip link set lo up; exec "$@"' sh go test . ./tests \
  -run '(Classifier|Decisions|PiDecision|LlamaCPP|Provider.*Retry|ProviderErrorResponse)' \
  -count=1 -shuffle=42 -p 1 -timeout 55s
unshare -Un go vet -p 1 . ./tests
```

The assertion ledger validator checked 665 existing locators against pinned Git bytes. The tests use inert transports and a private loopback namespace. No live service was tested.
