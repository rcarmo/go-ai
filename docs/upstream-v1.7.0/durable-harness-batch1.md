# Unchanged Durable harness and transcript originals

This bounded packet owns the eight exact pi v1.7.0 Durable originals named below at `fb0fe823539f6d04850f6e34748c01f01f212e1f`. The isolated native lane starts at `5917e027185304af08f09718a88351f1cee05ff2`. It does not overlap the completed session, view, blocked, cancellation, close, turn-limit, environment or storage packets.

## Pinned originals

| File | Git blob | SHA-256 | Result |
| --- | --- | --- | ---: |
| `conversation-transcript.test.ts` | `985941ef9ca1d247ede6df05cd2f47ad69e6a6f1` | `775ef73de2f397ffc2fea610e24afbdb18572982d3c549893f05dddd7826b621` | 16 |
| `harness-after-run.test.ts` | `72188bce2ca3a00659cafa66c7d2af5be19a49cc` | `6f93887a1568db233b5647e0db399548b2e689e4a7ec1afca712ba9664c8fcc0` | 29 |
| `harness-callers.test.ts` | `bbf4e7db922228b11e8181edbde7d86aaa67a3ae` | `9e4cea225cac010778395935df0975adb0a8cf8573d213dc5c505f61302ba4e3` | 5 |
| `harness-compaction.test.ts` | `cc6b701e1f7a144ea1347312569fcebe27525340` | `092378ce9747ec53213c493e021cc5d3ee21d07132311360201c7c794a090a3c` | 114 |
| `harness-context-range.test.ts` | `d44cab3db53bc2ce4776967234418525920194db` | `acd35d7585a3ac1063c4f335a7779d5b86e3422559707e8c3e2b408cce09d2ca` | 1 |
| `harness-context.test.ts` | `4c85b1c2b8d6822d4718e0f5808bc13789db4379` | `ddb45ba6d40d139f20bd9d5b31ecf0e6614c1b94614c9ddebb799bb06bc908c2` | 22 |
| `harness-continue.test.ts` | `627e6218fd5400d5213808b15f500ba240b6668b` | `c6021bdd60c8bbc8e53232b177cb37894c41e7208c698a45942144c8d8e57081` | 13 |
| `harness-conversations.test.ts` | `fb3b8b71f8e1bc3c39732f26ce5138570ff5dd7f` | `9de21114fcad5d101a8d3e42e7b516c507338356b8d2e95ec965af827a893b81` | 12 |

Each staged file matched the pinned Git blob and SHA-256 byte for byte. The checkout remained detached at the pin. Node 26.13.0 and Vitest 4.1.12 ran the unchanged files together, one worker at a time with `NODE_OPTIONS=--max-old-space-size=650` and the shared `/tmp/go-ai/v170-focused-verify.lock`: **212 passed, 0 failed, 0 skipped**.

```sh
cd "$PI_MIRROR/packages/durable"
NODE_OPTIONS=--max-old-space-size=650 flock /tmp/go-ai/v170-focused-verify.lock \
  node ../../node_modules/vitest/vitest.mjs run --maxWorkers=1 --fileParallelism=false \
  test/conversation-transcript.test.ts test/harness-after-run.test.ts \
  test/harness-callers.test.ts test/harness-compaction.test.ts \
  test/harness-context-range.test.ts test/harness-context.test.ts \
  test/harness-continue.test.ts test/harness-conversations.test.ts
```

## Native public controls

The unchanged production Go implementation was compared first. The neutral public controls use real SQLite records, conversations, generation/provider paths, watches and hooks; no Memory, Journal or Chord backend was substituted.

- Transcript/window behavior: `TestMainTranscriptWindowHydration`, `TestMainTranscriptIndependentWindowGrowthAndTrim`, `TestMainTranscriptWindowForkHistory`, `TestMainTranscriptWindowEntryOperationSequence` and `TestMainViewUpstreamHistoryOracles`.
- After-run hooks and ownership: `TestMainAfterRunAnswerTasksHoldRun`, `TestMainAfterRunHookReadsCommittedEntryBoundaries`, `TestMainAfterRunQueuedBoundary`, `TestMainAfterRunTerminateSummary`, and recovery/deselection/fork controls.
- Caller boundaries: `TestToolCallerBoundariesRemainDetachedAndPersisted`, `TestAgentFiltersPinnedLiveDefaultsAddRemoveAndToolControl`, `TestSourceCanonicalToolNestedUsesSameDefinition` and `TestSourceCanonicalToolNestedUnavailableSettles`.
- Compaction: cut, summary, retry, threshold, overflow, reopen and canonical request controls in the existing compaction suite; `TestManualCompactionPinsCutUsageAndReopensWithoutReplay` is the representative SQLite receipt.
- Context and context ranges: `TestMainContextReadPublicationWitness`, `TestMainContextReadInstallationFences`, `TestMainContextReadWrapperAuthority`, `TestMainContextReadPublicOffLine` and the public transcript/context equivalence controls.
- Continue: `TestMainContinueFailedRunWithoutNewUserEntry`, `TestMainContinueBusyRejectAndRequestIDDedup`, `TestMainContinueNothingToAnswerEmptyAndAnswered`, `TestMainContinueLengthLimitedToolCall`, and SQLite/reopen controls.
- Conversations: `TestConversationInitPinnedCreationHookThenAgentThenInitAtomicRollback`, `TestConversationCreatedPinnedRawOwnedForkOrderAndRollback`, and public agent/configuration/document controls.

The new neutral caller-boundary test passed first against unchanged production code. It checks model-only versus tool-only selection, model filtering without changing tool-call authority, canonical persisted `modelTools`, and detached public reads. The separate `TestAdmittedCancellationAndCloseFence`, `TestTaskRecoveryHostCallbackFencesPreserveNormalErrorIdentity` and `TestTaskOwnershipCancelledOwnerLateChildConversationWorkAfterReopen` controls also passed twice. No production mismatch was demonstrated in the selected public paths, so no runtime, schema, ownership, redaction or cancellation change was made.

All 751 direct `expect` locators in the eight owned originals are recorded as partial representative mappings in [the assertion ledger](assertions/durable-harness-batch1.jsonl). `partial` is intentional: the Go selectors assert observable public SQLite records, hook execution and protocol outcomes, not JavaScript private helper identity, MemoryStorage internals or exact object references. Twelve parameter-expanded original cases are represented by their 200 source declarations; the runtime total is 212 cases.

## Reproduction and limits

```sh
source scripts/project-env.sh
export CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1 GOTOOLCHAIN=go1.27.3
flock -w 300 /tmp/go-ai/v170-focused-verify.lock \
  go test ./durable -run 'SELECTOR' -count=2 -shuffle=42
```

`SELECTOR` is the exact grouped native locator set in the lane receipt. The final focused selector runs twice, followed by `go vet ./durable` and Staticcheck `v0.8.2 ./durable` under the same bounded pure-Go environment.

This packet does not claim byte identity of JavaScript helper objects, Chord tables, private Session implementation counters, alternate storage engines, exact UI transcript identity or full `harness-compaction` one-to-one coverage. It compares public SQLite, canonical DTO/record visibility, hook ordering, ownership and cancellation boundaries where representable. It is a scoped unchanged-source comparison, not full acceptance or acceptance profiling.
