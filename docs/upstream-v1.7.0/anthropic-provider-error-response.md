# v1.7.0 Anthropic retry/error response receipt

## Pinned original and bounded receipt

The unchanged original is `packages/ai/test/provider-error-response.test.ts` at official pi v1.7.0 `fb0fe823539f6d04850f6e34748c01f01f212e1f`. The file's SHA-256 is `1d3a280bac6c203f8f207153fc540434919c70b6fd9d3a3f985037fd9a02e927` and its Git blob is `4d6bb10eecfa68c89488999988ba558cafcc4bcf`. The selected Anthropic case was compared with native base `2d605e5b4b07c6680c5ffa5b86eb5a1b25270879`; this lane started as a clean isolated worktree from that exact main commit.

The checked-out source bytes matched both the recorded SHA-256 and the pinned Git object byte for byte. Vitest 4.1.12 on Node 26.13.0 ran the unchanged Anthropic case with one worker and `NODE_OPTIONS=--max-old-space-size=650`, under `/tmp/go-ai/v170-focused-verify.lock`: **1 passed, 4 skipped, 0 failed**. No credential or external provider request was used; the original starts its own loopback HTTP server and returns two deterministic 429 responses.

## Native public-path comparison

Against the unchanged native implementation, `goai.Stream` used the public Anthropic Messages provider registration and intercepted HTTP transport. The existing native control replays the same request body, returns two 429 responses, and verifies that the terminal result is an error, both attempts report to `OnResponse`, each callback observes the corresponding request, and canonical `Retry-After` and `X-Request-Id` headers are preserved. It also covers no-retry and native `RetryConfig` precedence controls from the same transport path.

With `CGO_ENABLED=0`, `GOMAXPROCS=2`, `GOMEMLIMIT=650MiB`, and `GOFLAGS=-p=1`, the public-path selection passed before any native change:

```sh
go test ./tests -run '^TestSourceV161ProviderErrorResponse$/anthropic-messages/' -count=1 -v
```

The focused native selection passed twice under the same controls. `go vet ./tests ./inference/provider/anthropic` and `go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./tests ./inference/provider/anthropic` also passed. The crosswalk checker and assertion validator passed with the pinned upstream checkout and Git mirror; `git diff --check` passed.

No production mismatch was demonstrated, so this receipt changes no provider implementation. The original's four non-Anthropic adapter variants are outside this packet. The [assertion ledger](assertions/provider-error-response.jsonl) records the selected Anthropic loop iteration rather than asserting coverage of every adapter.

This is a bounded local comparison, not full v1.7.0 acceptance, acceptance profiling, live-provider verification, hosted validation or a push receipt.
