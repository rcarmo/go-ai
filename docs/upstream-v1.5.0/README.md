# Upstream v1.5.0 alignment

The target is the official upstream `v1.5.0` release at `f9a26365568127461c47a07a37dcb42c140a3ac5`, replacing the earlier moving-main goal at Rui's request on 2 November 2026. GitHub published the stable release at `2026-11-02T11:05:31Z`. The accepted upstream baseline stays `v1.4.0` at `4a080252f06caf63a525d7873c5d94d3c23887e0`; native v1.4.0 stays at `ee3076b3659697f1de01b87da964396e069a4389` with its assets protected.

## Release inputs

[Package provenance](packages.json) records npm 1.5.0 archives for `@earendil-works/pi-ai`, `pi-durable`, `pi-codemode` and transitive `pi-schema`: registry/tarball URLs, sizes, computed SHA-256, verified registry SHA-512 integrity, and packed manifests. All four archived names/versions match. Registry metadata supplies no `gitHead`; complete package-build/source correspondence and generated-catalogue alignment have not been established.

The official release is [v1.5.0](https://github.com/earendil-works/pi/releases/tag/v1.5.0). Exact tag refs, GitHub stable/non-draft status and published npm versions were verified. No hosted workflow, release ref or asset was changed.

## Changed paths and test corpus

The [inventory](tip-f9a26365.json) records **428 target-package paths** relative to accepted upstream v1.4.0. Since the prior c9 checkpoint there are 42 changed repository paths: seven target-package paths and 35 dependency/support paths. Target changes are release/changelog/manifest metadata and the AI version string. Main/schema test corpora have 362/210 paths; none are added, removed or changed since c9.

The [main crosswalk](upstream-tests-f9a26365.tsv), [schema queue](dependency-tests-f9a26365.tsv) and [scoped reviews](upstream-review-f9a26365.json) preserve native receipt pins/limitations. Reused reviewed paths were checked against the exact release objects. Dispositions are inventories of work; overall release alignment is unverified.

The earlier main tip `3e0eec29` includes **post-release** OpenCode Claude mid-conversation effort/`drop_block` and Anthropic non-strict `$defs`/`definitions` preservation. Those production changes and the added definitions-key regressions are excluded from this release target. The original release's Anthropic schema test still needs its existing native coverage reconciled; the newer assertions receive no 1.5.0 credit.

## Reused local-listener result

Native runtime `148c77b08c1d84a9cfca278380b3b872544a7ffe` was verified against 3e durable inputs. Exact comparison confirms the release's durable/schema runtime and test trees are identical. The [Source564 port and private local-listener controls](../upstream-main/session-local-observers.md) therefore retain their original focused normal/race/static/mutation results; no test was repeated merely to retarget. Earlier stored-observer/retention/lifecycle/deadline and generation receipts also keep their original scope.

The lane is private and standalone. [Conversation-view memory controls](conversation-memory.md) now pass unchanged runtime on all three backends, including acquisition during a hold, own-only dependent flushes and timer/backlog replay. [Numeric deadline controls](numeric-deadlines.md) now cover fractional/extreme windows, duration saturation and full-width native clocks while preserving source Number arithmetic in its safe-integer domain. [Live host persistence settings](harness-persistence-settings.md) now read current host policy, preserve native nonfinite settings across ownership copies and keep request overrides separate. The [private runtime-owned progress lane](runtime-progress-lane.md) now drives [production generation presentation and stored-partial recovery](generation-deferral.md). The unchanged batching oracle now delivers20 memory partials/8 stored batches, and the older stored-touch oracle passes; both were promoted into default discovery. Crash/reopen, atomic conversion, memory agent events and receipt-adopted close requeue have focused controls. [Held-generation clean close and deferred-poll crash recovery](close-poll-recovery.md) pass unchanged runtime. [Tool crash recovery](tool-recovery.md) now preserves restart counts across presentation writes, unsafe stored output and safe replay through a second crash. [Tool presentation](tool-deferral.md) now uses runtime-owned document progress, with memory-before-storage, timer/dependent/Close persistence, first-store crash, sibling preservation and memory-event controls. [Summarisation crash/restart and actual third-party dependent reads](summary-dependent-recovery.md) now pass unchanged production, including same-task recovery and actual atomic storage-batch checks. Public immutable/shared values, tracker/WeakRef contracts and broader Harness waiter/abort/report ordering still need controls; the [eleven-declaration assertion map](harness-deferred-assertions.md) records preserved outcome assertions and an unresolved literal retry-fixture mismatch, so full-body equivalence is not accepted. [Earlier validation reconciliation](../upstream-main/validation-baseline.md) and [retained technical reviews](../upstream-main/README.md) describe the remaining AI/durable/codemode interactions. Version/user-agent/catalogue metadata, full-record catalogue comparison, dependencies/support applicability and broad contract completion are unverified.

## Regeneration

With exact release/baseline refs and c9 ancestry available in a project-owned mirror:

```sh
. scripts/project-env.sh
bun scripts/inventory-upstream-main.ts <mirror> \
  docs/upstream-main/tip-c9f4c7b9.json \
  f9a26365568127461c47a07a37dcb42c140a3ac5 docs/upstream-v1.5.0 v1.5.0
```

The optional release argument must resolve to the exact target before the generator writes anything. A mismatched v1.4.0 tag is rejected. The existing four-argument main mode keeps its original output contract. Raw fetched objects, npm archives, package copies, logs and completed build cache are disposed after analysis; only concise provenance/inventories/results are retained. No full-suite, profiling, native 1.5.0 release or complete alignment result.
