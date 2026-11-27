# Anthropic keeps the dialect on non-strict tool schemas

Anthropic Messages now keeps the declared `$schema` when serialising a non-strict tool. A draft-07 tuple still uses an `items` array, and Anthropic receives the dialect needed to interpret it. The adapter still filters unsupported root combinators and does not mark the tool strict.

## Pinned originals

All four unchanged cases in `packages/ai/test/anthropic-strict-tool-schema.test.ts` passed at official pi v1.6.1 `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe`. The test SHA-256 was `eeb7df219677ca888be09eccd95aed2ba7e1d79da99d2fe49ad1124506829afd`. Ten scratch files matched pinned Git bytes, including the test, Anthropic adapter, constrained-sampling and strict-limit helpers, types, session resource cleanup, transcript normaliser, retry helper, Vitest config and AI package manifest. Node 26.13.0 and Vitest 4.1.12 ran the originals offline in a scrubbed checkout:

```sh
node ../../node_modules/vitest/vitest.mjs run test/anthropic-strict-tool-schema.test.ts \
  --pool=forks --maxWorkers=1 --reporter=json
```

## Native comparison

On unchanged Go base `c85b3c311b5dba5a936485c9db68ee11ed3ba3d4`, the compiled public `Complete` test preserved the tuple but omitted its `$schema` in both the payload callback and HTTP request. The fixed test checks the original draft-07 schema and omitted `strict` key. It first stops at `OnPayload`, like the original, then captures the production request with an inert transport. The second check includes definitions, `additionalProperties`, and unsupported root keywords to check the allowlist. Only the two assertion sites in the changed original are mapped.

## Compiled controls

Four controls failed the relevant targets: omit `$schema`, rewrite `items` to `prefixItems`, allow root `oneOf`, or mark a non-strict tool strict. After byte restoration, the focused Go tests, full Anthropic and strict-tool helper packages, vet and Staticcheck 0.8.2 passed with Go 1.27.3 and the shared verification lock:

```sh
unshare -Un go test ./inference/provider/anthropic \
  -run '^(TestSourceV1(60AnthropicStrictToolSchemaWire|61AnthropicNonStrictTupleDialect)|TestAnthropicNonStrictTupleRootFilteringV161)$' \
  -count=3 -shuffle=42 -p 1 -timeout 45s
unshare -Urn sh -ec 'ip link set lo up; exec "$@"' sh go test \
  ./inference/provider/anthropic ./internal/stricttools -count=1 -shuffle=42 -p 1 -timeout 75s
unshare -Un go vet -p 1 ./inference/provider/anthropic ./internal/stricttools
```

The change is limited to legacy schema projection. It does not change strict-schema validation, fallback policy or hooks. No live Anthropic request was sent.
