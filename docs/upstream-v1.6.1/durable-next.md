# Next Durable comparisons

Three selected unchanged originals passed on the pinned v1.6.1 Node setup. The changed test files, source paths and Vitest config were checked against Git before execution. The [Round 2 verification log](README.md#round-2-publication-invalid-parks-signals-and-catalog) now records the tagged signal results and the scoped invalid-park counterpart.

| Original | What to compare next |
| --- | --- |
| `harness-tool-parks.test.ts:293–318` — invalid park through `afterTool` | The source now supplies `{kind: "nap", until: 1}` instead of a mixed sleep/wait object. Compare the `done` submission, error result and invalid-park text, no live tool, the failing owned task and one after-tool report. Native `TestSourceV160CanonicalToolTimedAndInvalidParks` has representable rejection cases; its `mixed` case is not an exact unknown-kind input. |
| `harness-signals.test.ts:129–152` — ordered concurrent deliveries | Compare `delivered`, `duplicate`, `delivered` and the two ordered winning values. Upstream now returns states `{kind: "value", value}`. Distinguish the caller-facing state from the persisted memo before changing anything. Start from `TestSourceV160ParallelSignalsComposeDeliveriesInOrder`. |
| `harness-signals.test.ts:231–245` — late delivery and expiry | Compare the `closed` delivery, expired wake and `{kind: "expired"}` state. Start from `TestSourceV160LateSignalExpiresBeforeReservation`. The Go `SignalState.MarshalJSON` currently emits `{value}` or `{expired:true}`; this source inspection is not the native baseline. |

The scheduler's new fit/blocked, owner-chain and wait-check discriminants are private shapes. Test blocked reasons, ownership refusal, wake behavior and report attribution through public paths; do not map the discriminants as stored or public records. The owned-child park fixtures changed their tagged syntax without adding assertions; the native owned-child wake probe remains a starting point.

The unknown-kind park remains a representation gap; the native mixed case is not its substitute. Scheduler and ownership observations remain separate from this scope. The historical after-run draft remains unverified and unmerged.
