# Canonical compaction task kind

Compaction now uses `pi.compaction` throughout creation, registry lookup/protection, persisted records, public stored publications, status projection, restart cutoffs and telemetry. The old `task.pi.compaction` alias is removed; native backwards compatibility is outside the authorised port contract.

Target: pi-durable v1.5.0 at `f9a26365568127461c47a07a37dcb42c140a3ac5`. Source `src/harness/compaction.ts:114` defines version1 and initial checkpoint `{phase:"select"}`. `src/tasks.ts` constructs tokens without a blanket `pi.*` namespace ban. Independent probes establish the real Harness manual-compaction publication, a running application `pi.example` task and raw application/compaction storage records.

## Production changes and assertions

- Public Harness compaction publications now carry kind `pi.compaction`, version1, manual input and the initial pending/select state. This uses actual SQLite, model resolution, scheduling, stored observers and task waits; an empty context makes no provider request.
- DefineTask accepts the `pi.*` namespace. The prior constructor test's `pi.generation` case now asserts token acceptance; empty kind is separately rejected. Input ownership, copied phase maps and every other constructor assertion remain intact.
- Core validation and public Storage.Commit no longer refuse every `pi.*` task. Application tasks and generic compaction records pass receipt, exact canonical JSON and SQLite close/reopen checks. The helper predicate now covers only the two private generation/tool checkpoint adapters.
- Registry replacement/removal protections for compaction still pass across reopen. Existing compaction, automatic/background selection, cutoff, afterRun and telemetry fixtures keep their assertions with the source kind literal.

The first native public-observer test failed on `task.pi.compaction`; application task construction failed on the prefix restriction. After the constructor change, a deliberate restored-storage-prefix fault failed with invalid native task execution; restoration was automatic. A separate real public Storage.Commit probe then failed with only the conversation batch stored because its public translator retained another prefix ban. That ban is now narrowed too.

The first source application probe incorrectly expected waitForTask to return only an outcome and used a nonexistent Harness.task method. Those fixture errors were corrected against the real returned TaskRecord before production changes; they receive no implementation-failure credit.

## Verification and limits

Final full durable412.725s/tools15.198s; repository-wide vet/Staticcheck and logging pass. Public compaction/application/raw-storage/reopen/registry controls pass30 shuffled repetitions, seed42. The preceding compaction, legacy-task, constructor and telemetry selection passed three shuffled repetitions. The reference runner passes145 unchanged pinned tests plus31 independent probes. Pinned hashes/tree are checked; the retained source snapshot has no .git, so the runner does not independently reverify commit identity.

This closes the compaction-name and generic namespace failures demonstrated here. D09 and D16 stay open: raw `pi.generation`/`pi.tool` records still require private adapters, GenerationTask's source version2/checkpoint differs from the native projection, ToolTask's source version3/input union is not installed, and nested-tool naming differs. Compaction's complete input/checkpoint/settings/migration contract still needs assertion-level acceptance. No old alias, automatic migration of existing native records, full release profiling or full parity is established.

## Source transcript and prompt literals

The full original serialization packet (harness-compaction.test.ts:351–370) now passes natively, including every negative assertion: system text and thinking are absent; visible user/assistant text, the tool call and the exact truncated result remain. It failed first because the native serializer emitted thinking. The older native fixture expected that incompatible output; it now requires thinking to be absent rather than preserving a second contract.

Both summarization prompts now match the original source literals exactly. The generated fixture comes from scripts/capture-durable-compaction-prompts.mjs, which checks source SHA256 22e5e341e7e6aede218eb4f4c48784d59031c0660a2dea62bdb6da5fd9e8a2c4. Those bytes were independently compared with the raw GitHub file at the exact target commit. Five provider fixture files now identify summary requests using the original You are a context summarization assistant prefix instead of the former native Summarise marker; their semantic assertions remain.

Full uncached sequential durable829.093s/tools24.860s; ten new shuffled repetitions0.588s; focused compaction/summary/search16.213s; three semantic compiler faults rejected (thinking, system prompt, user prompt); vet, Staticcheck, logging and register pass. The original compaction file passes110 unchanged cases, plus61 supplemental cases (171 total), zero skipped. This reference execution is not110 accepted native counterparts. An earlier run was deliberately interrupted after spotting two remaining old routing markers; it is not a passing receipt. Raw overlays/logs are deleted.

Complete compaction acceptance remains open: private checkpoints, selection/recovery details, tool-argument property order, truncation across a UTF-16 surrogate pair and explicit-empty instructions still need direct ports/assertion mapping. This is not full alignment or pre-release profiling acceptance.

## UTF-16 truncation and regression findings

Tool-result truncation now follows the original String.slice boundary even when unit2000 is the high half of a supplementary scalar. A literal source-derived test failed on the old scalar-preserving cut; the paired original probe confirms the exact high unit and remaining count. The scoped checkpoint codec preserves that unit without widening ordinary caller text. The older native complete-scalar assertion now requires the original UTF-16 slice and count.

During full regression, two independent issues surfaced. DocumentWatch closed its completion channel before removing its Session registration; termination now removes it before Closed can release a waiter, with the established observerMu/watch.mu lock order. The case680 fixture used two immediate entry commits instead of the original deferred-document successor; it now checks the actual source packet, including exact first refusal identity, successor SessionFailed/cause and no publications. Refusal is injected at the writer sink, not through a conforming public Storage wrapper: D10 remains unaccepted.

Full uncached durable849.377s/tools26.940s;100 shuffled targeted repetitions39.619s; two semantic slice/count faults rejected; vet, Staticcheck and logging pass. Dead fixture-only landed/refusal helpers were then removed; three complete gate-suite repetitions6.941s and static gates pass. Selected reference254 cases (200 unchanged +54 probes), zero skipped.

Failed full runs are not green receipts:855.848s hit the repeated-call-ID native three-second wait;25 isolated repetitions passed, but its cause remains unknown and failure diagnostics are now retained. The next848.604s run exposed the watch race and incorrect case680 fixture fixed above. These timing/lifecycle caveats remain reported; no release acceptance is claimed. Analysed raw logs/overlays are deleted. Remaining compaction work includes instruction-presence/public API, argument property order and canonical checkpoint/selection/recovery contracts.

## Manual compaction API and instruction presence

ConversationHandle.Compact(ctx, instructions *string) now maps the original compact(instructions, context). Nil omits instructions; an explicit empty string remains present and adds the original empty Additional focus line. Public CompactionOptions is removed, without an alias or compatibility wrapper. Selection, budget and retries are settings, not private per-call payloads. The18 existing caller fixtures now configure the actual stored agent settings before compacting; their policy/ownership/reopen assertions remain. Native trigger/max policy extensions and complete policy mapping are not accepted by this slice.

The exact public creation input assertion failed first: native manual tasks leaked instructions:"", maxTokens:0 and retry defaults. Creation now stores exactly reason:manual plus instructions only when supplied. Per-call keepRecentTokens/maxTokens/retry reads are removed from execution. Nil, empty and nonempty instructions retain their presence through hooks, durable checkpoints and provider requests. Optional strings handed to hooks are detached: a dedicated mutation test failed before the copy and now preserves the request. The input pointer is also copied before returning to its caller. The independent original probe verifies the same raw input and focus-line cases.

Full uncached durable854.735s/tools26.544s; ten focused shuffled repetitions12.790s; four semantic compiler faults rejected (private input, missing empty focus, checkpoint omission and hook alias); vet, Staticcheck and logging pass. Selected original execution255 cases (200 unchanged +55 probes), zero skipped. Original compaction110-case file remains separately verified at the earlier checkpoint. Raw logs/overlays and migration scripts are deleted. This closes the demonstrated manual-call options and instruction-presence deviations, not D09/D13 or full alignment.
