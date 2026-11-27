# v1.7.0 Anthropic preferred strict-tool fallback

Scope: the unchanged original `packages/ai/test/anthropic-strict-tool-fallback.test.ts` at `fb0fe823539f6d04850f6e34748c01f01f212e1f`, compared with the native Anthropic Messages production path from `/work/go-ai` main `59c6ed7e4cd8c0ff73236615d345d8d974060d4d`.

The original bytes matched the pinned Git blob: SHA-256 `218e9c5fc8a5c98b0d3d2eb1ff0efd0aa9c142392863d9afe039d60790cd0015`. Vitest 4.1.12 ran it unchanged with one worker and a 650 MiB Node old-space limit: **19 tests passed, 0 failed, 0 skipped**. The suite is deterministic and made no live credentialed request.

The unchanged native Anthropic package passed its existing strict-fallback selection before edits. The existing native controls cover the source's three limit messages, hook ordering and repeated invocation, inline additions/redefinitions, required and locally unsupported boundaries, unrelated errors/statuses, second rejection, hook exceptions, cancellation, SSE errors, sessionless behavior, session cleanup, and remembered-session rejection. No production mismatch was proven, so no Anthropic production code changed.

A new public-adapter control adds the source's final pi-messages case through `goai.Stream`: the intercepted radius request carries the untouched context, the intercepted native Anthropic HTTP attempts observe `strict: true` then absent, and the gateway returns a terminal stop. This is a production transport comparison, not a helper-only substitute.

Verification under `/tmp/go-ai/v170-focused-verify.lock` with `CGO_ENABLED=0`, `GOMAXPROCS=2`, `GOMEMLIMIT=650MiB`, and `GOFLAGS=-p=1` passed:

- `go test ./inference/provider/anthropic -run StrictFallback -count=2`;
- `go vet ./inference/provider/anthropic ./inference/provider/pimessages`;
- Staticcheck v0.8.2 for the same two packages; and
- `git diff --check`.

This is a bounded local receipt, not full v1.7.0 acceptance. No live Anthropic or radius credential case was run or claimed.
