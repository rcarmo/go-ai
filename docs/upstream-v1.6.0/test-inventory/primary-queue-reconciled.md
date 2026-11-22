# Changed primary suite queue: receipt reconciliation

The [queue reconciliation](primary-queue-reconciled.jsonl) joins the 21 AI and 16 Durable files that had no assertion-ledger rows to focused receipts known at native commit `d1bd6c9fc67967345df7755c2606b2a823e3a31f`. Every input file is bound to its exact Git blob at official pi v1.6.0, `faac585884ae2c013a6b1400f3e9e77f10b34bac`.

| Queue status | Files |
| --- | ---: |
| Prior original receipt located | 15 |
| New unchanged original receipt | 1 |
| No original receipt found in the audited records | 21 |

Fifteen files have documented native work. The records link that evidence separately; neither a focused Go packet nor a suite receipt creates per-assertion mappings. Multiple named files may share a single original count. For example, the 73-case receipt names both `error-body.test.ts` and `retry.test.ts`; this packet does not assign 73 to `retry.test.ts`. A missing link means missing in this audit, not proof no such test has ever run.

Three known conditional areas remain **withheld, not excluded**: live cache retention, OpenAI/Codex replay E2E, and POSIX/Windows process branches in `tools.test.ts`. Dummy-key and SDK-mock tests are distinct from live credentialed cases. Other lexical flags remain review leads, not an exhaustive inventory of gates.

## New original: directory enumeration race

[`env-node-list-race.json`](env-node-list-race.json) records **one pass, zero failures, zero skips** for the whole unchanged file on Node `v26.13.0` and Vitest `4.1.12`. `NodeExecutionEnv.listDir` observes the file after enumeration but before `lstat`; the fixture renames it in that gap. The exact result is `['doc-1.jsonl']`, and the callback must have been consumed.

The next native translation can target [`Env.ListDir`](../../../durable/tools/filesystem.go). Its loop currently returns the first `FileInfo` error. That inspection suggests a place to reproduce the race; it is not a compiled native red or mapping. A future probe should distinguish a vanished enumerated entry from other errors and check the exact retained entry list. Other OS behavior is unverified.

## Reproduce without network or credentials

```sh
inventory=docs/upstream-v1.6.0/test-inventory
bash "$inventory/probe-env-node-list-race.sh" "$pinned_git_mirror" "$retained_oracle" \
  > /tmp/pi-v160-env-node-list-race.json
cmp "$inventory/env-node-list-race.json" /tmp/pi-v160-env-node-list-race.json
flock /tmp/go-ai/v160-focused-verify.lock bun run "$inventory/reconcile-primary.ts" "$pinned_git_mirror" \
  > /tmp/pi-v160-primary-queue.jsonl
cmp "$inventory/primary-queue-reconciled.jsonl" /tmp/pi-v160-primary-queue.jsonl
```

The run stages exact Git source and only the retained Vitest dependency closure, uses the unchanged Durable runner config, checks source bytes before and after execution, and validates the exact resulting case list. It holds the shared verification lock and runs with a scrubbed environment and no Node network permission. No suite outside the one named file, credentialed call, native test or release gate was run.
