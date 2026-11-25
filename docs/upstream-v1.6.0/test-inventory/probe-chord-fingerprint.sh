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
base=3bce7c31d3dea01dc947baa39ad9a54439326cfc
test=packages/chord/test/contracts/fingerprint.test.ts
config=packages/chord/vitest.config.ts
json_guard=packages/chord/src/json.ts
fingerprint=packages/chord/src/contracts/fingerprint.ts
snapshot=packages/chord/src/contracts/snapshot.ts
reference=packages/durable/src/signals.ts
deps="$here/../dependencies"
scratch=$(mktemp -d "$TMPDIR/chord-fingerprint.XXXXXX")
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
import_line=$(grep -nF 'import { isJsonValue, type JsonValue } from "@earendil-works/chord/json";' "$scratch/$reference" | cut -d: -f1)
call_line=$(grep -nF '!isJsonValue(value)' "$scratch/$reference" | cut -d: -f1)
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
cp -- "$here/chord-fingerprint-witness.mjs" "$scratch/witness.mjs"

run() {
  local tag=$1
  shift
  (
    cd "$scratch/packages/chord"
    env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" CHORD_FINGERPRINT_PROBE_ROOT="$scratch" \
      CHORD_FINGERPRINT_PROBE_TRACE="$scratch/$tag.trace.jsonl" CHORD_FINGERPRINT_PROBE_PHASE="$tag" \
      NODE_OPTIONS="--max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons --import=$scratch/witness.mjs" \
      unshare -Urn timeout 90s node ../../node_modules/vitest/vitest.mjs run test/contracts/fingerprint.test.ts --pool=forks --maxWorkers=1 \
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
jq -e '.numPassedTests==13 and .numFailedTests==0 and .numPendingTests==0 and .numTodoTests==0 and
  (.testResults|length)==1 and ([.testResults[].assertionResults[]|select(.status=="passed")|.fullName]|unique|length)==13' \
  "$scratch/original.result.json" > "$scratch/original.check" || { failures original; jq 'del(.testResults)' "$scratch/original.result.json" >&2; exit 1; }
jq '[.testResults[].assertionResults[]|.fullName]' "$scratch/original.result.json" > "$scratch/names.json"

selector='fingerprints are stable across key order and annotations'
run witness -t "$selector" || { failures witness; exit 1; }
jq -e --arg name "$selector" '.numFailedTests==0 and .numPassedTests==1 and .numPendingTests==12 and .numTodoTests==0 and
  ([.testResults[].assertionResults[]|select(.status=="passed")|.fullName]==[$name])' \
  "$scratch/witness.result.json" > "$scratch/witness.check" || { failures witness; exit 1; }

# Keep annotations in the scratch normal form, without changing JSON checks.
cp -- "$scratch/$snapshot" "$scratch/original-snapshot.ts"
[[ $(grep -Fc 'if (keyword !== "annotation") normal.push([key, copy.normal]);' "$scratch/$snapshot") == 1 ]]
sed -i 's/if (keyword !== "annotation") normal.push/normal.push/' "$scratch/$snapshot"
if run control -t "$selector"; then
  echo 'The annotation-dependent scratch fingerprint unexpectedly passed' >&2
  exit 1
else
  [[ $? == 1 ]] || { failures control; exit 1; }
fi
mv -- "$scratch/original-snapshot.ts" "$scratch/$snapshot"
flock -u 9
jq -e --arg name "$selector" '.numFailedTests==1 and .numPassedTests==0 and .numPendingTests==12 and .numTodoTests==0 and
  ([.testResults[].assertionResults[]|select(.status=="failed")|.fullName]==[$name]) and
  any(.testResults[].assertionResults[]|select(.status=="failed")|.failureMessages[]; contains("fingerprint.test.ts:57:"))' \
  "$scratch/control.result.json" > "$scratch/control.check" || { failures control; exit 1; }

for tag in original witness control; do
  jq -es --arg guard "$json_guard" --arg fingerprint "$fingerprint" '
    ([.[] | select(.kind=="source-evaluated")|[.file,.export]] | sort)==([[$guard,"isJsonValue"],[$fingerprint,"materialize"]]|sort)
  ' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.modules.check" || { cat "$scratch/$tag.trace.jsonl" >&2; exit 1; }
  jq -s '{jsonChecks:[.[]|select(.kind=="json-guard")|del(.kind)],fingerprints:[.[]|select(.kind=="fingerprint")|.value]}' \
    "$scratch/$tag.trace.jsonl" > "$scratch/$tag.observed.json"
done
jq -e '.jsonChecks==[{value:"T",valid:true}] and (.fingerprints|length)==2 and .fingerprints[0]==.fingerprints[1]' \
  "$scratch/witness.observed.json" > "$scratch/witness.trace.check" || { cat "$scratch/witness.observed.json" >&2; exit 1; }
jq -e '.jsonChecks==[{value:"T",valid:true}] and (.fingerprints|length)==2 and .fingerprints[0]!=.fingerprints[1]' \
  "$scratch/control.observed.json" > "$scratch/control.trace.check" || { cat "$scratch/control.observed.json" >&2; exit 1; }

jq -n --arg pin "$pin" --arg base "$base" --arg test "$test" --arg blob "$blob" --arg testSha "$(sha256 "$scratch/$test")" \
  --arg configSha "$(sha256 "$scratch/$config")" --arg node "$(node --version)" --arg selector "$selector" \
  --arg platform "$(node -p 'process.platform + "/" + process.arch')" --arg vitest "$vitest" \
  --arg integrity "$integrity" --arg runnerSha "$(sha256 "$archive")" --arg evaluatorSha "$(sha256 "$scratch/$key/dist/module-evaluator.js")" \
  --arg guard "$json_guard" --arg guardSha "$(sha256 "$scratch/$json_guard")" --arg fingerprintSha "$(sha256 "$scratch/$fingerprint")" \
  --arg reference "$reference" --arg referenceSha "$(sha256 "$scratch/$reference")" --argjson importLine "$import_line" --argjson callLine "$call_line" \
  --slurpfile names "$scratch/names.json" --slurpfile copied "$scratch/copied.json" --slurpfile modules "$scratch/original.trace.jsonl" \
  --slurpfile witness "$scratch/witness.observed.json" --slurpfile control "$scratch/control.observed.json" '
  {format:1,pin:$pin,evidenceBase:$base,node:$node,platform:$platform,configSha256:$configSha,
   original:{file:$test,blob:$blob,sha256:$testSha,selection:"whole-file",passed:13,failed:0,skipped:0,todo:0,caseNames:$names[0]},
   runner:{vitest:$vitest,integrity:$integrity,archiveSha256:$runnerSha,evaluatorSha256:$evaluatorSha,copiedDependencies:$copied[0]},
   primaryReference:{file:$reference,sha256:$referenceSha,import:"@earendil-works/chord/json",importLine:$importLine,
     call:"isJsonValue",callLine:$callLine},
   observedModules:($modules | sort_by(.file) | map(. + {sourceSha256:(if .file==$guard then $guardSha else $fingerprintSha end)})),
   witness:{case:$selector,passed:1,unselected:12,observed:$witness[0]},
   control:{change:"retain-annotations-in-scratch-normal-form",case:$selector,failed:1,unselected:12,observed:$control[0]}}'
