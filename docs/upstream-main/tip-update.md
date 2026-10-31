## Upstream tip advanced on 31 October 2026

Current convergence target: `81f3cd412758ebdd92324c7d63b669a189408d0a` (upstream main, commit time `2026-10-31T20:06:14+00:00`). Rui explicitly requested this update. The previous target `4b535d57ea8863dbba44b89872146523da271786` and its test receipts remain historical checkpoints; native v1.4.0 stays unchanged at `ee3076b3659697f1de01b87da964396e069a4389`.

The new tip is 57 commits beyond the previous pin. [The new inventory](tip-81f3cd41.json) records 87 incremental package paths: one AI manifest, 85 durable paths and one codemode manifest. Relative to accepted upstream v1.4.0 (`4a080252f06caf63a525d7873c5d94d3c23887e0`), it records 402 package path entries. This inventory uses `--no-renames`, representing renames as deleted/added paths; the original 354-entry inventory keeps its original method and pin.

The [current target test corpus](upstream-tests-81f3cd41.tsv) has 360 paths: 192 AI, 160 durable and eight codemode. Its scope matches the previous crosswalk: all `test/` paths plus worker `.test.ts` cases; worker configuration/support files remain in the package inventories. Each row preserves any previous-pin disposition and native references in separate columns; current-target review starts pending. Eight paths are newly present; no previous corpus path was removed. The older `upstream-tests.tsv`, `changes.json` and `dependencies.json` continue to describe the previous pin and must not be treated as current-target inventories.

## Added review work

Durable changes include cached storage, conversation tails, group commit, writer/keys, context ranges, idle scopes, search and storage commit behaviour. The eight newly present tests are harness-context-range, harness-group-commit, harness-idle-scopes, search, session-cached-storage, session-conversation-tails, session-group-commit and store-storage-commit. Existing changed tests also require per-contract review.

The inventory includes all 279 incremental repository paths outside the three target packages for conservative dependency/support screening, including 205 Chord paths and 60 coding-agent paths. This list is an applicability review queue, not a claim that all unrelated features require native ports. Package manifests at both pins are recorded. AI/codemode packaging replaces shx commands with `scripts/package-files.mjs`; build/dependency review must include that helper and its tests, root lockfile and entry-graph checks.

Google runtime files and the pending raw-stop test are unchanged between the pins. The completed Google thinking-map receipt keeps its original pin; no tests have been rerun or relabelled as full new-tip acceptance. Resume tests-first work against exact new-tip objects and review dependency impacts before promoting inherited dispositions.

## Validation

The upstream mirror was fetched without resetting its index or checking out files. The remote main SHA, commit distance, per-package paths, manifests and complete current test tree were read from exact Git objects. JSON counts and TSV paths are checked against those objects. No native production code, release ref, hosted workflow or publication changed. Full convergence, staticcheck closure and acceptance/profiling are incomplete.
