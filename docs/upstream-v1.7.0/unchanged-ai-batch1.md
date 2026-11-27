# Unchanged AI originals: batch 1

Pin: `fb0fe823539f6d04850f6e34748c01f01f212e1f`. Native base: `033fea39d5803fad264edca7e67670226be64b30` from `/work/go-ai` main. The worktree is `/work/port-lanes/unchanged-ai-batch1` on `port/v170-unchanged-ai-batch1`.

## Selected unchanged originals

1. `packages/ai/test/abort.test.ts`
2. `packages/ai/test/anthropic-auth-token.test.ts`
3. `packages/ai/test/anthropic-cache-write-1h-cost.test.ts`
4. `packages/ai/test/anthropic-eager-tool-input-compat.test.ts`
5. `packages/ai/test/anthropic-eager-tool-input-e2e.test.ts`
6. `packages/ai/test/anthropic-empty-thinking-signature-compat.test.ts`
7. `packages/ai/test/anthropic-error-tool-result-images.test.ts`
8. `packages/ai/test/anthropic-federation-sdk.test.ts`

All eight source bytes matched their pinned Git blobs and the retained v1.6.1 checkout, byte for byte. The pinned source was executed unchanged with Vitest 4.1.12 and Node 26.13.0 under the shared `/tmp/go-ai/v170-unchanged-ai.lock`, `NODE_OPTIONS=--max-old-space-size=650`, an empty `HOME`, and a fresh network namespace with only loopback enabled. The successful run reported **7 passed files, 1 skipped file; 29 passed tests and 62 credential-gated skips**. The initial run without a loopback interface produced `EADDRNOTAVAIL` in the eager-input compatibility server test and was a harness failure, not an original failure. It was rerun with loopback enabled.

## Native comparison and fixes

The unchanged native Anthropic selection passed. New production-path checks then exposed two gaps:

- `ANTHROPIC_OAUTH_TOKEN` used bearer and Claude CLI request headers but did not emit the `oauth-2025-04-20` beta expected by the pinned auth-token original.
- A context already canceled before the Anthropic request returned only `context canceled`; it did not return the empty `aborted` assistant message required for a follow-up transcript.

The first fix adds the OAuth beta without affecting explicit header-owned auth or `ANTHROPIC_AUTH_TOKEN`. The second fix returns an empty, usage-initialized aborted message from the production Anthropic stream while preserving the Go cancellation error. It permits the next user message to continue after the aborted entry.

The cache-delta, eager-input compatibility, signed/empty thinking replay, catalog compatibility, error-tool image routing and federation cases already matched. No production change was made for a passing contract.

## Limits and gaps

- `abort.test.ts`: all 43 cases are credential-gated live provider cases; no live provider was called. Native coverage checks the immediate-abort/follow-up transcript contract only through Anthropic's production transport path, not all provider implementations.
- `anthropic-eager-tool-input-e2e.test.ts`: 19 live provider cases were skipped. Its one catalog-selection case passed. Native eager-input compatibility is covered deterministically, but the live-provider remainder is not claimed.
- SDK-specific identity federation internals are represented by native federation production HTTP behavior, including exchange reuse and explicit-header bypass; this is not an execution of the Anthropic JavaScript SDK inside Go.

Focused Go tests and `go vet` are required for the final receipt; no full acceptance or hosted validation is claimed by this batch.
