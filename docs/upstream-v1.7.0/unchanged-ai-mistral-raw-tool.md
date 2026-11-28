# Unchanged Mistral raw stops and tool schemas

Pin: `fb0fe823539f6d04850f6e34748c01f01f212e1f`. Native base: `f6f191ec74dc7620ee274b890d9371b406400758` from exact `main`. This slice owns only the exact originals:

- `packages/ai/test/mistral-raw-stop-reason.test.ts`
- `packages/ai/test/mistral-tool-schema.test.ts`

## Exact originals

Both files are unchanged from the comparison tag `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe`. Their v1.7.0 SHA-256 values are `d03713e87ccc50f9c375c25bf6b7a2da04584afface472f4affdaf10bc0e8646` and `f17d3113f90c8e9ebc2739920ee2af9007dba1c286a73667c5849859c290f2c8` respectively. The exact Git checkout was clean at the pinned SHA before execution.

Under the shared `/tmp/go-ai/v160-focused-verify.lock`, scrubbed environment and `NODE_OPTIONS=--max-old-space-size=650`, Vitest 4.1.12 passed all 5 original cases:

```sh
node node_modules/vitest/dist/cli.js --run \
  packages/ai/test/mistral-raw-stop-reason.test.ts \
  packages/ai/test/mistral-tool-schema.test.ts
```

The original source run was complete: 2 files and 5 tests passed. It made no live provider request; the stop-reason suite injected a fetch response and the tool-schema suite captured payloads before using a loopback rejection endpoint.

## Native public-path comparison

No production fix was needed. The unchanged native implementation already covered both contracts through the public `goai.Stream` dispatcher and catalog models:

- `TestUpstreamMainMistralRawStopReasons` verifies `stop`, `error` and unknown raw reasons, diagnostics and retryability.
- `TestUpstreamMainMistralToolSchema` verifies the JSON projection of strict nested tool parameters through both the payload hook and wire body, followed by the upstream HTTP error boundary.
- `TestSourceV160MistralStrictModeIsOptIn` verifies explicit `strict:false` for a catalog model without strict-mode support and preserves required fields.

The source symbols from TypeBox have no native object representation. Go accepts JSON-backed tool schemas, so the native port cannot inspect JavaScript `OwnPropertySymbols`; the equivalent assertion is exact preservation of the nested JSON schema and absence of a validation failure on the provider request path. This is the only assertion-level adaptation in the slice.

Under `CGO_ENABLED=0`, `GOMAXPROCS=2`, `GOMEMLIMIT=650MiB`, `GOGC=60`, `GOFLAGS=-p=1` and Go 1.27.3, the focused public-path selection passed twice (`-count=2`, 3 named tests). Focused `go vet` and pinned Staticcheck 0.8.2 passed for `./inference/provider/mistral`; the Staticcheck invocation used the local module cache with network disabled.

This slice is a passing unchanged-source audit. It does not broaden the release acceptance, profiling, publishing or live-provider gates.
