# Known-failing source oracles

## Deferred stored-observer publication

`session_deferred_observers_test.go` retains the complete c9f4c7b9 `session-deferred-commits.test.ts:521-562` progression: held `a`/`b`, immediate `c`, held `d` then timer, held `e` plus a dependent entry, and held `f` then Close. It reconstructs Storage from actual admitted backend writes and compares the publication-only replica at every delivered prefix, final `abcdef`, publication count and the four literal change-type arrays. No expected count is relaxed.

The legacy native API adapter uses real `SubscribeCommits` callbacks and waits for the first three asynchronous deliveries. The corrected baseline at `bd2123f` fails all three CGO backends (0.079s) and both non-CGO backends (0.053s): **three publications for four stored batches**, missing the final `["document"]` publication. Storage itself contains `abcdef`. A compile-time result-type mistake and an incorrect deferred-zero-to-immediate adapter caused earlier harness failures; neither receives runtime-failure credit. Immediate mode uses the ordinary native commit path; a zero deferred deadline is distinct.

Run only this pending oracle with a virtual test-file overlay (Go 1.27.3 supports added files). The protected continuation prototype is separately overlaid out:

```sh
. scripts/project-env.sh
export GOTOOLCHAIN=go1.27.3 GOMAXPROCS=4
mkdir -p "$PROJECT_TMP_ROOT/runs/session-deferred-observers/manual"
bun -e 'const {realpathSync}=require("fs"); const r=process.env.PROJECT_TMP_ROOT+"/runs/session-deferred-observers/manual", Replace={}; await Bun.write(r+"/excluded.go","package durable\\n"); const fixture=realpathSync("durable/testdata/upstream-main/pending/session_deferred_observers_test.go"); for(const root of new Set([process.env.PWD,realpathSync(".")])){Replace[root+"/durable/session_deferred_observers_oracle_main_test.go"]=fixture;Replace[root+"/durable/continuation_prototype_main_test.go"]=r+"/excluded.go";} await Bun.write(r+"/overlay.json",JSON.stringify({Replace}));'
CGO_ENABLED=1 go test -overlay "$PROJECT_TMP_ROOT/runs/session-deferred-observers/manual/overlay.json" ./durable -run '^TestMainSessionDeferredStoredObserverOracle$' -count=1
# Expected non-zero. Dispose this invocation's overlay files after analysis:
rm "$PROJECT_TMP_ROOT/runs/session-deferred-observers/manual/overlay.json" "$PROJECT_TMP_ROOT/runs/session-deferred-observers/manual/excluded.go"
rmdir "$PROJECT_TMP_ROOT/runs/session-deferred-observers/manual"
```

The fixture stays outside default discovery until a source-aligned observer boundary passes it. Existing asynchronous callbacks may reenter Session APIs; upstream synchronous observers explicitly must not block or call the Session. Replacing the native callback contract or manufacturing a Close publication in this adapter would invalidate the oracle. See [observer contracts](../../../../docs/upstream-main/synchronous-observers.md#stored-observer-counterexample-at-c9f4c7b9).

## Generation stored-touch count

`view_generation_persistence_test.go` preserves the full stored-touch-count assertion from pinned `harness-view.test.ts:128–158`, with documented native provider/input/entry-envelope adaptations. It is a pending executable source fixture, outside the default Go test discovery path. It must pass before that assertion can be marked converged. There is no skip or relaxed expected count inside the test.

Run it by overlaying `durable/view_generation_oracle_main_test.go` with this fixture, then selecting `TestMainViewUpstreamGenerationOracle`. Use the project environment and Go 1.27.3. The tested invocation currently fails on memory, journal and SQLite with four delivered frames and four stored touching commits; source requires `frames == touches + 1`.

Example from the repository root (use an unused project-owned run directory):

```sh
. scripts/project-env.sh
export GOTOOLCHAIN=go1.27.3 GOMAXPROCS=4
mkdir -p "$PROJECT_TMP_ROOT/runs/generation-pending/manual"
bun -e 'const fs=require("fs"); const p=process.env.PWD+"/durable/view_generation_oracle_main_test.go"; const f=fs.realpathSync("durable/testdata/upstream-main/pending/view_generation_persistence_test.go"); await Bun.write(process.env.PROJECT_TMP_ROOT+"/runs/generation-pending/manual/overlay.json",JSON.stringify({Replace:{[p]:f,[fs.realpathSync(p)]:f}}));'
CGO_ENABLED=1 go test -overlay "$PROJECT_TMP_ROOT/runs/generation-pending/manual/overlay.json" ./durable -run '^TestMainViewUpstreamGenerationOracle$' -count=1
# The command above currently exits non-zero. Remove the overlay after analysis:
rm "$PROJECT_TMP_ROOT/runs/generation-pending/manual/overlay.json"
rmdir "$PROJECT_TMP_ROOT/runs/generation-pending/manual"
```

The two overlay keys cover the checkout's inherited logical PWD and its physical path. The normal active test deliberately covers only the passing value/operation subset. Passing normal development tests does not close this pending oracle. See `docs/upstream-main/generation-view-oracle.md` for the storage counter, remaining implementation work and measurements.
