# Deferred Harness assertion reconciliation

This is a static review of all11 declarations in `packages/durable/test/harness-deferred-progress.test.ts` at official v1.5.0, `f9a26365568127461c47a07a37dcb42c140a3ac5`, against the native scenarios through `a08d73b1c69fe52d287c61a980e4d011b44d6762`, plus four assertion strengthenings below. Production remains `decbd9d4419d0580c6b84e6832b07d7893573444`. It is not TypeScript execution or full-body parity acceptance.

## Declaration and assertion map

Function names below are in `durable/`. Related validation receipts remain at their original native/source pins; this review does not repeat their tests.

| Source lines | Source assertions and conditions | Native function and reconciliation |
| --- | --- | --- |
|100-114|640-character `LONG` prefix; memory count>12; all document-change batches<memory/1.5.|`harness_deferred_progress_main_test.go::TestMainHarnessDeferredProgressBatching` uses the literal source answer,20 acknowledged32-character prefixes, a400ms storage window, actual append records and both inequalities. Controlled pacing replaces the source faux stream.|
|116-156|Stored text>40; stored text occurred in memory and prefixes the last memory text; stored-publication replica equals storage after crash; resumed submission done; exactly one aborted stored answer then `answer`; live state{}.|`harness_generation_recovery_main_test.go::TestMainHarnessRecoveryStoredPartialAndReplica` and `mainFinishRecovery` check each result. A full replica comparison **after** failed-host shutdown is now explicit, in addition to its previous pre-crash comparison. Async native callbacks are acknowledged before loss.|
|158-175|Held memory text>40 at infinite window; no stored attempt/generation or text after crash; done with only `answer`.|`TestMainHarnessRecoveryBeforeFirstStore` in the same file acknowledges64 characters, checks absent generation/text and the exact final answer array.|
|177-207|Stored text>40; first crash, reopen without resume and second crash preserve it; a resend at infinite window followed by a third crash leaves no stored text; done with one aborted stored answer and `answer`.|`TestMainHarnessRecoveryRepeatedCrashes` preserves that three-crash sequence and exact answer array. Its stored-text>40 precondition is now explicit rather than relying on the known64-character fixture prefix.|
|210-226|Held memory text>40; clean close stores the last memory value; recovered first answer is that aborted value.|`harness_close_recovery_main_test.go::TestMainHarnessCleanCloseStoresHeldGeneration` checks the stored value and stronger full answer array. It samples the last acknowledged prefix before Close; the controlled producer emits nothing during Close. Source samples its synchronous memory feed after Close, so concurrent-close observation equivalence is not established here.|
|228-248|Deferred handle stored before crash; resume clock1000→61000; settled submission is done/input; its answer is `deferred answer`; fetch count1.|`TestMainHarnessDeferredPollAfterCrash` checks all these values plus no redispatch/early poll/extra attempt. Canonical `SubmissionHandle.Status()` now explicitly checks done/input/nonzero answer. Legacy raw `Submission.Type` is `follow-up`, not the canonical `input`; no runtime representation changed.|
|302-339|Before each of two crashes, own stored output starts with `run N `; reopened tool restarts=N.|`harness_tool_recovery_main_test.go::TestMainToolProgressDoesNotResetCrashRestarts` uses `mainStoredToolPrefix` and now explicitly checks invocation number=N before publishing that run's output, retaining restart checks1/2. Commands replace the20ms loop.|
|341-364|Unsafe stored output>20 prefixes emitted lines; recovered submission done; execution count1; exact stored-prefix/interruption-diagnostic result.|`TestMainUnsafeToolCrashUsesStoredOutput` uses the helper's length/prefix condition, checks crash storage stability, done/count1 and the literal diagnostic/result array.|
|366-394|Safe stored output>20; second execution stalls; output durably absent before it emits and after a second crash; final submission done, count3, exact `rerun 3\n` result.|`TestMainSafeToolReplayClearsOutputThroughSecondCrash` retains each condition and assertion. The real execution channel acknowledges the silent second invocation.|
|398-426|Three long conversation pairs; source compaction settings; crash during summary request; original task completed, a compaction entry appears, live state{}.|`harness_summary_recovery_main_test.go::TestMainHarnessSummarisationCrashResumesCheckpoint` preserves the messages/settings and assertions, adding pinned-input/five-call/one-summary controls. Native model contribution location is explicit in [the receipt](summary-dependent-recovery.md).|
|430-475|Actual third-party Runtime.Commit reads held text>40; its derived entry and document change share a storage transaction; after crash stored text equals derived text; recovered first answer is that aborted text.|`harness_dependent_read_main_test.go::TestMainHarnessThirdPartyCommitStoresDerivedPartialAtomically` uses public task/transaction APIs, actual append interception and exact aborted-plus-final answers. Document-ID/object-Entry adapters are explicit in [the receipt](summary-dependent-recovery.md).|

All11 declarations now have scenario anchors. This does not establish that every literal fixture setting, callback timing, representation or whole-body effect is equivalent.

## A concrete remaining fixture mismatch

Source `slowSetup` at29-35 sets retry enabled, maximum5 and base delay1ms. Native recovery helpers inherit `defaultHarnessSettings` (`durable/settings.go:74`): enabled, maximum3, base delay2000ms. `mainOpenRecovery` installs progress settings without those retry overrides. The batching fixture also installs progress settings only. The nine declarations using source `slowSetup` therefore do **not** have literal retry-configuration correspondence, even though their recorded nominal outcome assertions pass.

Next: add a source-literal fixture-policy check, establish its unchanged-fixture failure, align the fixture configuration without changing production defaults, and validate only affected contracts. Only investigate a runtime change if that source-configured path demonstrates a behavior gap. No retry-policy divergence is accepted.

Other adapters remain: deterministic clock steps, zero generation-publication interval, acknowledged watch delivery instead of the source's synchronous local feed, explicit Resume calls, failed-Session/join/reopen instead of in-process Session failure, synthetic ASCII prefix text for several recovery cases, command-paced tool output and native record/entry/document addressing. These are documented test boundaries, not acceptance of differing observable contracts.

## Four strengthened checks

This checkpoint changes tests only: post-crash full replica equality, repeated-crash stored length>40, deferred canonical input settlement and own tool invocation number. An initial attempted `input` assertion on the **legacy raw** submission failed; inspection of `nativeSubmission` confirmed the raw `follow-up` mapping. The final check uses the existing public canonical Status API and keeps the original answer/count assertions.

Go1.27.3, `GOMAXPROCS=4`, project environment, protected prototype excluded:

- Four changed functions only, normal count3/shuffle42: CGO1 2.554s; CGO0 1.521s.
- Same functions, race count3/shuffle7, CGO1:16.376s.
- Active wrong-canonical-type fault: all three deferred-poll backend leaves fail the new input-settlement check, not compilation or process timeout.
- Durable vet and Staticcheck v0.8.2 in both CGO modes, module verification, formatting, diff and prototype-content checks pass. Staticcheck uses a physical source copy excluding the prototype.

Pattern: `^(TestMainHarnessRecovery(StoredPartialAndReplica|RepeatedCrashes)|TestMainHarnessDeferredPollAfterCrash|TestMainToolProgressDoesNotResetCrashRestarts)$`. No complete-file or unrelated closed-suite rerun supplies new credit.

The canonical review/generated362-row crosswalk update one evidence cell only. Flags, dispositions, inventory, the210-row dependency queue and historical reviews stay unchanged. Completed owned extracts/copies/overlays/logs/build cache are disposed; installed dependencies/toolchains and continuing pinned source are preserved.

## Not accepted by this review

The file stays `reviewed-open`. Literal retry configuration and callback/fixture equivalence remain open. Shared public identity, tracker/WeakRef, synchronous observer/reporting and broader abort/flush ordering require their own source contracts. Source228 is deferred polling, not abort-during-flush. Cross-package/schema/support/catalogue review and authorised acceptance CPU/allocation profiling remain unverified. No TypeScript, full-suite, profiling, dependency/storage-format, CI, release or full-v1.5.0-alignment result is claimed.
