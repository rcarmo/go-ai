# Canonical integration baseline

Latest complete run:e9c0f61 plus the selected schema/UTF-16 packet and source-reconciled registration consumers. Prior complete groups:40 (28),41 (29). Baseline27 was interrupted without a durable result. Baseline30 compiled before the final full-checkpoint assertion strengthening; that stronger assertion and expanded selection subsequently pass three-shuffle60.371s. No runtime changes followed baseline30.

Command: `make test PACKAGES=./... TEST_FLAGS='-v -count=1 -p 1 -timeout 1200s'`, after sourcing scripts/project-env.sh. CGO disabled, sequential packages. Baseline30 ran in a separate tmux session with a saved exit code. No overlays or test exclusions.

**Integration is red.** Complete baseline30: durable866.370s with39 failed top-level groups;25 other tested packages pass (tools14.832s/public11.887s). No panic, timeout, overlay or exclusions. Integration remains red;89c031e is still the last fully verified tree.

Selected schema, repair and registration/wrapper consumers pass. [Mapping](canonical-builtin-execution.md#selected-tool-parameter-schemas-and-utf-16-validation).

This is an observed failure inventory, not an assertion crosswalk or root-cause verdict. Names come from the complete run; links resolve source declarations.

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
| `TestM1cAbortDrainsChildBeforeParentAndFencesEscapedAPI` | [harness_tools_test.go](../durable/harness_tools_test.go) |
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
