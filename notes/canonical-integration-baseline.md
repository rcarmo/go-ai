# Canonical integration baseline

Latest complete actual-tree run:ac28bb7 plus structured-output report attribution and public telemetry/report consumers in the containing commit. Prior counts:52 (23),48 (24),46 (25).

Command: `make test PACKAGES=./... TEST_FLAGS='-v -count=1 -p 1 -timeout 1200s'`, after sourcing scripts/project-env.sh. CGO disabled, sequential packages. No overlays or test exclusions.

**Integration is red.** Complete actual-tree baseline26: durable838.350s with43 failed top-level groups;25 other tested packages pass (tools15.365s/public12.512s). No panic, timeout, overlay or exclusions. Integration remains red;89c031e is still the last fully verified tree.

All three report/telemetry groups reconciled in this packet pass. [Mapping](canonical-builtin-execution.md#phase-tool-report-origin-and-public-write-telemetry).

This is an observed failure inventory, not an assertion crosswalk or root-cause verdict. Names come from the complete run; links resolve source declarations. Earlier forward-scanned diagnostic associations are not used.

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
| `TestToolRecoveryPinnedCurrentAgentCwdAndDeselection` | [tool_selection_recovery_test.go](../durable/tool_selection_recovery_test.go) |
| `TestMainViewUpstreamGenerationValueOperations` | [view_generation_oracle_main_test.go](../durable/view_generation_oracle_main_test.go) |
| `TestMainViewUpstreamGenerationOracle` | [view_generation_persistence_main_test.go](../durable/view_generation_persistence_main_test.go) |
