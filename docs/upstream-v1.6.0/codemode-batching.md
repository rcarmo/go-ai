# Codemode journal batching

The pinned case at `packages/durable/test/codemode.test.ts:913–946` returns `50` and requires fewer than ten journal publications. The [native published-worker comparison](../../durable/codemode_worker_concurrent_results_v160_test.go) checks the exact value and the publication limit. The older policy used 103 publications, then 54 after the single-result fold. That policy still applies to its saved runs.

## New and saved runs

New runs on the built-in positioned worker pin a grouped journal in the saved start. Generic Go remotes stay on the earlier policy. Reopening a saved run uses its recorded policy, not the current default.

The host can commit up to 16 ready outcomes in a group. It doesn't wait to fill the group. A preceding non-final drain can commit with the group, and each outcome must already be covered by durable call admission. The host can acknowledge a drain at existing coverage without another write; newly issued effects still need a committed count. If the combined candidate hits an existing limit before staging, the host keeps the original count or single-outcome path. It never retries an uncertain commit or increases a record limit.

The journal records the group boundaries and keeps outcomes in order. An early script return seals its actual delivered prefix alongside the result. It does not erase the committed suffix. A typed worker stop can preserve the failed result without claiming a guest delivery position. Raw put and delta writes cannot change a sealed journal.

## Recovery checks

The native proofs use the actual published guest and writable SQLite. They cover exit before the first grouped delivery, between deliveries, and after the last result before terminal settlement. Reopen returns the saved values without repeating callbacks. The early-return case keeps an unused committed outcome; the new-call check reads the committed count inside the effect. The scoped record-limit test checks both an accepted combined write and rejection after an individually committed prefix, under different configured limits.

Other replay differences remain open. See [acceptance gaps](acceptance-gaps.md) for saved-version refusal, delayed callback joins and the other unclosed cases.
