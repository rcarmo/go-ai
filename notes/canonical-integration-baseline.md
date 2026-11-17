# Canonical integration baseline

Latest complete actual-tree run:56fdf5c plus environment error semantics and public metadata/reset consumers in the containing commit. Prior complete counts:53 (baseline21),56 (intermediate22),52 (23).

Command: `make test PACKAGES=./... TEST_FLAGS='-v -count=1 -p 1 -timeout 1200s'`, after sourcing scripts/project-env.sh. CGO disabled, sequential packages. No overlays or test exclusions.

**Integration is red.** Complete actual-tree baseline24: durable854.904s with48 failed top-level groups;25 other tested packages pass (tools14.876s/public12.266s). No panic, timeout, overlay or exclusions. Integration remains red;89c031e is still the last fully verified tree.

The four reconciled groups pass: environment factory failure/abort, requested thinking/protocol fields, queued entry metadata across reopen, and post-tools reset. [Mapping](canonical-builtin-execution.md#environment-errors-and-public-metadatareset-consumers).

The table is an observed failure inventory, not an assertion crosswalk or root-cause verdict. Names come from the complete run; file links are resolved from source declarations. The earlier forward-scanned diagnostic index was unreliable and is not used.

| Test | Native file |
| --- | --- |
| `TestFailedRun140BoundedWithdrawalRecovery` | [failed_run_140_test.go](../durable/failed_run_140_test.go) |
| `TestFailedRun140WithdrawalJoinsOwnedHosts` | [failed_run_140_test.go](../durable/failed_run_140_test.go) |
| `TestMainBoundaryEligibilityInheritedCancellationBeforePlacement` | [harness_boundary_eligibility_main_test.go](../durable/harness_boundary_eligibility_main_test.go) |
| `TestMainBoundaryEligibilityAbsentAgentHostAllModes` | [harness_boundary_eligibility_main_test.go](../durable/harness_boundary_eligibility_main_test.go) |
| `TestMainBoundaryEligibilityLocallyDecidedFailFast` | [harness_boundary_eligibility_main_test.go](../durable/harness_boundary_eligibility_main_test.go) |
| `TestMainBoundaryLegacyRecoveryBusyControl` | [harness_boundary_lifetime_main_test.go](../durable/harness_boundary_lifetime_main_test.go) |
| `TestMainHarnessRecoveryCrashAfterConversion` | [harness_generation_recovery_main_test.go](../durable/harness_generation_recovery_main_test.go) |
| `TestM1bTerminalAndExplicitUnsupported` | [harness_generation_test.go](../durable/harness_generation_test.go) |
| `TestMainInterruptedBudgetAndCloseAccounting` | [harness_interruption_budget_main_test.go](../durable/harness_interruption_budget_main_test.go) |
| `TestMainInterruptedPollRetryEdges` | [harness_interruption_poll_main_test.go](../durable/harness_interruption_poll_main_test.go) |
| `TestMainPollTerminalOnlyBoundaryDerived` | [harness_poll_boundary_main_test.go](../durable/harness_poll_boundary_main_test.go) |
| `TestMainAbortedPollCancelHandlerCutoff` | [harness_poll_boundary_main_test.go](../durable/harness_poll_boundary_main_test.go) |
| `TestMainPollCutoffSkipsRepeatedCancellationDerived` | [harness_poll_boundary_main_test.go](../durable/harness_poll_boundary_main_test.go) |
| `TestMainToolProgressDeferredMemoryAndPersistence` | [harness_tool_deferral_main_test.go](../durable/harness_tool_deferral_main_test.go) |
| `TestM1cActualHTTPToolResultAwareAnswer` | [harness_tools_test.go](../durable/harness_tools_test.go) |
| `TestM1cSchemaSubsetAndRepairBeforeIntent` | [harness_tools_test.go](../durable/harness_tools_test.go) |
| `TestM1cNotOfferedAndInvalidArgumentsZeroEffects` | [harness_tools_test.go](../durable/harness_tools_test.go) |
| `TestM1cAbortDrainsChildBeforeParentAndFencesEscapedAPI` | [harness_tools_test.go](../durable/harness_tools_test.go) |
| `TestM1cCloseNoncooperativeToolKeepsOwnershipAndReturnedOutcome` | [harness_tools_test.go](../durable/harness_tools_test.go) |
| `TestM1cRegistryPhasePinOutputCapAndAtomicAppFailure` | [harness_tools_test.go](../durable/harness_tools_test.go) |
| `TestM1cInvalidToolUsageSettlesOnceNoAppOrReplay` | [harness_tools_test.go](../durable/harness_tools_test.go) |
| `TestM1cAbortAtOfferedCallBoundaryStartsNoTool` | [harness_tools_test.go](../durable/harness_tools_test.go) |
| `TestBeforeToolBlockOrThrowPreventsEffectAndRevalidatesReplacement` | [hooks_test.go](../durable/hooks_test.go) |
| `TestIdentity140DurableDefaultHostAndPinnedOverrides` | [identity_140_test.go](../durable/identity_140_test.go) |
| `TestReaderUTF16MainCommittedEventsAndSnapshot` | [reader_utf16_main_test.go](../durable/reader_utf16_main_test.go) |
| `TestReport140ModelHostFailuresReportAfterFallbackAdoption` | [report_140_test.go](../durable/report_140_test.go) |
| `TestReport140StructuredOutputUsesCapturedProducer` | [report_140_test.go](../durable/report_140_test.go) |
| `TestRestartLimit140BuiltinGenerationAbortCutoffSettlesSubmission` | [restart_limit_140_test.go](../durable/restart_limit_140_test.go) |
| `TestStopBackgroundSuppressesReportAndChildStaysUsable` | [subagent_test.go](../durable/subagent_test.go) |
| `TestTaskRecoveryBuiltinHoldFinalFailureAndReopen` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoveryToolOutcomePanicRollbackFallbackAndReopen` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoveryGenerationPreparationFallbackWakeSequence` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoveryOfflineGenerationPreparationFallbackWakeSequence` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoveryToolDecidingHoldRejectedUncertainAndReopen` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoveryQueuedWithdrawalOwnedChildDefersSettlement` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoveryGenerationDecidingFinalFailureMatrix` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoveryGenerationHostCallbacksPreparationAndDispatchedReceipts` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoveryHostPanicDecidingStoragePrefixAndReopen` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoveryToolValidatorHostFailureBeforeIntentAndDecidingReopen` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoverySuccessfulValidatorIntentStoragePrefixAndReopen` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoveryOrderedToolContinuationAdmissionCloseAndLateOwnedWork` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskSchedulerCapacityToolChildHoldOne` | [task_scheduler_test.go](../durable/task_scheduler_test.go) |
| `TestMainTaskTraceQueuedFirstInvocation` | [task_trace_main_test.go](../durable/task_trace_main_test.go) |
| `TestTelemetry140AdoptedEntryAndSettlementEvents` | [telemetry_140_test.go](../durable/telemetry_140_test.go) |
| `TestToolRecoveryPinnedCurrentAgentCwdAndDeselection` | [tool_selection_recovery_test.go](../durable/tool_selection_recovery_test.go) |
| `TestToolUTF16MainExecutionReopenHooks` | [tool_utf16_main_test.go](../durable/tool_utf16_main_test.go) |
| `TestMainViewUpstreamGenerationValueOperations` | [view_generation_oracle_main_test.go](../durable/view_generation_oracle_main_test.go) |
| `TestMainViewUpstreamGenerationOracle` | [view_generation_persistence_main_test.go](../durable/view_generation_persistence_main_test.go) |
