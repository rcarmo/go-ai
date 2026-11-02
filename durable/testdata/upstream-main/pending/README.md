# Known-failing source oracles

## Promoted stored-observer oracle

The complete c9 source521 oracle moved to `durable/session_deferred_observers_main_test.go` at runtime `3bb8baef0cb9af092e88775bc70f77f45d433038`. Its progression, replica/count/type assertions and real callback-receipt reads are unchanged; registration now uses the actual private synchronous stored lane. Four stored batches produce four publications, including Close, and reconstruct `abcdef`.

The earlier three-publication/four-batch legacy baseline stays historical at `4077631:durable/testdata/upstream-main/pending/session_deferred_observers_test.go`; it was not rerun. [Observer scope and checks](../../../../docs/upstream-main/synchronous-observers.md). The generation oracle below is still failing and outside default discovery.

## Generation partial batching (official v1.5.0)

`harness_deferred_progress_test.go` retains the source100-115 memory-count and stored-batch-ratio assertions from official release `f9a26365`, with a deterministic native stream/clock/watch adapter. On unchanged `4f46899`, all three backends deliver20 memory partials but store24 changed-document batches; the first partial is already durable. Production generation still uses that immediate checkpoint path after `8d8bced`; the new private runtime lane is a prerequisite. The baseline was not repeated. [Contracts, adapters and next integration](../../../../docs/upstream-v1.5.0/runtime-progress-lane.md).

Use the same project-owned overlay/prototype-exclusion setup below, mapping virtual target `durable/harness_deferred_progress_main_test.go` to `durable/testdata/upstream-main/pending/harness_deferred_progress_test.go`, and select `^TestMainHarnessDeferredProgressBatching$`. Go discovers the virtual test file; vet/Staticcheck compile it without executing its known failure. Keep both ratio assertions and the raw-storage control intact when integrating generation.

## Generation stored-touch count

`view_generation_persistence_test.go` preserves the full stored-touch-count assertion from pinned `harness-view.test.ts:128–158`, with documented native provider/input/entry-envelope adaptations. It is a pending executable source fixture, outside the default Go test discovery path. It must pass before that assertion can be marked converged. There is no skip or relaxed expected count inside the test.

Run it by overlaying `durable/view_generation_oracle_main_test.go` with this fixture, then selecting `TestMainViewUpstreamGenerationOracle`. Use the project environment and Go 1.27.3. The tested invocation currently fails on memory, journal and SQLite with four delivered frames and four stored touching commits; source requires `frames == touches + 1`.

Example from the repository root (use an unused project-owned run directory):

```sh
. scripts/project-env.sh
export GOTOOLCHAIN=go1.27.3 GOMAXPROCS=4
mkdir -p "$PROJECT_TMP_ROOT/runs/generation-pending/manual"
bun -e 'const fs=require("fs"),r=process.env.PROJECT_TMP_ROOT+"/runs/generation-pending/manual",f=fs.realpathSync("durable/testdata/upstream-main/pending/view_generation_persistence_test.go"),Replace={}; await Bun.write(r+"/excluded.go","package durable"+String.fromCharCode(10)); for(const root of new Set([process.env.PWD,fs.realpathSync(".")])){Replace[root+"/durable/view_generation_oracle_main_test.go"]=f;Replace[root+"/durable/continuation_prototype_main_test.go"]=r+"/excluded.go";} await Bun.write(r+"/overlay.json",JSON.stringify({Replace}));'
CGO_ENABLED=1 go test -overlay "$PROJECT_TMP_ROOT/runs/generation-pending/manual/overlay.json" ./durable -run '^TestMainViewUpstreamGenerationOracle$' -count=1
# The command above currently exits non-zero. Remove the overlay after analysis:
rm "$PROJECT_TMP_ROOT/runs/generation-pending/manual/overlay.json" "$PROJECT_TMP_ROOT/runs/generation-pending/manual/excluded.go"
rmdir "$PROJECT_TMP_ROOT/runs/generation-pending/manual"
```

The overlay covers the checkout's inherited logical PWD and physical path and excludes the protected prototype without touching it. The normal active test deliberately covers only the passing value/operation subset. Passing normal development tests does not close this pending oracle. See `docs/upstream-main/generation-view-oracle.md` for the storage counter, remaining implementation work and measurements.
