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
base=57afb7e680ed227a450aee1f554eada8b52b135b
test=packages/chord/test/host/object.test.ts
config=packages/chord/vitest.config.ts
json_copy=packages/chord/src/json.ts
tracker=packages/chord/src/delta/tracker.ts
reference=packages/durable/src/harness/submissions.ts
deps="$here/../dependencies"
scratch=$(mktemp -d "$TMPDIR/chord-json-retry.XXXXXX")
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
  packages/chord/test/support.ts "$reference" | tar -x -C "$scratch"
import_line=$(grep -nE '^import .*copyJson.*from "@earendil-works/chord/json";' "$scratch/$reference" | cut -d: -f1)
call_line=$(grep -nF 'const value = copyJson(draft.type === "write" ? draft.entry : draft.content, {' "$scratch/$reference" | cut -d: -f1)
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
cp -- "$here/chord-json-retry-witness.mjs" "$scratch/witness.mjs"

run() {
  local tag=$1
  shift
  (
    cd "$scratch/packages/chord"
    env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" CHORD_COPY_PROBE_ROOT="$scratch" \
      CHORD_COPY_PROBE_TRACE="$scratch/$tag.trace.jsonl" CHORD_COPY_PROBE_PHASE="$tag" \
      NODE_OPTIONS="--max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons --import=$scratch/witness.mjs" \
      unshare -Urn timeout 90s node ../../node_modules/vitest/vitest.mjs run test/host/object.test.ts --pool=forks --maxWorkers=1 \
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
selector='a live object failing .* is recovered by ref[.]retry[(][)]: a new attempt, the documents as they were'
name='a live object failing (slice 6a §1.4) is recovered by ref.retry(): a new attempt, the documents as they were'
run original -t "$selector" || { failures original; exit 1; }
jq -e --arg name "$name" '.numFailedTests==0 and .numPassedTests==1 and .numPendingTests>0 and .numTodoTests==0 and
  ([.testResults[].assertionResults[]|select(.status=="passed")|.fullName]==[$name])' \
  "$scratch/original.result.json" > "$scratch/original.check" || { failures original; exit 1; }
unselected=$(jq -er '.numPendingTests' "$scratch/original.result.json")

# Change only the scratch copy of the inserted string. Preserve its type.
cp -- "$scratch/$json_copy" "$scratch/original-json.ts"
[[ $(grep -Fc 'return copy(value, undefined, options?.omitUndefinedProperties === true);' "$scratch/$json_copy") == 1 ]]
sed -i '/return copy(value, undefined, options?.omitUndefinedProperties === true);/i\    if (value === "a") return "b";' "$scratch/$json_copy"
if run control -t "$selector"; then
  echo 'The wrong copied string unexpectedly passed' >&2
  exit 1
else
  [[ $? == 1 ]] || { failures control; exit 1; }
fi
mv -- "$scratch/original-json.ts" "$scratch/$json_copy"
flock -u 9
jq -e --arg name "$name" --argjson unselected "$unselected" '.numFailedTests==1 and .numPassedTests==0 and .numPendingTests==$unselected and .numTodoTests==0 and
  ([.testResults[].assertionResults[]|select(.status=="failed")|.fullName]==[$name]) and
  any(.testResults[].assertionResults[]|select(.status=="failed")|.failureMessages[]; contains("object.test.ts:1230:"))' \
  "$scratch/control.result.json" > "$scratch/control.check" || { failures control; exit 1; }

verify_trace() {
  local tag=$1 value=$2
  jq -es --arg copy "$json_copy" --arg tracker "$tracker" --arg value "$value" '
    ([.[] | select(.kind=="source-evaluated")|[.file,.export]] | sort)==([[$copy,"copyJson"],[$tracker,"track"]]|sort) and
    ([.[]|select(.kind=="json-copy")]==[{kind:"json-copy",input:"a",output:$value}])
  ' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.trace.check" || { cat "$scratch/$tag.trace.jsonl" >&2; return 1; }
  jq -s 'map(select(.kind=="json-copy")) | .[0]' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.observed.json"
}
verify_trace original a
verify_trace control b

jq -n --arg pin "$pin" --arg base "$base" --arg test "$test" --arg blob "$blob" --arg testSha "$(sha256 "$scratch/$test")" \
  --arg configSha "$(sha256 "$scratch/$config")" --arg node "$(node --version)" --arg name "$name" \
  --arg platform "$(node -p 'process.platform + "/" + process.arch')" --arg vitest "$vitest" \
  --arg integrity "$integrity" --arg runnerSha "$(sha256 "$archive")" --arg evaluatorSha "$(sha256 "$scratch/$key/dist/module-evaluator.js")" \
  --arg copy "$json_copy" --arg copySha "$(sha256 "$scratch/$json_copy")" --arg trackerSha "$(sha256 "$scratch/$tracker")" \
  --arg reference "$reference" --arg referenceSha "$(sha256 "$scratch/$reference")" --argjson importLine "$import_line" --argjson callLine "$call_line" \
  --argjson unselected "$unselected" --slurpfile copied "$scratch/copied.json" --slurpfile modules "$scratch/original.trace.jsonl" \
  --slurpfile witness "$scratch/original.observed.json" --slurpfile control "$scratch/control.observed.json" '
  {format:1,pin:$pin,evidenceBase:$base,node:$node,platform:$platform,configSha256:$configSha,
   original:{file:$test,blob:$blob,sha256:$testSha,selection:$name,passed:1,failed:0,unselected:$unselected,todo:0},
   runner:{vitest:$vitest,integrity:$integrity,archiveSha256:$runnerSha,evaluatorSha256:$evaluatorSha,copiedDependencies:$copied[0]},
   primaryReference:{file:$reference,sha256:$referenceSha,import:"@earendil-works/chord/json",importLine:$importLine,
     call:"copyJson",callLine:$callLine},
   observedModules:($modules | map(select(.kind=="source-evaluated") | . + {sourceSha256:(if .file==$copy then $copySha else $trackerSha end)}) | sort_by(.file)),
   witness:$witness[0],control:{change:"substitute-one-string-in-scratch-copyJson",case:$name,failed:1,unselected:$unselected,observed:$control[0]}}'
