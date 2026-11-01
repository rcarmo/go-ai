## Current tip: c9f4c7b9

The final checkpoint remote check found `c9f4c7b9fcd0f3e6b5bbc35c7c9b6cda35580eb8`. Rui's Git-tip convergence goal applies to this exact pin. The two commits after `b556cfe5` change only three Chord design documents: `docs/next/{decisions,design,slices}.md`. They describe source-aligned cross-plugin contracts (relative imports or copied contracts for code changing together, versioned npm contracts and provider `accepts` for independently changing plugins); the documents explicitly leave implementation work for a later plugin slice.

[Current inventory](tip-c9f4c7b9.json), [362-path target crosswalk](upstream-tests-c9f4c7b9.tsv), [210-path schema dependency-test queue](dependency-tests-c9f4c7b9.tsv) and [current review entries](upstream-review-c9f4c7b9.json) supersede the baseline pointers below. There are zero incremental target-package paths, three incremental support paths and 427 target-package paths relative to accepted upstream v1.4.0. No test paths were added, removed or changed. The full AI/durable/codemode/schema trees are unchanged from `b556cfe5`, verified with `git diff --exit-code`; the focused `f2b8997` validation evidence retains its tested code and original pin. No tests or profiling were rerun for this documentation-only advance. Native release refs/assets are unchanged.

Regenerate using the command below with `tip-b556cfe5.json` as the previous inventory and full target `c9f4c7b9fcd0f3e6b5bbc35c7c9b6cda35580eb8`. Preserve both earlier inventory sets and scoped receipts. Runtime semantic convergence, dependency applicability review and acceptance are still incomplete.

## Upstream tip advanced on 1 November 2026

Convergence target at checkpoint `f2b8997`: `b556cfe5b0300a91f39c7bc5369a65a25fb7fcd5`, explicitly authorised by Rui's 23:10 UTC goal update to converge with upstream main Git tip using upstream tests as porting oracles. A fresh remote check still returned this SHA. The earlier check below records the decision before this authorisation. Previous-pin inventories and test receipts retain their original scope; native v1.4.0 stays at `ee3076b3659697f1de01b87da964396e069a4389`.

[The current inventory](tip-b556cfe5.json) records 88 incremental target-package paths (19 AI, 67 durable, two codemode), 427 target-package paths relative to accepted upstream v1.4.0, and 543 incremental dependency/support paths. The [current test crosswalk](upstream-tests-b556cfe5.tsv) contains 362 paths: 193 AI, 161 durable and eight codemode. Exactly two are newly present and none removed; 60 paths changed since the previous pin. Current dispositions began at `pending-review`, with prior disposition/evidence preserved separately and scoped to `81f3cd41`. Current review entries live in `upstream-review-b556cfe5.json` and are applied by the generator; the first validation-probe row is explicitly partial. Unchanged test files still need reconciliation with changed dependencies.

The new schema dependency has a separate [210-path test/fixture review queue](dependency-tests-b556cfe5.tsv). The support inventory includes every changed schema path; the schema package manifest is included in the JSON. Schema, Chord and repository-support changes need applicability review alongside the three target packages.

Regenerate from exact Git objects, without fetching or checking out files:

```sh
. scripts/project-env.sh
bun scripts/inventory-upstream-main.ts \
  "$PROJECT_TMP_ROOT/cache/upstream-pi/pi-repo" \
  docs/upstream-main/tip-81f3cd41.json \
  b556cfe5b0300a91f39c7bc5369a65a25fb7fcd5 docs/upstream-main
```

The generator records complete no-renames package/support diffs, both manifests, every target test path and the schema test queue. It merges the checked-in current review JSON, validating its exact target and path membership; regenerate tables after updating that review file. Its counts and path lists are checked independently against Git. This pin advance does not establish runtime parity or acceptance. The first translated new-tip probes and bounded fix are recorded in [schema-validation-oracles.md](schema-validation-oracles.md).

## Upstream tip checked on 1 November 2026

Remote `refs/heads/main` is `b556cfe5b0300a91f39c7bc5369a65a25fb7fcd5` (commit time `2026-11-01T15:20:46+00:00`, `docs(chord): schemas are pi-schema's in the design`). Two remote checks returned this SHA. This is an observation; the authorised convergence pin and its inventories stay at `81f3cd412758ebdd92324c7d63b669a189408d0a` until Rui requests an advance.

The exact pin-to-tip range contains 17 commits and 631 changed paths with rename detection disabled: 19 AI, 67 durable, two codemode, 259 schema, 230 Chord, ten agent, 32 coding-agent, two MCP, five TUI and five root/script paths. These are incremental paths, not the accepted-release inventory or a completed semantic review.

### Impact inspected

- New `@earendil-works/pi-schema` supplies JSON Schema validation. AI adds it as a runtime dependency and moves TypeBox to development dependencies; durable replaces its TypeBox runtime dependency. The new schema package requires review as a transitive contract even though it is outside the three target packages.
- AI validation now caches pi-schema compilers by schema identity, parses cloned arguments with coercion and rejects unresolved references at their location. Durable tool checks use the non-coercing compiler, registered formats and JSON Pointer error paths. Exported schema types/builders and error wording change. The upstream changelog lists compatibility differences for refinements, codecs, registered formats, non-JSON types and closed object builders; native equivalence is unverified.
- Added target tests are `packages/ai/test/validation-probes.test.ts` and `packages/durable/test/tool-args-typebox.test.ts`. Inspected probes cover large enums, cached validators after schema mutation, optional-null removal, branch/sibling coercion and recursive-union rejection. The durable test checks inferred tool arguments. Existing modified tests also need review if the pin advances.
- All ten changed durable production paths concern codemode schemas, tool definitions/types, tool search or built-in tools. Codemode's two changed paths are README and CHANGELOG; its runtime and manifest are unchanged.
- `packages/durable/test/harness-view.test.ts` and `packages/durable/src/harness/{generation,live,view}.ts` exist at the observed tip and are unchanged from the pin. The generation deferred-persistence oracle and its native four-frames/four-stored-touches failure therefore still apply. The newer tip supplies no fix for that gap.
- Chord HTTP/worker additions, routed-host admission and schema migration require dependency screening before a new baseline is adopted. The 631-path count does not make every unrelated feature part of the native port.

### Verification and next action

Read exact Git objects for commit distance, path counts, manifests, validation changes, new test assertions and unchanged generation/view sources; rechecked the remote SHA. No checkout, reset, native runtime change, test execution, dependency installation, release-ref change or publication occurred. No source extracts or disposable run artifacts were created. The cached upstream repository is the existing reference mirror.

If Rui advances the pin, regenerate complete changed-path/test/dependency inventories against that exact SHA, preserve earlier receipts with their original scope, and port schema-validation assertions before changing native production. Until then, continue the demonstrated deferred-publication/durability gap against `81f3cd41`.

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
