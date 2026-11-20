# pi v1.6.0 parity target

The upstream target is [pi v1.6.0](https://github.com/earendil-works/pi/releases/tag/v1.6.0) at `faac585884ae2c013a6b1400f3e9e77f10b34bac`. The prior [v1.5.2 notes](../../notes/upstream-v1.5.2-restart.md) and [v1.5.0 acceptance evidence](../upstream-v1.5.0/) are historical. No repo-wide acceptance gate or model-regeneration pin has been switched.

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
