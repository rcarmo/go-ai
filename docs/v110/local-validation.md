# Native 1.1.0 local validation

The uncommitted native 1.1.0 candidate passes local normal/race, catalog, static, security and public-consumer gates on Go 1.27.1. No commit, push, tag or publication is authorised or performed.

## Candidate and scope

* Working base: `1a5a1cf3abd96afbe07d64d5eb5b6b421ca4789b`. Results include uncommitted production, generated catalog and test changes; this base SHA alone does not identify the candidate runtime.
* Official target: `abe508e1b89912adde45528136c3221eb69acdd7`, compared with accepted official 1.0.4 `7c10bd4337495ee613f2224843ecdf349b80d1df`.
* Published native 1.0.4 runtime: `65e25029c969d154560d0c26eec032d2605d80da`; stash: `9be4ebbd0c1de275d69bd95a4f1fc3db78f00e74`. HEAD/origin/main, published tag and stash are preserved.
* Toolchain: `go1.27.1 linux/amd64`; `GOTOOLCHAIN=local`. Cache/scratch: `/home/agent/tmp/go-ai`, resolved through `PROJECT_TMP_BASE=/home/agent/tmp` and `scripts/project-env.sh`.
* Rui removed durable legacy API/journal compatibility as an acceptance requirement. Native exported APIs, Go channels and sqlite3/WAL remain documented adaptations; Cloudflare Durable Object platform execution is not supplied.
* [All 80 changed paths](changed-paths-crosswalk.md), [216 official suites](test-corpus-crosswalk.md), [provenance](provenance.json), [catalog delta](catalog-delta.json).

## Local gates

| Gate | Result |
| --- | --- |
| `make test TEST_FLAGS='-shuffle=on'` | All 19 test-bearing packages pass. Durable 51.989s; durable/tools 4.173s. CPU, alloc_space and alloc_objects analysed package-by-package; raw captures disposed |
| `make test-race TEST_FLAGS='-shuffle=on'` | All 19 test-bearing packages pass. Durable 327.459s; durable/tools 25.744s. Profiles analysed/disposed |
| `make test-deterministic` | Final full count3 suite passes all19 test-bearing packages; durable168.826s/tools12.551s/tests14.257s, with CPU/allocation analysis and disposal |
| Late focused additions | Profiled `TestModels110` race/shuffle passes 1.104s; includes host credential/request boundary and ended-capability fencing. Profiled SQLite ordered scan/reopen race/shuffle passes3.047s. Conversation history defaults ascending; direct Storage defaults descending. Its final focused context race/shuffle/profile and durable vet pass. Profiled replay duration race/shuffle passes. Codex/Anthropic new-edge race count3 passes 1.043s |
| `golang.org/x/text v0.41.0` | Replaces v0.39.0 for GO-2026-6629. Affected durable/tools full race/shuffle/profile passes 26.224s after upgrade. No vulnerability exception added |
| Catalog regeneration | Exact pinned 1.1.0 chat/image/classifier full-record comparators pass: 1563 / 61 / 26. Deliberate non-ID metadata corruptions are rejected. Offline schema-v6 hydration is reproducible; twelve fail-closed atomic faults pass |
| Static/build | Repository vet, staticcheck v0.8.1, logging quality, cgo/default test builds and non-CGO `build ./...` pass. `git diff --check` passes |
| Security/licence | Pinned govulncheck v1.7.0: no reachable vulnerabilities on Go1.27.1. Policy negative self-tests pass. Licence check passes; assembly dependency-inspection warnings remain for websocket/compress |
| SBOM | CycloneDX1.6, 19 components, normalisation/checksum/schema/VCS validation passes. Candidate SBOM SHA256 `ea7339ebcd19949e327813cabc97ef54f0878d202c83f57d434f42e1c7978d66`. Version/VCS identify the working base, not an immutable 1.1.0 release. SBOM/normaliser/publisher self-tests pass; no release published |
| Fuzz | Five 30s runs pass: partial JSON, SSE, context round-trip, message transformation and overflow classification. Child CPU/allocation profiles analysed and disposed |
| External consumer | Separate replacement-module public consumer race/shuffle passes 1.094s. Exercises Catalog, required storage scans, as-of context, optional durations, classifier images and login agentName. Fixtures/module removed after use; this is not clean-clone/hosted acceptance |
| Portable temp/inventory gates | Project temp self-test, historical v0850/v0851/v0870/v0871 inventory/catalog validators and negative tests pass. Generic fallback routing remains checked |

Full shuffled suites precede the x/text fix and final small model-request/OAuth/SQLite/replay additions. The affected packages and static/build checks were rerun, followed by the required full deterministic count3 gate. The last conversation-history default-order correction has its own profiled focused race and public-consumer checks. Aggregate `make check`/`test-repro` aliases were not invoked to repeat already passing gates; their listed components were exercised serially.

## Profiling and tuning

Equivalent boundary benchmark, Go1.27.1, two-second runs, identical progress message: producer timing plus two snapshots costs 430.1ns/op, 1536B/op, 8allocs/op; one producer snapshot costs 234.6ns/op, 768B/op, 4allocs/op. CPU and allocation histories identify `SnapshotEvent`/`cloneMessage`. OpenAI's redundant pre-send snapshots were removed because the sender already snapshots on the producer goroutine. Progress ownership and terminal timing assertions still pass. These are boundary measurements, not a claimed end-to-end throughput improvement.

Full normal durable profile: CPU36.70s, alloc_space7909.13MB, alloc_objects119660975. Full race durable profile: CPU306.94s, alloc_space9135.49MB, alloc_objects133055300. Final count3 durable profile: CPU117.67s, alloc_space23807.53MB, alloc_objects355787334. The leading application chains are `Session.commit`, `prepareWithReferences`, strict JSON encoding/decoding and detached task/state ownership. Race and normal workloads are distinct; their totals are not a speedup comparison. Removing ownership/codec validation to reduce these totals would weaken contracts, so no such shortcut was taken. Cached contexts derive only new contributions and reorder the open assistant suffix; no independent measured whole-context speedup is claimed.

Very short packages have zero CPU samples despite valid capture files; those captures provide no CPU hotspot evidence. Heap/allocation histories and longer production/test packages were analysed. Generator hydration and fault probes also profile helper work. Raw profiles, binaries, traces, fixtures and disposable logs are deleted after use; only concise findings are retained.

## Diagnosed failures

* The first full durable attempt hit a 180s package timeout; final verification uses the existing wrapper timeout. Three-second behaviour deadlines were not widened.
* Timestamp additions changed expected clock-call counts. Inspection remains effect-free; start/end stamps are explicitly counted. Sleep full-int64-range assertions remain valid.
* New fixture errors involved native helper signatures, reopened-store ownership, implicit root conversation pagination and missing Faux/Radius metadata. Assertions were corrected to the real contract, without skipping behaviour.
* A `make` benchmark argument containing an unescaped `$` produced no matching tests. The wrapper rejected it; direct quoted wrapper invocation completed the equivalent comparison.
* Hydration used an incorrect tarball extraction path once. The missing manifest failed closed; the correct pinned `package/dist/models.generated.js` input passed all twelve faults.
* The initial vulnerability gate rejected GO-2026-6629; the dependency upgrade fixes it. Policy exceptions remain empty.
* The final scan audit corrected conversation-history default direction to ascending, while Storage keeps descending. Dedicated page/continuation assertions pass.
* Independent read-only review delegates timed out and supply no review acceptance. Local inspection and executable tests provide the recorded evidence.

## Verification limits

Live provider billing/quality, live OAuth, Windows/installed-PowerShell, Cloudflare deployments, a fresh clean-clone consumer and hosted CI have not been run for this candidate. Npm SHA512 integrity and decoded SLSA subjects match; DSSE signatures/certificates were not cryptographically verified. Every official suite has a disposition; this is not certification of every inner TypeScript subcase. Native Codex device-flow default is retained for AI compatibility, while browser login adds the 1.1.0 originator option. Compiled native providers do not have a JavaScript lazy-module loader. Published 1.0.4 remains immutable.
