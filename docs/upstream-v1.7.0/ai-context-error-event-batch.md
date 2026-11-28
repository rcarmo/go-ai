# v1.7.0 AI context, error-body and event-stream originals

This bounded lane owns three unchanged deterministic AI originals at `fb0fe823539f6d04850f6e34748c01f01f212e1f`, disjoint from the credentialed, catalog and provider-specific lanes:

| Upstream test | SHA-256 | Git blob |
| --- | --- | --- |
| `packages/ai/test/context-estimate.test.ts` | `ea24bb2285fd3364a9712ab39a13937a93acac7efafa7533547d559ac43003ea` | `ffd532234b70b2af9c234795ace97ddfcdb831f7` |
| `packages/ai/test/error-body.test.ts` | `99555b5788c823d0880b9937093218b23c49fde8ed07dea8c7c4cf4b01e19d7f` | `b6e2367704ed03bc2bc32e40909dc22e019905a7` |
| `packages/ai/test/event-stream.test.ts` | `3e8e8bc39b33c79be9858bd3210212658d75de196222c69b61a5a183737ab6d9` | `39d3248730afa13fbf0c6da8a822f73e28220638` |

The files were byte-verified with SHA-256 and `git hash-object`, matched the detached exact-pin checkout, and were run unchanged. Vitest 4.1.12 under the shared verification lock and a 400 MiB Node old-space limit passed 3 files and 33 tests. No credentials or live requests were used; the OpenAI connection test contacted only a closed loopback listener in the unchanged source.

## Native comparison

The unchanged implementation passed the translated context-estimation/output-clamp controls and the existing public error-normalization controls. Context estimates preserve the latest valid usage anchor, invalidate stale assistant usage after a later inserted prefix, reserve 3.5 UTF-16 characters per token, and derive the same 2,904- and 4,755-token output ceilings. Error-body tests already covered provider status/body extraction, plain-object serialization, opaque SDK-wrapper rejection, bounded truncation, composed diagnostics and real loopback connection failures through the public request path.

The unchanged implementation failed `TestSourceV170EventStreamStopsAtFirstTerminal`: public `Stream` exposed four provider events instead of stopping after the first done event, so `Complete` could also replace the first terminal result with a later one. The fix wraps the provider channel and delivers events only through the first `DoneEvent` or `ErrorEvent`, closes the public channel immediately, and continues draining the private provider channel to avoid blocking its producer. Constrained to the public request path, the change preserves pre-terminal order and terminal-result ownership without introducing a second stream abstraction.

The JavaScript generic stream's per-consumer iterator registration and explicit `end()`-without-result contract have no native public analogue; Go exposes one ordered provider channel. The mapped public controls cover the corresponding buffered-order, terminal-close and post-terminal suppression behavior. Node heap retention in the excerpt test is represented by the bounded Go truncation result rather than a claim of cross-runtime GC identity.

## Verification

From exact native main `d557b0a191976d8bf0c78e54488cf02162dfe062`, with `CGO_ENABLED=0`, `GOMEMLIMIT=650MiB` and `GOMAXPROCS=2`:

- the new event test failed before the production edit, proving the terminal-ordering gap;
- context, public event-stream and provider-error focused controls passed twice across the root and `./tests` packages;
- `make vet` passed;
- `make staticcheck` (Staticcheck v0.8.2) passed;
- `git diff --check` passed.

This is a bounded deterministic parity receipt, not full v1.7.0 acceptance, release authorization, or live-provider verification.
