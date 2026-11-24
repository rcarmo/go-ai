# Round 33: evidence for all 42 changed primary suites

The [immutable snapshot](primary-queue-r33.jsonl) finds committed evidence of upstream execution for **41 of 42** changed primary suites, up from 29 in the [Round 21 audit](primary-queue-r21.md). The only file with no original run on record is the live OpenAI and Codex reasoning replay file. The 42 paths are 22 AI and 20 Durable, including two Durable-hosted Codemode files. This audit reads pi v1.6.0 at `faac585884ae2c013a6b1400f3e9e77f10b34bac` and native evidence through `ebe8ec7ab0a1f2da0a1641a9b63c633a48f1a707`. It ran no tests.

| Strongest original evidence | All 42 | Historical 37 |
| --- | ---: | ---: |
| Whole-file machine receipt | 3 | 3 |
| Complete, disjoint machine selections | 2 | 2 |
| Complete across machine receipts and a named prose result | 1 | 1 |
| Whole-file prose claim | 23 | 20 |
| Whole-files prose aggregate | 2 | 1 |
| Selected machine receipt, no full report | 3 | 3 |
| Selected prose or ledger claim | 4 | 3 |
| Named prose aggregate with no per-file width | 3 | 3 |
| No original run found | 1 | 1 |

Across the 42 suites there are **16 machine receipts covering 10 files**, **34 files with named prose claims**, and **25 with original-run links in the assertion ledger**. These sets overlap. The strongest evidence table assigns one state per file; it does not add overlapping tests or turn a prose count into a captured report. A ledger receipt can cover only a named selection. On the native side **25 files have assertion indexes**, **15 have documented comparisons without an assertion index**, and **two have no file-specific comparison** (`spec-usage` and live replay). An index or native comparison does not imply a whole-file mapping. The independent ledger validator checked **1,517 scoped locators** against source bytes, without running tests.

## Exact machine widths

| File | Recorded width |
| --- | --- |
| AI `fetch-option`, `llama-cpp-classify`; Durable `env-node-list-race` | Whole-file machine receipts for 6, 15, and 1 cases respectively. |
| AI `fireworks-models`, `qwen-token-plan-models` | Disjoint named machine selections cover 25 and 120 cases respectively; neither is an unfiltered run. |
| Durable `tools-images` | 32 of 34 named originals in two disjoint machine receipts; the exact last two Photon timing cases passed per the later prose receipt. Worker scheduling and idle restart still lack exact Go mappings. |
| Durable `images-fit` | One machine-witnessed Photon resize case; the earlier whole-file prose receipt reports all 11. |
| AI `google-stop-reason-continuation` | One machine-witnessed SDK case; earlier prose reports eight cases jointly with `fetch-option`. No independent full-file report or second machine case is inferred. |
| AI `together-models` | Two of three named originals. The unrun `TOGETHER_API_KEY` case uses a dummy key and is offline. |
| Durable `tools` | 24 of 36 originals across three disjoint machine selections. Twelve names have no pass evidence: 11 open offline and one authorised Windows exclusion. |

## Remaining work in this slice

- **Exact offline gaps:** one Together dummy-key case and 11 Durable tool cases: three image reads, two aborted mutation waits, and six edit serialization or symlink cases. The JSONL lists all 12 names. The three mocked PowerShell cases already ran.
- **File widths still unproved:** the selected Anthropic strict schema, Codex stream, input-entry and tool-park files, and the Mistral tool-schema, model-data and retry aggregates need exact case reconciliation. The Google partial machine report is retained beside its older aggregate. Names absent from a partial report are not presumed to have failed or to be unrun if the report does not enumerate its filtered cases.
- **Specification example:** the Vitest case passed. The separate TypeScript check under the pinned Node-only options did not pass; adding the DOM library for a scratch check does not close that gate.
- **Live replay:** the user authorised the three `gpt-6.2-sol` variants (OpenAI, Codex SSE, Codex WebSocket), but the round-29 preflight found no provisioned access. No live request was made. This is open, not excluded. Synthetic cache-retention keys, SDK mocks and loopback tests establish their offline behavior only.
- **Authorised exclusion:** only the real Windows PowerShell process case at `tools.test.ts:617` is excluded within these 42 files. The exclusion does not cover the mocks or other Windows behavior. The separately approved SQLite read-only comparison does not exclude its unchanged JSONL original.
- **Other gates:** the local workerd example is supplemental and does not establish hosted Cloudflare behavior. Full local acceptance, its required measurements, hosted CI and publication are tracked separately in [acceptance gaps](../acceptance-gaps.md).

## Reproduce without network

```sh
source scripts/project-env.sh
inventory=docs/upstream-v1.6.0/test-inventory
actual=$(mktemp "$TMPDIR/primary-queue-r33.XXXXXX")
bun run "$inventory/reconcile-primary-r33.ts" "$pinned_git_mirror" > "$actual"
cmp "$inventory/primary-queue-r33.jsonl" "$actual"
rm -- "$actual"
```

The generator independently checks the changed-suite path set and pinned source blobs, resolves the prose locators to exact Git commits, and validates named machine partitions and ledger widths. The Round 21 and Round 33 receipts both reproduced byte for byte. Historical receipts remain unchanged.
