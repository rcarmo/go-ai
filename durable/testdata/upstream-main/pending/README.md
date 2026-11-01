# Known-failing source oracles

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
