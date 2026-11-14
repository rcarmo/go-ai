# Upstream v1.5.0 alignment

The sole alignment target is official upstream `v1.5.0` at `f9a26365568127461c47a07a37dcb42c140a3ac5`. GitHub published the stable release at `2026-11-02T11:05:31Z`. Earlier release pins and receipts are historical comparison inputs; they establish no v1.5.0 acceptance. Retained tests, runtime identity and generated-schema/catalogue tooling must follow this release, including required breaking semantics.

## Reproduced inference assessment fixes

[Assessment fixes and verification](assessment-fixes.md) records the 166-fixture validation regression table, short OAuth interval correction, deterministic retry timer checks, catalogue verification and seven inherited durable full-suite failures. The 24 reproduced common-domain validation mismatches are fixed; broader schema/compiler and durable/codemode alignment remain open.

## Optional assistant error text

The [current development continuation](../../notes/canonical-compaction-kind.md#optional-provider-error-text) preserves optional empty assistant errors across root/durable JSON and actual summary failure handling. Other message variants/fields and full API/schema/release acceptance remain open.

## Current durable deviations

The [durable deviation register](durable-deviations.md) tracks current evidence for29 groups and165 pinned durable test/support/fixture obligations, including101 test files. The [biggest deviations and path to zero](alignment-roadmap.md) separates confirmed API/record/ordering failures from unverified coverage. The builtin publication gate now passes in the ordinary suite; raw builtin admission/reopen and the primary canonical task read API also pass. Three original task-storage cases and exposed transaction read/write fences are ported. Canonical builtin execution, complete Storage semantics, nested tools, migrations and edge states still need acceptance. State counts are group labels, not a parity percentage.

[Primary conversation/entry reads](../../notes/primary-record-reads.md) add the original global/visible lookup overloads, separate original commit sequences, complete named transaction-fence assertions and literal deep-fork/conversation pagination. Reference coverage is152 unchanged source cases +39 probes. The queued tool-progress/close error-order caveat remains open.

[Submission record reads](../../notes/submission-record-reads.md) replace native lookup/query readers and preserve the original lifecycle and request-index cases. Reference coverage is155 unchanged source cases +43 probes; creation/settlement and other API/runtime gaps remain open.

[Submission transactions](../../notes/submission-transactions.md) now follow the original creation-map/deferred-change flow and return complete records. Reference coverage is166 unchanged source cases +46 probes; full parity and release acceptance remain open.

[Atomic submission admission](../../notes/submission-admission.md) removes the native split lookup/write jobs and preserves Tx wrapper authority. Reference coverage is166 unchanged source cases +48 probes; full parity and release acceptance remain open.

## Historical parity backlog and contract decisions

The semantic-equivalence request supersedes the earlier direction-before-runtime pauses below. Some earlier diagnoses were fixture-translation errors; the current deviation register and semantic-port receipts reconcile them.

[Parity work packets](parity-work-packets.md) define bounded assignments, source/native entry points, reproduction setup, acceptance tests, dependencies and stop conditions. [Contract decisions](parity-contract-decisions.md) records the afterRun, close/requeue, wait-cycle and refusal/failure conflicts; afterRun and close runtime changes need Rui's explicit direction. [Wait-cycle admission](wait-cycle-admission.md) records the P03 graph analysis and its separate runtime-change pause.

## Terminal-only deferred polling

The pinned poll API returns one message; it exposes no harness streaming progress. [Poll-boundary checks](poll-boundary.md) cover source671's real abort-handler cutoff plus two source-derived terminal/crash/cancellation controls on SQLite, without runtime changes. Three functions pass nine repeated checks; four injected faults reject five SQLite leaves. Whole-file dispositions and full v1.5.0 acceptance do not change. Next: after-run source785 missing-definition/Stop-orphaning.

## SQLite-only storage

Rui's clarification supersedes the earlier multi-engine matrix: **SQLite through pure-Go modernc is the sole storage implementation**. Runtime `f284e255e0711ff2cf2950240fdf83b9c5652f5c` removes Memory/Journal implementations and selection paths; [scope and checks](sqlite-only.md) record29selected durable functions/87repeated passes, tools/codemode consumers and static checks. Existing data and published assets are preserved. Earlier memory/journal results below retain their historical scope. The [cutoff-order/salvage checkpoint](cutoff-order-salvage.md) adds source621/648 and derived registry/deferred controls; its four functions also pass on SQLite. No whole-file disposition or full v1.5.0 acceptance changes.

## Session observer checkpoint

[Session observers](session-observers.md) records installed public canonical stored and readonly document observers, exact identity controls and remaining dispatch/binding/lifetime contracts. The row stays `reviewed-open`.

## Conversation-view identity checkpoint

[Conversation-view document identity](view-shared-identity.md) records source330 and later shared readonly identity controls. The native public readonly API is installed; remaining lifecycle/wrapper assertions are still open and historical row314 stays `partial`.

## Release inputs

[Package provenance](packages.json) records npm 1.5.0 archives for `@earendil-works/pi-ai`, `pi-durable`, `pi-codemode` and transitive `pi-schema`: registry/tarball URLs, sizes, computed SHA-256, verified registry SHA-512 integrity, and packed manifests. All four archived names/versions match. Registry metadata supplies no `gitHead`. [Release metadata checks](release-metadata.md) now compare all1712 public catalogue records against that archive and all25 exported JSON schemas against the pinned source and published JavaScript. Complete package-build/source correspondence and wider SDK alignment remain unverified.

The official release is [v1.5.0](https://github.com/earendil-works/pi/releases/tag/v1.5.0). Exact tag refs, GitHub stable/non-draft status and published npm versions were verified. No hosted workflow, release ref or asset was changed.

## Changed paths and test corpus

The [inventory](tip-f9a26365.json) records **428 target-package paths** relative to the historical comparison commit `4a080252f06caf63a525d7873c5d94d3c23887e0`. Since the prior c9 checkpoint there are 42 changed repository paths: seven target-package paths and 35 dependency/support paths. Target changes are release/changelog/manifest metadata and the AI version string. Main/schema test corpora have 362/210 paths; none are added, removed or changed since c9.

The [main crosswalk](upstream-tests-f9a26365.tsv), [schema queue](dependency-tests-f9a26365.tsv) and [scoped reviews](upstream-review-f9a26365.json) preserve native receipt pins/limitations. Reused reviewed paths were checked against the exact release objects. Dispositions are inventories of work; overall release alignment is unverified.

The earlier main tip `3e0eec29` includes **post-release** OpenCode Claude mid-conversation effort/`drop_block` and Anthropic non-strict `$defs`/`definitions` preservation. Those production changes and the added definitions-key regressions are excluded from this release target. The original release's Anthropic schema test still needs its existing native coverage reconciled; the newer assertions receive no 1.5.0 credit.

## Release metadata correction

Active identity, schema and catalogue tooling now target1.5.0. The source generator uses pi-schema1.5.0 rather than the obsolete TypeBox input contract. Independent full-record checks found and fixed three metadata omissions;1712 records now match the official archive. Focused tests, negative controls, vet and Staticcheck pass; [inputs, representations and limitations](release-metadata.md) remain explicit. Older release receipts confer no current acceptance.

## Explicit continue checkpoint

Runtime `b66619a5169405a993ea9187d44185c171385d05` integrates the preserved continuation work: idle entryless admission, queued-input ordering, inherited boundaries, handoff, abort/ownership, nothing-to-answer and no-model settlements. Output-limit tool calls stay answered without execution. The [source crosswalk and checks](explicit-continue-baseline.md) record 29 focused functions, count=3/shuffle=42: 87 passes, no failures/skips, 18.493s under Go 1.27.3/CGO=0. A real SQLite stored-partial/crash/reopen case, seven active faults, vet and Staticcheck v0.8.2 pass. At that checkpoint, `status.trace` had no native serializable field. [Submission trace carriers](submission-traces.md) persist backend-owned strings through status, copies and pure-Go SQLite reopen. [Task traces](task-traces.md) adds task parent/link selection, explicit roots, per-phase extraction, host-root fallback and first-invocation links, replacing the earlier queued-link collector; 26 focused affected functions pass. [Builtin task traces](builtin-task-traces.md) now persist parallel/sequential tool carriers and report aborted/OK for closed active phases; background-compaction metadata, nested carriers and active reopen parenting have source-derived controls. Four new and ten affected functions pass 42 repeated focused checks. Broader builtin labels/phases, full-body/callback-clock checks, mixed boundaries, empty-error presence and restart-limit/report/settings need source oracles. The [after-run runtime](after-run.md) now supplies the typed hook and selected answer/finish/drain, terminate, Stop and orderly-close paths. Original answer/child-hold and empty-hook assertions are ordinary tests; the declaration-only adapter was removed. Validation/installed-definition controls, queued boundaries and builtin-name guards pass. The [recovery baseline](after-run-recovery-baseline.md) preserves its original answered-finish and child-cutoff failures. The [cutoff checkpoint](restart-cutoffs.md) now passes those cases, crash/no-reask, both lowered-limit controls and three coupled builtin controls across all three backends. The [result-hook checkpoint](cutoff-result-hooks.md) adds source584 at/past-limit hook execution and selected replacement/error/usage/current-limit controls. Other result-hook/phase contracts, history and remaining29-file assertions are still open; the [historical baseline](after-run-baseline.md) preserves its original failures and source index. Whole-file telemetry and continue reviews stay `pending-review`; release alignment is unverified.

## Pure-Go SQLite checkpoint

Native runtime `526b719b13c87c92cf7d36a1eb4ea2f802ea37d6` was committed locally at the SQLite checkpoint. The explicit-continue checkpoint above integrates the WIP that prevented its clean-main push. [SQLite migration checks](pure-go-storage.md) pass with Go 1.27.3 and CGO=0: 42 selected SQLite/group functions repeated three times, 126 top-level passes, no failures/skips. The final candidate excludes continuation WIP only in an isolated validation copy. The modernc v1.61.1 adapter preserves native WAL/FULL, schema/replay, rollback/poisoning, ownership and admitted-cancellation settlement. A private connector boundary restores Linux/mips64le package/test compilation; SQLite remains unavailable where its engine or secure locking is unsupported. Upstream storage facade/schema/migration and wider-platform acceptance remain open. Code mode retains QuickJS in wazero.

## Reused local-listener result

Native runtime `148c77b08c1d84a9cfca278380b3b872544a7ffe` was verified against 3e durable inputs. Exact comparison confirms the release's durable/schema runtime and test trees are identical. The [Source564 port and private local-listener controls](../upstream-main/session-local-observers.md) therefore retain their original focused normal/race/static/mutation results; no test was repeated merely to retarget. Earlier stored-observer/retention/lifecycle/deadline and generation receipts also keep their original scope.

The lane is private and standalone. [Conversation-view memory controls](conversation-memory.md) now pass unchanged runtime on all three backends, including acquisition during a hold, own-only dependent flushes and timer/backlog replay. [Numeric deadline controls](numeric-deadlines.md) now cover fractional/extreme windows, duration saturation and full-width native clocks while preserving source Number arithmetic in its safe-integer domain. [Live host persistence settings](harness-persistence-settings.md) now read current host policy, preserve native nonfinite settings across ownership copies and keep request overrides separate. The [private runtime-owned progress lane](runtime-progress-lane.md) now drives [production generation presentation and stored-partial recovery](generation-deferral.md). The unchanged batching oracle now delivers20 memory partials/8 stored batches, and the older stored-touch oracle passes; both were promoted into default discovery. Crash/reopen, atomic conversion, memory agent events and receipt-adopted close requeue have focused controls. [Held-generation clean close and deferred-poll crash recovery](close-poll-recovery.md) pass unchanged runtime. [Tool crash recovery](tool-recovery.md) now preserves restart counts across presentation writes, unsafe stored output and safe replay through a second crash. [Tool presentation](tool-deferral.md) now uses runtime-owned document progress, with memory-before-storage, timer/dependent/Close persistence, first-store crash, sibling preservation and memory-event controls. [Summarisation crash/restart and actual third-party dependent reads](summary-dependent-recovery.md) now pass unchanged production, including same-task recovery and actual atomic storage-batch checks. Public immutable/shared values, tracker/WeakRef contracts and broader Harness waiter/abort/report ordering still need controls; the [eleven-declaration assertion map](harness-deferred-assertions.md) records preserved outcome assertions and matching source retry overrides for its nine slow fixtures. [Crash-interrupted generation retries](interrupted-retry.md) now charge current policy atomically with partial conversion, back off or fail interrupted, retain deferred jobs through poll retries and cancel on exhaustion. Focused request/poll, backwards-clock, budget, sleeping, clean-close and ownership controls pass; the [pre-fix failures](interrupted-retry-baseline.md) remain historical evidence. [Run-failure projection](run-failure.md) and the joined source275 retry/tool-wait oracle now have a validated focused checkpoint at native `6fb7c75`; the [18-declaration map](restart-failure-assertions.md) records its scope. No-model failure now ends its existing run before selecting queued successor inputs. [Boundary-first](boundary-first.md) closes the source-derived passive-write-first boundary while preserving `cp.InputEntry` as the input's own entry, and selected answer/model-error/no-model atomic end/start cases. Full explicit-continue fidelity (including task trace restoration and queued links), upstream `afterRun`, full mixed-mode/reset coverage, full source test bodies and the remaining restart-limit/report/settings declarations remain open. Whole-file restart-failure review remains `pending-review`. Logical-attempt, callback/clock, representation and empty-error field-presence equivalence is unverified. The [superseding validation note](run-failure.md#superseding-boundary-checkpoint) records the current pure-Go gates; earlier 44-function/race results predate the latest fail-fast overlay. [Earlier validation reconciliation](../upstream-main/validation-baseline.md) and [retained technical reviews](../upstream-main/README.md) describe the remaining AI/durable/codemode interactions. Version/user-agent/catalogue metadata, full-record catalogue comparison, dependencies/support applicability and broad contract completion are unverified.

## Regeneration

With exact release/baseline refs and c9 ancestry available in a project-owned mirror:

```sh
. scripts/project-env.sh
bun scripts/inventory-upstream-main.ts <mirror> \
  docs/upstream-main/tip-c9f4c7b9.json \
  f9a26365568127461c47a07a37dcb42c140a3ac5 docs/upstream-v1.5.0 v1.5.0
```

The optional release argument must resolve to the exact target before the generator writes anything. A mismatched v1.4.0 tag is rejected. The existing four-argument main mode keeps its original output contract. Raw fetched objects, npm archives, package copies, logs and completed build cache are disposed after analysis; only concise provenance/inventories/results are retained. No full-suite, profiling, native 1.5.0 release or complete alignment result.

Caller-owned [submission admission](../../notes/submission-admission.md#caller-owned-transaction-admission) now has the `SubmitIn(*Tx,Input)` spelling with ownership/fence/rollback controls. Reference coverage is189 unchanged source cases +48 probes; the complete native submit-in-commit assertion set remains open.

The [current completion report](../../notes/upstream-conformance-report.md) records canonical queued Inbox drafts, the full atomic busy-steer assertion packet and remaining blockers. Latest reference coverage:189 unchanged source cases +49 probes. The full migration is not complete.

Canonical queued write consumption now follows Inbox drafts and defers target transitions to assembly; see the [updated report](../../notes/upstream-conformance-report.md). The [expanded original oracle](../../notes/upstream-oracle.md) reaches all99 Node test files but retains four original fixture/resource failures; it is not a full-oracle pass or native parity claim.
