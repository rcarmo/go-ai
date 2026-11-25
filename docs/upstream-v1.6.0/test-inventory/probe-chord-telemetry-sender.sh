#!/usr/bin/env bash
set -euo pipefail

if [[ $# != 3 ]]; then
  printf 'Usage: bash %s <pinned-git-mirror> <prepared-oracle-dir> <user-npm-cacache>\n' "$0" >&2
  exit 1
fi
mirror=$(realpath -- "$1")
oracle=$(realpath -- "$2")
cache=$(realpath -- "$3")
here=$(cd -- "$(dirname -- "$0")" && pwd -P)
cd "$(git -C "$here" rev-parse --show-toplevel)"
# shellcheck source=/dev/null
source scripts/project-env.sh

pin=faac585884ae2c013a6b1400f3e9e77f10b34bac
base=681ae6ce8132e75f46c855f791d7ff91ca0f616c
test=packages/chord/test/telemetry-ingest/sender.test.ts
config=packages/chord/vitest.config.ts
sender=packages/chord/src/telemetry-ingest/sender.ts
deps="$here/../dependencies"
scratch=$(mktemp -d "$TMPDIR/chord-telemetry-sender.XXXXXX")
trap 'rm -r -- "$scratch"' EXIT
mkdir -p "$scratch/home" "$scratch/tmp"
sha256() { sha256sum -- "$1" | cut -d' ' -f1; }

git -C "$here" show "$base:docs/upstream-v1.6.0/test-inventory/files.jsonl" > "$scratch/inventory.jsonl"
blob=$(git -C "$mirror" rev-parse "$pin:$test")
jq -e --arg test "$test" --arg blob "$blob" '
  select(.kind=="path" and .path==$test) | .scope=="shared-dependency" and .role=="suite" and .change=="M" and .pinned.object==$blob
' "$scratch/inventory.jsonl" > "$scratch/inventory.check"
git -C "$mirror" show "$pin:package-lock.json" > "$scratch/lock.json"
git -C "$here" show "$base:docs/upstream-v1.6.0/dependencies/npm-closure.jsonl" > "$scratch/closure.jsonl"
lock_sha=$(sha256 "$scratch/lock.json")
jq -e --arg pin "$pin" --arg sha "$lock_sha" 'select(.kind=="manifest") | .pin==$pin and .inputs["package-lock.json"]==$sha' \
  "$scratch/closure.jsonl" > "$scratch/lock.check"
git -C "$mirror" archive "$pin" package.json packages/{chord,schema,telemetry}/{src,package.json} "$test" "$config" \
  packages/chord/test/support.ts | tar -x -C "$scratch"
for source in packages/ai/src/telemetry.ts packages/durable/src/harness/telemetry.ts; do
  git -C "$mirror" show "$pin:$source" > "$scratch/ref.ts"
  line=$(grep -nF 'from "@earendil-works/pi-telemetry";' "$scratch/ref.ts" | cut -d: -f1)
  jq -nc --arg file "$source" --arg sha "$(sha256 "$scratch/ref.ts")" --argjson line "$line" \
    '{file:$file,sourceSha256:$sha,line:$line,import:"@earendil-works/pi-telemetry"}'
done > "$scratch/refs.jsonl"
libc=$(node -p "process.report.getReport().header.glibcVersionRuntime ? 'glibc' : 'musl'")
bun run "$deps/copy-openai-probe-deps.ts" "$scratch/closure.jsonl" "$lock_sha" "$oracle" "$scratch" "$libc" \
  node_modules/vitest > "$scratch/copied.json"
mkdir -p "$scratch/node_modules/@earendil-works"
for p in pi-schema pi-telemetry; do ln -s "../../packages/${p#pi-}" "$scratch/node_modules/@earendil-works/$p"; done

# Compare the retained launcher, CLI and evaluator with the lock-verified archive.
key=node_modules/vitest
read -r vitest integrity < <(jq -er --arg key "$key" '.packages[$key] | [.version,.integrity] | @tsv' "$scratch/lock.json")
[[ $vitest == 4.1.12 && $integrity == sha512-* ]]
hex=$(printf '%s' "${integrity#sha512-}" | base64 -d | od -An -v -tx1 | tr -d ' \n')
[[ $hex =~ ^[0-9a-f]{128}$ ]]
archive="$cache/content-v2/sha512/${hex:0:2}/${hex:2:2}/${hex:4}"
[[ sha512-$(openssl dgst -sha512 -binary "$archive" | openssl base64 -A) == "$integrity" ]]
for member in vitest.mjs dist/cli.js dist/module-evaluator.js; do
  tar -xOzf "$archive" -- "package/$member" | cmp - "$scratch/$key/$member"
done
cp -- "$here/chord-telemetry-sender-witness.mjs" "$scratch/witness.mjs"

run() {
  local tag=$1
  shift
  (
    cd "$scratch/packages/chord"
    env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" CHORD_SENDER_PROBE_ROOT="$scratch" \
      CHORD_SENDER_PROBE_TRACE="$scratch/$tag.trace.jsonl" \
      NODE_OPTIONS="--max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons --import=$scratch/witness.mjs" \
      unshare -Urn timeout 90s node ../../node_modules/vitest/vitest.mjs run test/telemetry-ingest/sender.test.ts --pool=forks --maxWorkers=1 \
        --reporter=json --outputFile "$scratch/$tag.result.json" "$@"
  ) > "$scratch/$tag.log" 2>&1
}
failures() {
  local tag=$1
  cat "$scratch/$tag.log" >&2
  if [[ -s $scratch/$tag.result.json ]]; then
    jq '[.testResults[].assertionResults[]|select(.status=="failed")|{fullName,failureMessages}]' "$scratch/$tag.result.json" >&2
  fi
}
exec 9>/tmp/go-ai/v160-focused-verify.lock
flock 9
run original || { failures original; exit 1; }
jq -e '.numPassedTests==3 and .numFailedTests==0 and .numPendingTests==0 and .numTodoTests==0 and
  (.testResults|length)==1 and ([.testResults[].assertionResults[]|select(.status=="passed")|.fullName]|unique|length)==3' \
  "$scratch/original.result.json" > "$scratch/result.check" || { failures original; exit 1; }
jq '[.testResults[].assertionResults[]|.fullName]' "$scratch/original.result.json" > "$scratch/names.json"

selector="a worker's sender sends spans and metrics to the worker variant"
cp -- "$scratch/$sender" "$scratch/original-sender.ts"
[[ $(grep -Fc 'metrics: batch.metrics.slice(0, WORKER_BATCH_ROWS)' "$scratch/$sender") == 1 ]]
sed -i 's/metrics: batch.metrics.slice(0, WORKER_BATCH_ROWS)/metrics: []/' "$scratch/$sender"
if run control -t "$selector"; then
  echo 'The dropped worker metric unexpectedly passed' >&2
  exit 1
else
  [[ $? == 1 ]] || { failures control; exit 1; }
fi
mv -- "$scratch/original-sender.ts" "$scratch/$sender"
flock -u 9
jq -e --arg name "$selector" '.numFailedTests==1 and .numPassedTests==0 and .numPendingTests==2 and .numTodoTests==0 and
  ([.testResults[].assertionResults[]|select(.status=="failed")|.fullName]==[$name]) and
  any(.testResults[].assertionResults[]|select(.status=="failed")|.failureMessages[]; contains("expected undefined to be 2"))' \
  "$scratch/control.result.json" > "$scratch/control.check" || { failures control; exit 1; }

verify_trace() {
  local tag=$1 batches=$2
  jq -es --argjson batches "$batches" '
    ([.[] | select(.kind=="source-evaluated")|[.file,.export]] | sort)==([
      ["packages/chord/src/telemetry-ingest/facet.ts","telemetryIngestFacet"],
      ["packages/chord/src/telemetry-ingest/recorder.ts","TelemetryRecorder"],
      ["packages/chord/src/telemetry-ingest/sender.ts","sendWorkerTelemetry"],
      ["packages/telemetry/src/memory.ts","telemetryNow"]]|sort) and
    ([.[] | select(.kind=="ingest") | del(.kind)] == $batches)
  ' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.trace.check" || { cat "$scratch/$tag.trace.jsonl" >&2; return 1; }
  jq -s '{modules:([.[]|select(.kind=="source-evaluated")]|sort_by(.file)),ingests:[.[]|select(.kind=="ingest")|del(.kind)]}' \
    "$scratch/$tag.trace.jsonl" > "$scratch/$tag.observed.json"
}
verify_trace original '[{"spanNames":["page.click","chord.hold"],"hasMetrics":false,"fixtureMetrics":[],"accepted":2},
  {"spanNames":[],"hasMetrics":false,"fixtureMetrics":[],"accepted":0},
  {"spanNames":["chord.hold","worker.job"],"hasMetrics":true,"fixtureMetrics":[{"name":"runs","sum":2}],"accepted":2}]'
verify_trace control '[{"spanNames":["chord.hold","worker.job"],"hasMetrics":false,"fixtureMetrics":[],"accepted":2}]'
while IFS= read -r rec; do
  file=$(jq -er '.file' <<< "$rec")
  jq -c --arg sha "$(sha256 "$scratch/$file")" '. + {sourceSha256:$sha}' <<< "$rec"
done < <(jq -c '.modules[]' "$scratch/original.observed.json") > "$scratch/runtime-modules.jsonl"

jq -n --arg pin "$pin" --arg base "$base" --arg test "$test" --arg blob "$blob" --arg testSha "$(sha256 "$scratch/$test")" \
  --arg configSha "$(sha256 "$scratch/$config")" --arg node "$(node --version)" --arg control "$selector" \
  --arg platform "$(node -p 'process.platform + "/" + process.arch')" --arg vitest "$vitest" \
  --arg integrity "$integrity" --arg runnerSha "$(sha256 "$archive")" --arg evaluatorSha "$(sha256 "$scratch/$key/dist/module-evaluator.js")" \
  --slurpfile names "$scratch/names.json" --slurpfile copied "$scratch/copied.json" --slurpfile refs "$scratch/refs.jsonl" \
  --slurpfile modules "$scratch/runtime-modules.jsonl" --slurpfile observed "$scratch/original.observed.json" \
  --slurpfile mutated "$scratch/control.observed.json" '
  {format:1,pin:$pin,evidenceBase:$base,node:$node,platform:$platform,configSha256:$configSha,
   original:{file:$test,blob:$blob,sha256:$testSha,selection:"whole-file",passed:3,failed:0,skipped:0,todo:0,caseNames:$names[0]},
   runner:{vitest:$vitest,integrity:$integrity,archiveSha256:$runnerSha,evaluatorSha256:$evaluatorSha,copiedDependencies:$copied[0]},
   primaryReferences:$refs,observed:{modules:$modules,ingests:$observed[0].ingests},
   control:{change:"drop-worker-metrics-in-scratch",case:$control,failed:1,unselected:2,ingests:$mutated[0].ingests}}'
