# Round 21: changed primary suite evidence

This [immutable snapshot](primary-queue-r21.jsonl) covers all **42 primary suites** added or changed in pi v1.6.0: 22 AI and 20 Durable paths, including two Durable-hosted Codemode files. It is tied to pinned upstream `faac585884ae2c013a6b1400f3e9e77f10b34bac` and native evidence through `d0f5e86bf5392e4a8027904f888c6e244c2e0284`. It preserves the [Round 12 reconciliation](primary-queue-reconciled.md). The 37 files in that earlier queue now have original-run evidence for 24 files; for 13 files, no original-run evidence was located in this audit, down from 21 in Round 12. This does not count assertions or imply parity.

No original or native tests were executed in Round 21. The script compares exact Git paths, blobs, byte hashes, report partitions, source conditions, and committed evidence. A machine receipt binds exact source bytes to named case outcomes; a prose claim or a ledger reference links to a committed account of execution, not a retained test report. Where several reports exist, the table lists the strongest file-level evidence and the snapshot retains the rest.

| Strongest original evidence | All 42 | Historical 37 |
| --- | ---: | ---: |
| Whole-file machine receipt | 2 | 2 |
| Complete, disjoint machine selections | 2 | 2 |
| Whole-file prose claim | 13 | 12 |
| Whole-files prose aggregate | 2 | 0 |
| Selected machine receipt only | 2 | 2 |
| Selected prose or ledger claim | 5 | 3 |
| Named prose aggregate only | 3 | 3 |
| No original-run evidence found | 13 | 13 |

The whole-file machine receipts are `fetch-option` and `env-node-list-race`. The Fireworks (25) and Qwen (120) machine receipts partition their files exactly; neither was one unfiltered run. The selected Google `CONTINUATION` and Together reports prove one and two cases respectively, not the rest of those suites. The two Codemode files share a documented 49-case whole-files aggregate; there is no per-file count here. Other aggregates and overlapping reports are not added together.

The current assertion ledgers index **14 files** (nine in the historical queue). Another **15** have documented native comparisons but no assertion index. **Four** only have Go source leads: Completions tool choice, Harness after-run, Harness submissions, and Harness tools. The other **nine** have no current file-specific native link in this snapshot. A lead is not a run or a correspondence; an indexed file can still have partial, candidate, unmatched, or missing assertions. All 29 files with an original receipt or claim have some native evidence, but this file-level overlap does not map all selected originals.

## Next work by owner

| Owner | Source evidence missing in this audit | Suggested next step |
| --- | --- | --- |
| AI | `constrained-sampling`, `llama-cpp-classify`, `openai-completions-tool-choice`, `openai-reasoning-replay-e2e`, `providers` | Start with offline classifier and tool-choice cases; also close Together's filtered dummy-key case. Keep live replay gated. |
| Durable | `harness-after-run`, `harness-nested-tools`, `harness-submissions`, `harness-tool-hooks`, `harness-tools`, `spec-usage`, `tools-images`, `tools` | Start with offline usage and image cases, then validate the historical Harness Go leads against v1.6.0. Split the tools suite into portable and host-specific cases before replay. |
| Codemode with Durable | None in this 42-file slice | Retrieve per-file machine results for the two Durable-hosted Codemode suites and reconcile unindexed assertions. |

The initial cache-retention credential hold has changed: the later whole-file claim reports four guards exercised with synthetic keys and payload hooks that stopped before transport. That is not live retention evidence. OpenAI/Codex replay still needs authorized credentials. The POSIX and Windows process branches in `tools.test.ts` need separate host receipts. They are open, not excluded. Lexical credential hints for mocked SDKs and dummy-key tests do not make those tests live.

A missing link only describes this audit of committed evidence. For example, the crosswalk mentions a combined Codemode/Harness tool selection without exact file names; this snapshot does not allocate the result to either changed Harness tool file. Native candidate comments were searched at the pinned evidence base and are not upgraded to v1.6.0 mappings.

## Reproduce without network

```sh
inventory=docs/upstream-v1.6.0/test-inventory
bun run "$inventory/reconcile-primary-r21.ts" "$pinned_git_mirror" \
  > /tmp/pi-v160-primary-queue-r21.jsonl
cmp "$inventory/primary-queue-r21.jsonl" /tmp/pi-v160-primary-queue-r21.jsonl
```

The generator reads the historical inventory, crosswalk, ledgers, reports and native comment candidates at exact Git commits. It independently checks the A/M path set against the upstream Git diff. The script does not fetch anything or interpret a missing artifact as a failed test.
