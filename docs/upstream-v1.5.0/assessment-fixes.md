# Reproduced pi-ai assessment fixes

Base: `d272616c6d09f7ce903a3e704f3bb07ea729a125`. Target: official v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5`. This is a bounded inference/validation correction, not full pi-ai/durable/codemode acceptance.

## Test-first evidence

The unchanged pinned upstream suites pass under Node 26.13.0, Vitest 4.1.12 and TypeBox 1.3.27, resolving pi-schema to the pinned source: validation probes 166 tests, provider retry 6, OAuth device code 5; **177 passes**. A disposable validation wrapper recorded real schemas/inputs/outcomes without changing those assertions; all 177 still passed.

`tests/testdata/validation_probes_v150.json` retains 166 common-domain calls. `TestV150ValidationProbeFixtures` failed **24 leaves** against unchanged Go: 14 acceptance/coercion differences and 10 diagnostic/error-shape differences. All 166 now pass. Successful JSON compares structurally, preserving field presence and types but ignoring key order. Non-JSON JS calls, one unpaired-surrogate call and the repeated-schema mutation/cache assertion are explicitly excluded; serialized Go schemas cannot establish JS object-identity/cache semantics. The fixture table is not the whole upstream test corpus.

Source-derived controls supplement the pinned assertions: 19 public constraint cases and native exact-number, ownership, cycle/depth and 10,000-member uniqueness controls. A baseline compiler overlay confirms the negative multipleOf, minItems, maxProperties, contains, conditional/dependency, tuple-rest and false-item controls fail before the changes.

## Runtime corrections

- Membership and diagnostics now enforce Unicode code-point lengths, exact decimal `multipleOf`, array/object bounds, structural `uniqueItems`, tuple rest and boolean item schemas, `contains` bounds, conditional/dependency rules, property names and unevaluated-property checks over successful applicators. These participate in union selection, fixing the false oneOf overlap.
- Error selection preserves required-field paths, unresolved-reference reasons, false-schema/additional-field diagnostics and the source union preference for the branch declaring most present fields, then the deepest first error. Invalid reached regex syntax is distinct from matcher infrastructure failure; the captured invalid character-class outcome matches upstream. This does not close the separate complete compiler-reachability, dynamic-scope or host-engine parity backlog.
- Uniqueness canonicalization owns its containers, keeps strings distinct from exact numeric values, rejects native cycles/depth beyond 256 and caps aggregate canonicalization work at 1,048,576 values. It uses hashed canonical keys rather than pairwise comparisons. Native exact numeric extensions are not rounded through float64 to establish divisibility.
- Explicit positive OAuth intervals now have the pinned one-second minimum; provider DTO omission remains the five-second default. Codex distinguishes its explicit zero interval. The real xAI provider test covers initial two seconds and a server-directed one-second slow_down. The prior five-second helper/Radius expectations were reconciled rather than retained as compatibility requirements. This does not certify every OAuth flow or fractional-interval representation.
- Public provider retry tests now check the exact 999ms/1ms and 1999ms/1ms boundaries with `testing/synctest`, cancellation with no later retry, and joined workers. A private waiter used by production makes timer ownership directly testable: after cancellation its unexpired timer must already be stopped.
- Existing identity/Codex user-agent and catalogue tests had stale expectations. Official npm 1.5.0 confirms the revised Kimi/GLM prices and that only the paid Vercel Laguna entry remains. No generated catalogue was hand-edited. Staticcheck's private error-name warning was fixed; the exact upstream capitalized classifier timeout message has a narrow lint explanation.

## Verification

Go 1.27.3, Linux ARM64, CGO disabled, project-owned scratch:

- Root/public/OAuth/QuickJS package regressions pass. The final run of every package except the independently failing `durable` package passes **1,398 top-level functions**, no failures/skips. It includes all provider packages and durable/tools; the exclusion is explicit, not a full-suite pass.
- Focused validation and OAuth/retry controls pass count=3/shuffle=42.
- Removing constraint membership, timer retirement or the short-interval policy independently makes the corresponding new tests fail. These are effective assertion failures, not build/panic failures.
- `make vet`, Staticcheck v0.8.2, logging gate and diff checks pass.
- Official npm archive SHA-256 `e320fe7bd2b927782ba9a37d356fe5837f1aa96a46432610e1b3b9ccb748ea5c` verified. Full public catalogue comparison still matches 1,592 chat, 64 image and 56 classifier records.

### Profiling and tuning

Focused root/public/OAuth CPU, alloc_space and alloc_objects captures were inspected and disposed by the profiling wrapper. Public-test cold-start CPU was dominated by QuickJS initialization (370ms cumulative of 560ms sampled); cumulative allocation included about 37.6MB for engine initialization and isolated matcher guests. Reusing mutable guest state would weaken isolation, so no pooling change was made. These are cold-start test totals, not per-request performance claims. OAuth CPU samples were insufficient for a meaningful CPU ranking; its sampled allocations were dominated by package initialization and test/runtime scaffolding.

The 10,000-integer uniqueness control exposed avoidable scalar JSON encoding and decimal-normalization allocation. A scalar-key fast path removed that work. Three 200ms benchmark samples with identical toolchain/workload and an overlay restoring the initial canonicalization give:

| Implementation | Time/op | Bytes/op | Allocations/op |
| --- | --- | --- | --- |
| Initial canonicalization | 18.25–18.51ms | about 1,325,300 | 80,924–80,925 |
| Scalar fast path | 1.77–2.62ms | about 588,500 | 19,933 |

Exact numeric/string-domain, duplicate-object, ownership and bounded-input controls still pass. The reprofiled root allocation outlier no longer contains scalar decimal normalization; no general cross-package speedup is inferred. Raw captures, benchmark overlays and disposable source/npm/test trees were removed after analysis.

## Full-suite blocker: inherited durable failures

The unrestricted `make test` was run and **failed**. The seven durable functions below also fail with the original validation implementation restored through an overlay, separating them from this patch's regressions:

1. `TestMainSessionCase256ReadSurfacesWaitForPendingCreation`: head-marker read returns unknown-conversation inside the pending window.
2. `TestInternalGroupSequentialBackendError`: appendFrame fault does not exercise SQLite's atomic appendFrames group path.
3. `TestInternalGroupAdmissionCancelAndClose`: its appendFrame hold never reaches grouped settlement.
4. `TestTaskRecoveryToolDecidingHoldRejectedUncertainAndReopen`: persisted progress/receipt expectation fails.
5. `TestTaskRecoveryQueuedWithdrawalOwnedChildDefersSettlement`: queue-preservation assertion fails.
6. `TestTaskRecoveryHostPanicDecidingStoragePrefixAndReopen`: resolver-call expectation fails after recovery.
7. `TestTaskRecoverySuccessfulValidatorIntentStoragePrefixAndReopen`: executing intent/prefix/memo assertion fails.

No assertions were removed, skipped or relaxed to conceal these failures; no durable production code changed. The full run also exposed a stale Codex user-agent assertion, which was corrected and its package passed in the final non-durable run.

Broader existing design gaps remain: afterRun/task history and close ordering, wait-cycle admission, concurrent grouped publication, immutable shared view identity and watch bounds. Shared identity cannot safely be obtained by returning writable internal Go maps; the repository explicitly requires a representation/API migration. Grouped publication requires prepared-part admission and ordered prefix adoption, not a timer or storage-only approximation. Those contracts and the seven failures remain follow-up blockers. Whole-file crosswalk dispositions and release acceptance remain open. No hosted CI, release/tag/asset mutation, dependency change or paid-provider calls occurred.
