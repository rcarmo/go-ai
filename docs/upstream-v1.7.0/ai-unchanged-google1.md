# v1.7.0 AI unchanged Google batch 1

Pin: `fb0fe823539f6d04850f6e34748c01f01f212e1f`. Native base: clean `f6f191ec74dc7620ee274b890d9371b406400758` on `/work/go-ai` main. The isolated worktree is `/work/port-lanes/unchanged-ai-google1` on `port/v170-ai-batch-google1`.

## Original source

The pinned source was a clean detached checkout at the exact release commit. All five files matched their Git blobs and their SHA-256 fingerprints. No selected source file was copied or modified in the native worktree.

| Upstream test | Git blob | SHA-256 |
| --- | --- | --- |
| `packages/ai/test/google-raw-stop-reason.test.ts` | `c5115cc1a4c11dc4a94f68ba31c8c009fe9f6ee9` | `ceacff5073e435d5c5beeffa4748c1663a9b03154772ca136906d0795e63a59e` |
| `packages/ai/test/google-shared-convert-tools.test.ts` | `81d26b95c4fcf741e65545e4abec23695b14fc03` | `98ce93a20b0e353ebbf6362cd2787429e804fd272a3442a032e7a383b3a668cb` |
| `packages/ai/test/google-shared-gemini3-unsigned-tool-call.test.ts` | `49785b0308e8e079cb7431b6ecd3a559e800b1dc` | `6529e3a32583b7be80b8a78b9b502f359b2925e61f3216d600f78219b734cf87` |
| `packages/ai/test/google-shared-image-tool-result-routing.test.ts` | `05cc1dca4fa7c1247e6e75d9216172f05c4531f0` | `0d18e4a7002af8a010193e87dd519f46bec326883e1d20a9d94ffbe705e8df4b` |
| `packages/ai/test/google-shared-retry.test.ts` | `21b3582ac040f45225f9285467231d6f04ff7e0c` | `97effcdaf3e8d01f8a3b5374fe29d1b2b454556bd84132d5284f0e1fdcf277fb` |

Vitest 4.1.12 ran those five files unchanged with Node 26.13.0, one worker, file parallelism disabled and `NODE_OPTIONS=--max-old-space-size=650`: **5 files and 54 tests passed**. The shared verification lock was `/tmp/go-ai/v170-shared-verification.lock`. No live request or credential was used.

## Native comparison

The unchanged native public Google path was exercised before any lane edit. Existing production-path controls covered raw error/length/tool stop reasons, provider-event forwarding, default/explicit User-Agent, strict tool mode and both adapters' history replay. Existing schema controls covered meta stripping, literal ownership, `const` rewriting and malformed input. Existing retry controls covered retryable status selection, retry limits, delay handling, cancellation and non-replay after an opened stream.

The lane adds three neutral controls for the remaining v1.7.0 assertions:

- `TestGoogleUnchangedV170ImageToolRoutingPublicPayload` captures the production GenAI request payload and checks Gemini 2 separate image turns plus Gemini 3 and prefixed Gemini 4 nested image responses.
- `TestGoogleUnchangedV170WhitespaceOnlyToolTextPublicPayload` checks that whitespace-only results become the exact empty/attached-image response values on the public request path.
- `TestGoogleUnchangedV170HeadersLessRetryErrorObservation` checks the public Google stream's one-attempt 429 path and the empty-header `OnResponse` observation expected for an SDK-style headers-less status.

All new controls passed against the unchanged implementation, so no production edit was warranted. Google raw-object reference identity in JavaScript is represented by ordered value observation through Go's public event hook; Go does not expose mutable JavaScript object identity. The retry helper's callback is represented by the public HTTP adapter response observation, with the same status and empty-header contract.

## Verification and limits

Under the shared lock, `CGO_ENABLED=0`, `GOMEMLIMIT=650MiB`, `GOMAXPROCS=2` and `GOFLAGS=-p=1`, the focused raw/shared selection passed twice with `-count=2`; vet and Staticcheck 0.8.2 passed. The assertion ledger pins 35 cases and 103 source assertion sites: 101 mapped and two partial for JavaScript object-identity-only expectations. The assertion validator matched all selected bytes against both the checkout and pinned Git objects, and the crosswalk checker reproduced the 1,296-site aggregate.

This is a bounded offline source comparison, not v1.7.0 full acceptance, live-credential replay, acceptance profiling, hosted validation or release authorization. No production runtime file changed in this lane.
