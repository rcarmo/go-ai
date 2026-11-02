# Validation baseline for the upstream-main delta

Native v1.4.0 is an accepted starting point. The upstream-main work extends that port; a fresh review queue does not erase its test results or native API/platform adaptations.

## Accepted scope

[Native v1.4.0 acceptance](../v140/acceptance.md) records upstream `4a080252f06caf63a525d7873c5d94d3c23887e0` and native `ee3076b3659697f1de01b87da964396e069a4389`. Final local full ordinary, shuffled and race suites passed on Go 1.27.3 with the exact official worker. Profiling, regeneration, fault, static, security and licence gates were completed. [Hosted CI 40777636922](https://github.com/rcarmo/go-ai/actions/runs/40777636922) passed on the released candidate. Those results retain their original revision and scope.

The [v1.4.0 crosswalk](../v140/crosswalk.json) maps 178 relevant changed upstream paths to 31 native production/test groups. All 642 distinct Go file pointers in those groups still exist. Reuse its recorded Go API, storage and platform mappings after checking the applicable observable contract. Historical acceptance neither erases a demonstrated difference nor requires rebuilding an equivalent implementation. Earlier durable closure records retain their narrower historical scopes.

At native `fdd7b47`, a static inventory finds 958 Go test files and 2,296 named `Test...` entrypoints, excluding the protected continuation prototype. Table cases and dynamically generated subtests are not counted individually; tests in preserved pending directories are included. This inventory establishes existing test assets, not fresh passing results. The earlier main-port receipt also records a full durable race run with 4,349 named passes and seven opt-in skips, at its recorded checkpoint.

## Current delta

The target remains `c9f4c7b9fcd0f3e6b5bbc35c7c9b6cda35580eb8`. Of 362 target-package test/fixture/support paths, 175 changed since accepted upstream v1.4.0: 38 AI and 137 durable. The other 187 are unchanged. Changed source implementations can still affect unchanged tests, so regression selection follows affected contracts and dependencies. The 210-path schema dependency queue has a separate scope and is not a percentage of unimplemented SDK code.

The new crosswalk omitted 14 explicit reviews retained at `81f3cd41`: ten development-verified partial paths and four reviewed-open paths. All 14 upstream test files are unchanged between that pin and the current target. Their exact prior native evidence is now carried into the current review source with its original pin. Partial results stay partial, open obligations stay open, and no current-tip run is inferred from unchanged test bytes.

The former 5/572 (0.9%) figure counted current inventory rows labelled fully ported. It is not a measure of implementation completeness or existing validation coverage. No trustworthy whole-port percentage follows from these inventories.

## Work selection

Use the accepted crosswalk, current native tests and later focused receipts before adding new cases. For each changed upstream contract, identify the existing native proof, any affected dependency, and the smallest missing assertion. Reuse completed source baselines. Run targeted native regressions when code changes, and add tests or production fixes for demonstrated gaps. Do not rebuild an accepted native equivalent merely because it differs mechanically from TypeScript.

New contracts still require work. For example, document/commit observer APIs were added after upstream v1.4.0; the Session source diff contains `observeDoc` and guarded observer publication additions. The accepted asynchronous watch mapping does not cover that new API. The preserved generation persistence oracle also has a demonstrated failure. Both have concrete contracts to investigate without repeating the completed schema baselines.

After carrying forward the 14 prior reviews, the next bounded investigation is the existing generation persistence mismatch and its mapped storage/publication tests. Broader delta mapping remains open. New `multipleOf` fixture work is paused; no source/native baseline or production change was made for it.

Full current-tip acceptance is not complete. The focused-only restriction remains: no full-suite rerun, profiling, hosted dispatch, release or publication was performed for this reconciliation. The protected prototype and native v1.4.0 refs/assets remain unchanged.
