# Hydration and mounted-field operation oracles

Pinned `harness-view.test.ts:113–126,160–176` scenarios now run through real native Harness submission/configuration paths in `durable/view_touch_oracle_main_test.go`. The exact field-operation assertion exposed a production mismatch and is fixed.

## Scenarios and adaptations

`completed-hydration` submits hi and receives hello through the registered fake provider and the real generation/settlement path. A fresh watch checks the root conversation record, complete ordered entry records against independently read committed entries, absent before, exactly the five mounted built-ins, empty live state and an empty inbox items array. Native Submit's default follow-up represents source type:input. The provider emits the native terminal event rather than the source faux text stream; model/system/default document shapes and native entry envelopes are setup adaptations.

`unmounted-silence` creates another ownerless conversation, starts a root watch, appends note to the other conversation, configures thinkingLevel:high, then appends note to the root. It retains the source's natural two-frame count and literal first tuple `["s",["docs","pi.agent","thinkingLevel"],"high"]`, with no delivery barrier between those commits. Source configure is partial, so native ConfigurePatch is the corresponding API; native Configure is full replacement. The three source unchanged-reference assertions remain unported: detached Go values do not establish JS structural sharing.

The initial probe mistakenly supplied native Input.Type=input and used full-replacement Configure. After correcting those API adaptations, hydration passed unchanged production and the field-tuple assertion still failed on memory, journal and SQLite: ConfigurePatch emitted a whole pi.agent set.

## Patch publication fix

Pinned `packages/durable/src/harness/agent.ts:90,120–165` loads AgentDoc and applies changes by top-level field assignment/deletion. Native ConfigurePatch already validates/canonicalises the complete next agent value before writing. It now compares that validated value with the existing document and applies sorted top-level set/delete operations. Object-valued fields retain whole-field set boundaries. Validation, creation when absent, ownership, stored final values, full-replacement Configure and DTO budgets remain unchanged. Unchanged fields emit no operation.

`TestMainAgentPatchFieldOperationBoundaries` is supplementary source-derived coverage, not an upstream test body. It checks model-object replacement, cwd deletion, replay and empty/repeated-value patch silence using a subsequent note as a delivery witness. Existing AgentPatch regression coverage retains partial/clear/conflict validation, detached selections, hook ordering, historical forks and reopen.

## Development checks

Go 1.27.3, GOMAXPROCS=4, project environment sourced:

- Final new oracles/boundary controls plus existing AgentPatch tests, ten repetitions/shuffle 42: CGO 1 passes in 3.148s, CGO 0 in 2.093s.
- Same tests, race three repetitions/shuffle 42: passes in 5.449s.
- Earlier touch/history/mount/atomic-publication/AgentPatch race three repetitions/shuffle 7: passes in 19.017s before adding the final supplementary boundary test; production fix unchanged.
- Durable vet and staticcheck v0.8.2 pass both CGO modes; module verification and diff checks pass.
- Restoring pre-fix agent_patch.go via a disposable logical/physical-path overlay rejects the exact field-tuple oracle on all three backends. Overlay, old source copy and output removed after analysis.

The unrelated continuation prototype remains untracked/excluded and is restored with a content comparison after static checks. Temporary upstream extracts and scratch are disposed after use. No full-suite/full-race, performance or acceptance profiling was performed. Source line128's real-generation touching-commit counter/deferred-attempt assertion is still unported. Shared references, broader microtask equivalence, terminal tags/diagnostics and remaining assertion-index rows stay open. No dependency, format, accepted asset or release-ref change.
