# pi v1.6.0 parity target

The upstream target is [pi v1.6.0](https://github.com/earendil-works/pi/releases/tag/v1.6.0) at `faac585884ae2c013a6b1400f3e9e77f10b34bac`. The prior [v1.5.2 notes](../../notes/upstream-v1.5.2-restart.md) and [v1.5.0 acceptance evidence](../upstream-v1.5.0/) are historical. The model-regeneration gate is pinned to v1.6.0. The repo-wide acceptance gate has not been switched.

## Verified inputs

[`source-delta.tsv`](source-delta.tsv) compares v1.5.2 and v1.6.0 across AI, Durable, Codemode, Schema, Telemetry, Chord, and the npm lockfile. The inventory checks 2,148 tracked files byte for byte and reports 396 changed paths. It includes all of Chord to expose dependency changes for review. It is not a transitive dependency inventory, assertion ledger, or approved exclusion list.

```sh
bun run scripts/inventory-upstream-v152.ts /path/to/pi-git /path/to/v1.6.0-source --v1.6.0
```

The published npm archives match registry SHA-512 integrity. Their SHA-256 digests are:

| Package | SHA-256 |
| --- | --- |
| `@earendil-works/pi-ai` | `d68312ec574bc959cb7fee26da4f4876d067aed9bbbe76e876359e60d2294747` |
| `@earendil-works/pi-durable` | `e76ddd7fdf50d53a9945de87adfd620f5734df6d57a075ef8168948ba6ddd855` |
| `@earendil-works/pi-codemode` | `22f7dabd30eeaac7e38a133b72008ccfc379732f2ed86d760297cb9d385a42db` |
| `@earendil-works/pi-schema` | `a308dfc8981c5badc8e113a97d5390788d6bcc82ed4e2aa2708844438ef88d25` |
| `@earendil-works/chord` | `3967193a8f1bdb79881e4f0715a170853a5946fbb4326f2403529a5a9faedc17` |
| `@earendil-works/pi-telemetry` | `7a28233ebfdd751940619be6a879bda0fe6eb31ac2056e8798e16293c0c39ec5` |

## First review

- Durable adds typed signals, signal-only sleep, waits that resume when any listed task ends, tool parks, nonblocking nested calls and submission notifications. Review these together with persisted generation/tool version upgrades, wake recovery and hook child tasks.
- Compare native lifecycle workarounds against the new abort, waiting and ownership tests before removing them. `session-group-commit.test.ts` is unchanged; v1.6.0 does not remove the between-part publication requirement.
- Review Codemode extension namespaces and the image-fit pipeline in Durable as well as in the standalone packages. The `packages/codemode` and `packages/schema` deltas only touch release metadata; outstanding native gaps still apply.
- AI changes include provider retry rules, resource handling, and the published model catalogue. Port focused assertions before changing the model-regeneration gate.

## Test consolidation and focused receipts

`19e16c5` merged provider cause tests into `tests/error_body_test.go`, source-derived retry cases into `tests/retry_assistant_test.go`, and both generations of line-read tests into `durable/session_read_line_test.go`. Four test files were removed and one renamed. A Go parser comparison found all 63 non-import declarations unchanged. The selected Go tests passed before the move and twice after it; vet, Staticcheck, and diff checks passed. No native assertion was removed or renamed.

The original v1.6.0 files ran unchanged: AI `error-body.test.ts` and `retry.test.ts` passed 73 cases; Durable `session-group-commit.test.ts` and `harness-signals.test.ts` passed 61. The workspace runs used the published, integrity-checked dependency `dist` assets; Vitest's incomplete Bun install was replaced with the exact locked tarball. These are focused source receipts, not native acceptance.

### Connection retry classification

The 13 new cases in `packages/ai/test/retry.test.ts` map to `TestSourceV160RetryConnectionFailures` in the consolidated retry suite. The public classifier now recognizes both Codex WebSocket lifetime-limit encodings, standalone socket codes, native peer-reset wording, and a gateway's `Connection closed.` error. Generic connection limits and socket codes embedded in identifiers are not retried. Nine positive cases failed before the implementation. Dropping underscore from the boundary caused the exact `ERR_EPIPE_ID` negative case to fail. The 52 unchanged source cases, focused Go regressions, vet, Staticcheck, logging and diff checks passed. This verifies classification, not WebSocket reconnection or session-resource handling.

### Retaining queued input on conversation abort

`harness-input-entries.test.ts:258` maps to `TestSourceV160SubmissionOnAbortKeep`. The native test uses the real Harness on SQLite with channel-controlled provider requests. It checks both aborted settlements, the exact retained inbox item and its entry metadata, queued status without a live run, later placement in provider order, and explicit withdrawal of a kept input.

With only the new input field in place, the compiled test timed out waiting for the ordinary queued input: the old abort path required a placeholder generation task, which canonical inbox-only follow-ups do not have. The current path settles eligible inbox-only inputs and removes them in the same transaction. It leaves queued writes and `onAbort: "keep"` items alone. Removing the keep check emptied the inbox and failed exact equality. All 12 unchanged source cases passed, plus focused Go regressions twice (7.999s), vet, Staticcheck, logging and diff checks. Cascade-specific retained-input assertions are still open.

### Published model catalogue

The regeneration gate uses the verified v1.6.0 `pi-ai` archive. Its schema-v6 manifest covers 43 provider files and has SHA-256 `cc46553f3f008788e667600a873d3d83a4df8fe46c67b06c65b16fd828450e8b`. Before the port, the generator rejected the release's structure hash. It now accepts the exact manifest and verifies every provider hash and the record structure. Historical manifest checks remain callable.

The three generated files match fresh generation byte for byte. The public registry comparator matches all 1,698 chat, 74 image and 81 classifier records, not just their IDs. Sample expectations changed only where the published records changed, including Mistral cache pricing, GLM and Kimi metadata, and absent Laguna records. The manifest/provider integrity tests and metadata-corruption self-test passed, as did focused Go tests and 74 unchanged upstream cases in `models-runtime`, `models-entry` and `model-data-validation`. Vet, Staticcheck, logging and diff checks passed.
