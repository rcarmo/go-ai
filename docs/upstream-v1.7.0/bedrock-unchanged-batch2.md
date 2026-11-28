# Bedrock unchanged originals: batch 2

Exact target: upstream `fb0fe823539f6d04850f6e34748c01f01f212e1f`; exact native base: `cbca8ba81df8a13ee1c88521a078203540a637b1`.

## Original and native checks

The six unchanged originals were verified as exact pinned Git blobs and SHA-256 inputs, then run unchanged on Node 26.13.0 and Vitest 4.1.12: **6 files, 23 tests passed, 0 failed**. No AWS credentials or live Bedrock requests were used. The model-extensiveness branch therefore stayed disabled, while the deterministic catalog assertions still ran.

| Original | Git blob | SHA-256 |
| --- | --- | --- |
| `bedrock-error-metadata.test.ts` | `4df3d83105bc7f6c83dcdc8645e451881f8f95e3` | `419813d91e53ec4365b88f47b16b1e11b284fe35e949bcd2bcf9e7f185413fcc` |
| `bedrock-models.test.ts` | `02e8b8f16b4baabc553b91dd8ffe09b61c83bfa2` | `adb580d3fd5fba8e916f540bdd6711bdc65a977714bc411115cf5fff6f008550` |
| `bedrock-proxy-credentials.test.ts` | `b006fbdb8553c167193200dd07903214c4a46197` | `f6103976cf86780a805462ad0167d47a68f25b2ee8dccd9c24d7757d16637298` |
| `bedrock-raw-stop-reason.test.ts` | `c9dcac7537a7db753eb40f7e493a0eb043a16e67` | `28e7ff81d411329ea5c2fb9f2f7382c0ff5cbc409eff13ef57a90fa112bc08d5` |
| `bedrock-redacted-reasoning.test.ts` | `f9dc58a299d29ef3750a6afd2adf5d48b5c50f46` | `f1f2e4d27c8f3cbf47da5712012d9a12c6580fbd93a69636d9744c7600aee896` |
| `bedrock-response-headers.test.ts` | `abee0fabcbc0d92e5e1e4fe9bbcf6d7bf70af5c2` | `8436eacf232697682ecf4efcddf86e5dec216f9ea83fe5be728409adfdd82fa4` |

Native public-path tests exercise `goai.Stream`: modeled 400 metadata and retry-classification text, pre-cancel abort without diagnostics, raw event order, successful and provider-error raw stop reasons, split and EOF-finalized redacted reasoning, request replay before a tool use, and raw response headers before an empty-stream failure. The catalog assertion uses registered lookup. The existing isolated profile-credential test verifies that SSO retrieval CONNECTs through the configured proxy.

All translated controls passed unchanged before any production edit. A test-harness compile correction replaced a nonexistent `GetModels` call with `ListModels`; a replay assertion was then corrected to inspect the assistant member without assuming Bedrock emits no synthetic following tool-result member. These were test-port corrections, not native implementation failures. No production field or behavior was added, because no scoped gap was demonstrated.

The focused Bedrock selector passed twice with shuffled order. Scoped `go vet ./inference/provider/bedrock` passed, and pinned Staticcheck v0.8.2 passed for that package. All runs used Go 1.27.3, `CGO_ENABLED=0`, `GOMAXPROCS=2`, `GOFLAGS=-p=1`, `GOGC=60`, `GOMEMLIMIT=650MiB`, and the shared verification lock at `/tmp/go-ai/v170-shared-verification.lock`.

## Limits

- The extensive model-availability loop is live-credential/region behavior and was not claimed by the 23 local deterministic originals.
- JavaScript mock-object identity is represented by native raw-event order and exact SDK union types, not by JavaScript reference identity.
- The bare-object modeled mid-stream failure is represented by native stream-error and diagnostic extraction controls; Go's SDK error representation is not the same object shape.
- No broader acceptance, profiling, hosted CI, release, push, or live-provider claim is made by this packet.
