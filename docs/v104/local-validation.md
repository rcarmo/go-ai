# Local 1.0.4 validation

The native release candidate aligns the audited AI 1.0.1 -> 1.0.4 and durable 1.0.0 -> 1.0.4 changes. Rui authorised committing, tagging and publishing v1.0.4 on 6 October 2026. It preserves the accepted local 1.0.0 commit `3d9c6ab0e1c24779691724222e5da5be1d0a557c`, pre-release origin/main `52d01064100411e62f8528cfa6de36f210f81aab`, stash `9be4ebbd0c1de275d69bd95a4f1fc3db78f00e74` and all historical tags/assets. [Native v1.0.4 is published](https://github.com/rcarmo/go-ai/releases/tag/v1.0.4); exact runtime, SBOM and hosted results are recorded below.

## Published native v1.0.4

* Release: [go-ai v1.0.4](https://github.com/rcarmo/go-ai/releases/tag/v1.0.4), release ID `405015775`, public/non-prerelease. Runtime: `65e25029c969d154560d0c26eec032d2605d80da`.
* Annotated tag object: `87563d040aa3e43fb3ff33451738bbee6821f7fa`, Rui Carmo <rui.carmo@gmail.com>, peeled to that runtime. Verification tooling head: `12c33e6947c1af6f6983193ac36dee250144c2c0`; only workflows/audit differ from runtime. Tag never moved. Rollback: pre-release origin/main `52d01064100411e62f8528cfa6de36f210f81aab`.
* Final [hosted CI 37507327576](https://github.com/rcarmo/go-ai/actions/runs/37507327576): SUCCESS on Go 1.27.1. Deterministic count3, race, coverage, five 30s fuzz targets, staticcheck v0.8.1, security/licence/catalog/SBOM checks and uploads pass. Local isolated deterministic `run-20261006T174909Z-QJPRtj`, race `run-20261006T173759Z-XcByeQ` and coverage `run-20261006T173812Z-YuZ5DZ` pass. Coverage instruments production packages with `-coverpkg=./...` (79.0% local statements); assertions/deadlines are unchanged.
* [Publisher 37509516521](https://github.com/rcarmo/go-ai/actions/runs/37509516521) passed exact-runtime SBOM/schema, no reachable vulnerabilities on Go1.27.1 and allowed licences, but GitHub denied release creation to its integration token (403). The authorised release credential created the release with local exact-tag assets after those gates passed.
* Public assets: `sbom.cdx.json` (ID `616288030`, 18572 bytes), `sbom.cdx.json.sha256` (ID `616288029`, 80 bytes). CycloneDX generator v1.12.0, 19 components, root version/purl/bom-ref 1.0.4, exact `vcs.revision`, one root dependency edge. SHA256 `844a5f1d61a17a0fb3e143441e705695bceb4ffb6ae494d9d177d14f09b4f856`. Both files downloaded from the public unauthenticated release URLs, checksum/schema/provenance validated and matched the accepted local exact-runtime assets. The independently normalised hosted SBOM digest was `ec047099ea2d30f6455812f462884d208a16e3ea039744ea6e86280e48681c59`; publication uses the separately generated/validated local asset, not an asserted byte-identical hosted artifact.

## Corrected release-tooling failures

Initial hosted CI `37499029156` rejected workflow-level `runner.temp` before jobs. Authorised tooling-only retries exposed inherited command-line root propagation through `MAKEFLAGS`, then coverage warnings from an external-only test executable without production instrumentation. Current routing uses shell `$RUNNER_TEMP`/`$GITHUB_ENV`; Make uses environment routing, and coverage uses `-coverpkg=./...`. Pinned actionlint v1.7.7 validates both workflows. Publisher validator runs from its tooling checkout, scratch stays in the project build root and existing native releases/assets fail closed.

Rui selected Go1.27.1 for final verification. An initial local three-heavy-job run hit a three-second journal-test deadline; the same deterministic count3 workload passed in isolation without changing any assertion/deadline. Staticcheck v0.7.0 could not read Go1.27 export data; final workflow pins v0.8.1. Final CI is green; failed runs remain failed, not counted as passes. No runtime changes were made after tagging.

Profiles covered CPU, alloc_space and alloc_objects. Isolated durable session commit accounted for 67.70% cumulative CPU, 78.27% allocation space and 80.79% allocation objects; bounded encoding/strict detached reads remain required. No comparable-workload speedup is attributed to changing Go/tooling or suite load. Raw profiles, failed/probe logs, scratch clones and build caches were disposed after use. Original stash and older published releases/assets were not modified. A read-only historical check found local v1.0.1 tag object `aa21fb8` differs from remote `913bdb5`; both were left untouched. This does not affect the independently verified native v1.0.4 tag.

## Authorised frozen release candidate

Index tree `52c0d205905e6e158000c8df1f328c028c59a8c6` passed Go 1.26.6 `make check` on 6 October 2026: all-package deterministic count3 tests (`run-20261006T164334Z-KtQhks`), vet/staticcheck/logging, portable temp tests, historical inventory/fault gates, clean catalogs, SBOM/security/licence checks. The full-repository shuffled race run passes `run-20261006T164333Z-xgjp7b` (durable 286.295s, tools 22.847s). Both CGO and non-CGO builds pass. Five 5s fuzz smoke targets pass with coordinator/worker CPU/heap analysis; hosted CI uses the full 30s fuzz workload. Workflow check/fuzz/publisher Go pins are all 1.26.6. Only this release-record paragraph was added after candidate verification; no runtime/test/dependency changed.

Release profiling again identifies session commit/bounded encode/strict decode/reference preparation as dominant durable chains; ownership and validation contracts are preserved. No equivalent-workload runtime speedup is attributed to the full release run. Raw captures and disposable logs/scratch are disposed after analysis.

## Resumed gap review -- modern Go only

Rui's 6 October instruction selects Go 1.26.6 only for continued validation. Older minimum-version receipts below are historical and were not repeated; the module's declared compatibility minimum is unchanged.

The resumed official-diff review found three gaps that the earlier suite missed:

* Positional MIME detection inspected only 512 bytes. A PNG with `acTL` beyond that probe was misclassified. `detectImageMimeOf` now skips PNG payloads and reads chunk headers in blocks <=64KiB until `acTL`, `IDAT` or an invalid boundary. Tests cover late/chunk-block-edge animation, IDAT ordering, malformed lengths, bounded reads and read-error propagation.
* Overlapping watcher targets could replace an explicit symlink target's followed identity with its parent directory listing. An explicit target now wins independent of target order; ancestor snapshots preserve existing entries. Regression tests assert both target orders.
* A positional decoded prefix ending at exactly 2000 newlines retained an extra continuation newline. Exported `TruncateHeadOf` now accepts exact decoded whole-selection totals and is used by the production reader. Eighty independent pinned upstream fixtures verify prefix/totals truncation, including zero limits and UTF-8. The exact-4KiB/2000-newline production test agrees with the native whole-file path.

Both probes failed before the fixes; the prefix-boundary probe also failed independently. Current complete tools package checks pass on Go 1.26.6: normal shuffle `run-20261006T150015Z-00Ixua` (4.315s), race shuffle `run-20261006T145852Z-kgEQmy` (25.729s); focused reader/watch/scan/truncation race count3 passes `run-20261006T145641Z-osObNC`. Repository vet, staticcheck v0.7.0, logging and non-CGO build pass after these edits. No other package runtime or dependency changed, so completed unchanged package suites/catalog/security checks were not repeated.

Latest tools profiles include CPU, `alloc_space` and `alloc_objects`. Session commit accounts for 35.48% cumulative CPU, 68.58% cumulative allocation space and 77.37% cumulative allocation objects under the race suite. Strict decode/reference ownership still dominates allocations. The new detector retains one bounded chunk block and skips payloads; no speculative ownership weakening or measured whole-suite speedup is attributed to these correctness fixes. Raw profiles, matching binaries, failed/probe data, run logs and rebuildable caches are disposed after analysis.

## Earlier gates on 6 October 2026

| Gate | Result |
| --- | --- |
| Go 1.25.0 `make test TEST_FLAGS='-shuffle=on'`, all packages | PASS `run-20261006T143139Z-tXWd67`; durable 55.201s, tools 4.529s |
| Go 1.26.6 `make test-race TEST_FLAGS='-shuffle=on'`, all packages | PASS `run-20261006T143139Z-qUoDtT`; durable 283.926s, tools 24.108s |
| Final additive PowerShell/shared shell runner, Go 1.25.0 tools shuffle | PASS `run-20261006T144331Z-vCclGQ`, 4.814s |
| Final additive PowerShell/shared shell runner, Go 1.26.6 tools race shuffle | PASS `run-20261006T144331Z-PVHkG8`, 21.155s |
| Vet, staticcheck v0.7.0, logging checks, CGO-disabled build | PASS after final runtime edits |
| Project-temp portable CI/local routing and unsafe-root tests | PASS, including generic fallback; no host-only CI helper |
| Chat/image/classifier regeneration | PASS from official npm 1.0.4, 1537/60/23 |
| Offline schema-v6 hydration | PASS: reproducible catalogs and twelve fail-closed, atomic-output fault cases |
| Independent regeneration corruption tests | PASS: comparators reject deliberately changed metadata |
| SBOM normaliser/validator, native tag verifier, vulnerability-policy negative tests | PASS |
| External module importing public AI/durable/tools/OAuth APIs | PASS race shuffle after final edits `run-20261006T144817Z-YwNbas`; local module replace, not clean-clone provenance |
| CycloneDX v1.12.0 SBOM generation/validation | PASS, 19 components, digest `4f24393e1634461f0dffafd7a3948bcaa8aead8446821c3312f94fdff313ef72` |
| govulncheck v1.7.0, Go 1.26.6 | PASS policy gate, no reachable finding |
| go-licenses v1.6.0 | PASS allowed-licence gate; assembly dependency inspection warnings for websocket/compress |
| `git diff --check` | PASS |

The full-repository runs below precede the additive PowerShell/shared shell runner and the resumed reader/watch/truncation fixes. Complete affected-package verification and repository static/build checks cover the latest tools changes as recorded above; older dual-toolchain runs retain their historical scope. The older failed image-width assertion expected 2300; pinned 1.0.4 specifies 2000. The older DeepSeek wire test exposed dropped reasoning replay; current production SSE capture preserves the reasoning field signature and replay emits it. Failed/probe runs are superseded, not counted as passes.

The earlier candidate SBOM identified `3d9c6ab0e1c2` and validated dependencies/schema. The authorised release regenerates canonical `artifacts/sbom.cdx.json` and its checksum from the exact accepted commit, with root version/purl/bom-ref 1.0.4 and the full native VCS revision; it never reuses a historical release asset.

## Profiling and tuning

CPU, `alloc_space` and `alloc_objects` were analysed for every pre-release test-bearing package. Durable session commit, bounded encoding, reference preparation and strict decode dominate full-suite CPU/allocations. These operations enforce detached transaction/state ownership; this cycle did not bypass cloning or strict JSON checks for a speculative speedup.

`BenchmarkLineScan104UTF8` scans 655360 bytes of repeated multibyte/malformed UTF-8 and newlines, Go 1.26.6, `-run ^none -benchtime=5x -benchmem`. Equivalent benchmark runs measured:

| Scanner | ns/op | bytes/op | allocs/op |
| --- | ---: | ---: | ---: |
| Slice-backed pending UTF-8 prefix | 8453207 | 2097404 | 262147 |
| Fixed four-byte pending prefix | 5147679 | 211 | 1 |

The fixed prefix removes per-byte pending-prefix allocation. All 240 independent pinned LineScanner fixtures and native whole-file/positional differential tests pass after tuning. Five iterations and short CPU captures limit timing confidence; the allocation change is directly measured. CPU still spends time decoding/counting UTF-8; no full-suite speedup is inferred from unlike workloads. Polling hashes are bounded to recent files <=256KiB, matching the reference strategy, instead of reading every watched large file on every poll.

Raw CPU/heap captures, matching binaries, failed/probe captures, disposable test logs and consumer/catalog-audit scratch are removed after analysis. Concise receipts remain in the selected project root's `evidence/analysis`; official pinned package/source assets, Go modules/toolchains, source fixtures and intentional SBOM assets are preserved.

## Native boundaries

* `ExtendedFileSystem` and `ArgvShell` are additive. Existing injected interfaces still work; legacy whole-file reads cannot promise positional memory bounds. The production positional test proves an 8MiB file uses only a 64KiB decoded head plus a signature probe after the counting scan.
* `LocalEnvWithWatchOptions` explicitly uses polling, with 2s default and configurable interval/directory budget. Native OS event mode is not implemented. Polls may miss changes undone between snapshots. Targets follow their own symlinks; descendant links are not traversed. Unix opened-file identity/no-follow checks are verified; non-Unix atomic no-follow races are not independently accepted.
* PowerShell uses injected argv execution, prefix/preparation, UTF-8 console setup, and `pwsh` -> `powershell` fallback only for spawn failure. Linux injection tests verify arguments and failures; no live PowerShell or Windows-host execution was performed. Output window is an advisory capability: the local shell forwards all decoded output and need not omit it. Injected omission metadata is consumed by `OutputSkipped`.
* OAuth stored refresh is a host-owned atomic `CredentialStore.Modify` boundary. Host stores must implement process-shared locking and persistence; the library cannot make an arbitrary external store atomic. Caller-owned refresh APIs keep their existing behaviour.
* Native read arguments are integral. Fractional/NaN JavaScript slicing behaviour, JavaScript test-runner exports and declaration identity are language adaptations. Live provider credentials and blanket TypeScript inner-subcase equivalence were not tested.
* Independent review of the entire 1.0.4 corpus and genuine external clean-clone acceptance have not been performed. The authorised release checks the frozen candidate and hosted checkout; it retains the stated native scope. Two bounded delegated reader reviews timed out; local review and focused tests supplied the current checks.
