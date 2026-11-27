# Anthropic strict-tool fallback reports the rejected request and remembers sessions

Anthropic reports the strict-tool HTTP 400 before sending its fallback. Payload hooks can replace request fields, but an object replacement keeps `stream: true`. When the adapter falls back after a recognised provider limit rejection, later requests in that session send preferred tools non-strict. Required tools stay strict. Session cleanup restores the initial strict attempt.

## Pinned originals

All 19 unchanged cases in `packages/ai/test/anthropic-strict-tool-fallback.test.ts` passed at official pi v1.6.1 `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe`. The test SHA-256 was `218e9c5fc8a5c98b0d3d2eb1ff0efd0aa9c142392863d9afe039d60790cd0015`. Eleven scratch files matched pinned Git bytes: the test, Anthropic and Pi Messages adapters, strict-limit and constrained-sampling helpers, session cleanup, error-body, retry and transcript helpers, Vitest config and AI package manifest. Node 26.13.0 and Vitest 4.1.12 ran the originals offline in a scrubbed checkout:

```sh
node ../../node_modules/vitest/vitest.mjs run test/anthropic-strict-tool-fallback.test.ts \
  --pool=forks --maxWorkers=1 --reporter=json
```

## Public Go comparisons

The eight selected originals use `goai.Stream` and an inert transport. The three positive limit-rejection variants check `[400, 200]` response hook order, the `start` and `done` events, both payload hooks, preserved request fields and schemas, untouched transcript and fallback diagnostic. The four session cases check sessionless requests, a remembered session versus another session, targeted cleanup and a subsequent required-tool rejection. The hook-exception case makes `OnPayload` return a native `ProviderRequestError` with the same HTTP status and provider error fields as the wire fixture; this calls no transport and adds no fallback diagnostic. JavaScript `undefined` maps to an absent JSON field or a nil Go diagnostic slice. An empty Go `SessionID` denotes the absence of a session.

Compiled against unchanged Go base `a872d0da5c5b5634c6ac377f2ee0e0b9d28aeed0`, all three limit variants got only `[200]` and sent `[false, false]` stream flags. The remembered-session row retried strict on the subsequent request, and the required-tool row made four HTTP requests instead of three. Sessionless requests and the typed hook-exception row passed. The cleanup row also passed on the base with no session state; a compiled control later showed that the fixed test detects a missing cleanup.

The scoped assertion ledger maps 28 direct sites across six case records (60 applicable variant comparisons). The Go cases also check that the 400 callback precedes the second payload hook. Two supplemental tests cover cancellation from the newly reported response hook and the source's 10,000-session FIFO cap, targeted cleanup and all-session cleanup.

## Compiled controls

Seven changes failed only the targeted tests: omit the rejected-response callback, duplicate the terminal callback, accept `stream: false`, skip recording the rejected session, skip targeted cleanup, ignore the payload callback error, or evict one entry too late. After byte restoration, the focused tests, full Anthropic and strict-tool helper packages, related root tests, vet and Staticcheck 0.8.2 passed with Go 1.27.3 and the shared verification lock:

```sh
unshare -Un go test ./inference/provider/anthropic \
  -run '^(TestSourceV161AnthropicStrictFallback|TestAnthropicStrictFallback(ResponseAbort|SessionCap)V161)' \
  -count=3 -shuffle=42 -p 1 -timeout 40s
unshare -Urn sh -ec 'ip link set lo up; exec "$@"' sh go test \
  ./inference/provider/anthropic ./internal/stricttools -count=1 -shuffle=42 -p 1 -timeout 75s
unshare -Urn sh -ec 'ip link set lo up; exec "$@"' sh go test . ./tests \
  -run '(Anthropic|Provider.*Retry|SessionResources)' -count=1 -shuffle=42 -p 1 -timeout 65s
unshare -Un go vet -p 1 ./inference/provider/anthropic ./internal/stricttools
```

This packet changes the Anthropic Messages adapter. Shared Bedrock session memory and the Pi Messages gateway case are outside its scope. The transport tests do not use a live service.
