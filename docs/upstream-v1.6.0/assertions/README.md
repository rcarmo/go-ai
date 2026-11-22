# v1.6.0 assertion ledger: round 1

Oracle: official pi `faac585884ae2c013a6b1400f3e9e77f10b34bac`. Native baseline: `3507d5974397ef7ee42373e35b44637174bd670e`. This is a bounded work queue, not a parity report. The [crosswalk](../README.md) records the release pin, assets and verification receipts.

| Lane file | Scope |
| --- | --- |
| [`durable-park-children.jsonl`](durable-park-children.jsonl) | All ten originals in `harness-park-children.test.ts`: park/recovery, keyed children and submission notifications. |
| [`codemode.jsonl`](codemode.jsonl) | All seven description/prompt originals, guest discovery and ten selected guest/journal replay originals. Other Codemode originals are out of this slice, not excluded from acceptance. |
| [`codex.jsonl`](codex.jsonl) | Five SSE request projection originals plus selected WebSocket cache, lifetime, timeout, replay, and continuation declarations. The two parameterized declarations identify their variants. |

## Read and update the records

Each file has one `manifest` row (format `schema: 1`) with the Oracle path hashes, historical receipts and native test references, followed by these row types:

- `case`: exact pinned name and source line span. `original` names a receipt for a known pass of the unchanged original, or is `null` if no case-level pass is recorded. A suite count does not establish a filtered selection. The six-case Codex receipt lacks an enumerated selector in the crosswalk; do not assign it to individual cases by inference.
- `assertion`: one source `expect()` site. The stable key is `<case-id>#L<source-line>` within this immutable pin, for example `durable.park-children.abandon-interrupted-parent#L296`. `expect` records the first line; read the whole pinned statement for the exact comparison, including all fields of multiline objects. `variants` on the case enumerates parameter sets; `restricted` identifies a branch that runs an assertion only for specified variants. A site is not an executed test count. A mapping must hold for every applicable variant; record `partial` and name covered/missing variants if it does not.
- `supplemental`: a related native probe, not an assertion translation. `gap`: a requested source-backed case or evidence action that is not itself an original assertion site.

| Assertion status | Meaning |
| --- | --- |
| `mapped` | The cited native check establishes this source assertion's contract through the native seam, and the cited packet has a focused native pass. Fixture differences do not waive required shape, count, state, or ordering. |
| `partial` | The native check passed but covers only part of the contract or lacks an exact comparison. The note names the gap. |
| `candidate` | A related native check was found but has no adequate focused receipt or exact mapping here. |
| `unmatched` | No corresponding native check is claimed. The original can still have passed. |

A `native` reference points to the named test/subtest (and optionally a helper via the `anchor` text). Keep original and native results separate. Only D25 alternate-engine availability has prior authorization for exclusion, and none of these rows uses it. Missing credential, platform, dependency or test evidence remains open. Do not use a row count as a test count, a whole-portfolio denominator, or the transitive inventory.

## Priority and dependencies

1. **Durable: `durable.park-children.abandon-interrupted-parent` (`#L294` through `#L300`).** Reproduce cutoff after the child is durably parked and while the replay-unsafe caller is still running. Compare the public submission outcome, child's committed restart reason and terminal state, exact `afterTool` diagnostic, and absent nested status. Keep the retained-parent and explicit-abort controls; verify that restart cannot rerun the unsafe executor. After this gate, close partial recovery/notification checks using the public task and storage receipts.
2. **AI: `ai.codex.ws-started-limit-reconnect`, then `ai.codex.ws-missing-cached-continuation`.** Inventory the two native recovery probes before writing more fixtures. Separate pre-output internal reconnect from retry after response metadata, rate-limit metadata and partial thinking. For missing continuation, check the started stream delivers exactly one public `start`, no leaked error, then compare the retried full input, connection IDs, exact delta and counts for both WebSocket and SSE. Next, address runtime cache boundaries (account, retention, age) and timeouts; do not infer a timeout from an immediate setup failure. Close the request-projection selector and exact wire omissions separately.
3. **Codemode: `codemode.namespace.start-snapshot-on-reopen`, paired with `codemode.replay.completed-calls`.** Compare the pinned start-record capture and discovery use (`packages/durable/src/codemode/codemode.ts:391–418,909–929`). Reopen via the public harness between a confirmed guest read and the next live effect; replace namespace metadata, membership and callable definitions independently. Check replayed data use the start record and a new forbidden call is not admitted. The declaration-replacement scenario is supplemental unless an exact pinned original is found. Then map exact guest shape, deferred prompt equality and journal/replay outcomes.
4. **Evidence: reconcile the landed lane commits, then add missing scope.** Expand from these cases to the unselected originals, including unchanged behavior. Track exact dependency/module and published-asset versions separately. Record credential and platform requirements as open obligations, and only resolve them with actual evidence or explicit authorization. Release acceptance remains a separate gate.

## Handoff template

```text
Lane / local commit SHA / base SHA:
Oracle pin / case names / selectors / unchanged-original outcomes:
Ledger keys and variants upgraded, and why the native comparison is exact:
Native command and public seams / pass counts limited to the selected tests:
Executable pre-fix red: base, command and expected failure:
Fault control: mutation, expected assertion failure, reverted before verification:
Vet / Staticcheck / diff and build settings:
Open or partial keys / prerequisite / next concrete action:
Supplemental probes (separate from translations):
Skips or exclusions (none, or explicit authorization receipt):
```

Run one shared heavyweight verification job at a time with `flock /tmp/go-ai/v160-focused-verify.lock`. The root integrates code, crosswalk and ledger updates. Only the root pushes. No broad suite or release gate runs as part of this ledger check.

## Check the ledger

From the repository root:

```sh
bun run docs/upstream-v1.6.0/assertions/validate.ts "$oracle" "$pinned_git_mirror"
```

The optional second argument is a git repository containing the pinned commit. The checker verifies pinned bytes, locator coverage and links within the scoped declarations; it does not execute tests or decide semantic equivalence. Review the native comparator and the receipt before upgrading a row.
