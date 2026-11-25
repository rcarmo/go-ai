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
base=4db6f2123184ab22e0fe6d09b8e3aa40cfa90183
test=packages/chord/test/state/derive.test.ts
config=packages/chord/vitest.config.ts
watch=packages/chord/src/watch.ts
derive=packages/chord/src/state/derive.ts
reference=packages/durable/src/harness/task-graph.ts
deps="$here/../dependencies"
scratch=$(mktemp -d "$TMPDIR/chord-derive-watch.XXXXXX")
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
import_line=$(grep -nE 'import \{ Watch,.*from "@earendil-works/chord/watch";' "$scratch/$reference" | cut -d: -f1)
advance_line=$(grep -nF 'observer.advance(call, frameContext)' "$scratch/$reference" | cut -d: -f1)
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
cp -- "$here/chord-derive-watch-witness.mjs" "$scratch/witness.mjs"

run() {
  local tag=$1
  shift
  (
    cd "$scratch/packages/chord"
    env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" CHORD_WATCH_PROBE_ROOT="$scratch" \
      CHORD_WATCH_PROBE_TRACE="$scratch/$tag.trace.jsonl" CHORD_WATCH_PROBE_PHASE="$tag" \
      NODE_OPTIONS="--expose-gc --max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons --import=$scratch/witness.mjs" \
      unshare -Urn timeout 90s node ../../node_modules/vitest/vitest.mjs run test/state/derive.test.ts --pool=forks --maxWorkers=1 \
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
jq -e '.numPassedTests==23 and .numFailedTests==0 and .numPendingTests==0 and .numTodoTests==0 and
  (.testResults|length)==1 and ([.testResults[].assertionResults[]|select(.status=="passed")|.fullName]|unique|length)==23' \
  "$scratch/original.result.json" > "$scratch/result.check" || { failures original; exit 1; }
jq '[.testResults[].assertionResults[]|.fullName]' "$scratch/original.result.json" > "$scratch/names.json"

selector='failures fold a failure and a value for a busy listener: the value replaces the failure, as a root replacement'
run witness -t "$selector" || { failures witness; exit 1; }
jq -e --arg name "$selector" '.numFailedTests==0 and .numPassedTests==1 and .numPendingTests==22 and .numTodoTests==0 and
  ([.testResults[].assertionResults[]|select(.status=="passed")|.fullName]==[$name])' \
  "$scratch/witness.result.json" > "$scratch/witness.check" || { failures witness; exit 1; }

# Keep the pending failure when a healthy value supersedes it in scratch.
cp -- "$scratch/$watch" "$scratch/original-watch.ts"
[[ $(grep -Fx -A1 $'\t\tif (this.#replace) {' "$scratch/$watch" | grep -Fc 'this.#failure = undefined;') == 1 ]]
sed -i '/^\s*if (this.#replace) {$/ {n; s/this.#failure = undefined;/\/\/ keep the pending failure in the scratch fault/;}' "$scratch/$watch"
if run control -t "$selector"; then
  echo 'The retained failure unexpectedly passed' >&2
  exit 1
else
  [[ $? == 1 ]] || { failures control; exit 1; }
fi
mv -- "$scratch/original-watch.ts" "$scratch/$watch"
flock -u 9
jq -e --arg name "$selector" '.numFailedTests==1 and .numPassedTests==0 and .numPendingTests==22 and .numTodoTests==0 and
  ([.testResults[].assertionResults[]|select(.status=="failed")|.fullName]==[$name]) and
  any(.testResults[].assertionResults[]|select(.status=="failed")|.failureMessages[]; contains("derive.test.ts:377:"))' \
  "$scratch/control.result.json" > "$scratch/control.check" || { failures control; exit 1; }

for tag in original witness control; do
  jq -es --arg watch "$watch" --arg derive "$derive" '
    ([.[] | select(.kind=="source-evaluated")|[.file,.export]] | sort)==([[$watch,"Watch"],[$derive,"Derive"]]|sort)
  ' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.modules.check" || { cat "$scratch/$tag.trace.jsonl" >&2; exit 1; }
  jq -s '[.[]|select(.kind!="source-evaluated")]' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.calls.json"
done
jq -e 'length==0' "$scratch/original.calls.json" > "$scratch/original.calls.check"
cat > "$scratch/expected-calls.json" <<'JSON'
[
  {"kind":"source","call":{"kind":"value","value":5,"ops":[["r",5]],"retired":false}},
  {"kind":"listener","call":{"kind":"value","value":5,"ops":[["r",5]],"retired":false}},
  {"kind":"source","call":{"kind":"failed","error":"t threw"}},
  {"kind":"source","call":{"kind":"value","value":6,"ops":[["r",6]],"retired":false}},
  {"kind":"listener","call":{"kind":"value","value":6,"ops":[["r",6]],"retired":false}}
]
JSON
jq -e --slurpfile expected "$scratch/expected-calls.json" '.==$expected[0]' "$scratch/witness.calls.json" \
  > "$scratch/witness.calls.check" || { cat "$scratch/witness.calls.json" >&2; exit 1; }
jq -e --slurpfile expected "$scratch/expected-calls.json" '.==($expected[0][0:4] + [{kind:"listener",call:{kind:"failed",error:"t threw"}}])' \
  "$scratch/control.calls.json" > "$scratch/control.calls.check" || { cat "$scratch/control.calls.json" >&2; exit 1; }

jq -n --arg pin "$pin" --arg base "$base" --arg test "$test" --arg blob "$blob" --arg testSha "$(sha256 "$scratch/$test")" \
  --arg configSha "$(sha256 "$scratch/$config")" --arg node "$(node --version)" --arg selector "$selector" \
  --arg platform "$(node -p 'process.platform + "/" + process.arch')" --arg vitest "$vitest" \
  --arg integrity "$integrity" --arg runnerSha "$(sha256 "$archive")" --arg evaluatorSha "$(sha256 "$scratch/$key/dist/module-evaluator.js")" \
  --arg reference "$reference" --arg referenceSha "$(sha256 "$scratch/$reference")" --argjson importLine "$import_line" --argjson advanceLine "$advance_line" \
  --arg watch "$watch" --arg watchSha "$(sha256 "$scratch/$watch")" --arg derive "$derive" --arg deriveSha "$(sha256 "$scratch/$derive")" \
  --slurpfile names "$scratch/names.json" --slurpfile copied "$scratch/copied.json" --slurpfile modules "$scratch/original.trace.jsonl" \
  --slurpfile witness "$scratch/witness.calls.json" --slurpfile control "$scratch/control.calls.json" '
  {format:1,pin:$pin,evidenceBase:$base,node:$node,platform:$platform,configSha256:$configSha,
   original:{file:$test,blob:$blob,sha256:$testSha,selection:"whole-file",passed:23,failed:0,skipped:0,todo:0,caseNames:$names[0]},
   runner:{vitest:$vitest,integrity:$integrity,archiveSha256:$runnerSha,evaluatorSha256:$evaluatorSha,copiedDependencies:$copied[0]},
   primaryReference:{file:$reference,sha256:$referenceSha,import:"@earendil-works/chord/watch",importLine:$importLine,
     call:"Watch.advance",callLine:$advanceLine},
   observedModules:($modules | sort_by(.file) | map(. + {sourceSha256:(if .file==$watch then $watchSha else $deriveSha end)})),
   witness:{case:$selector,passed:1,unselected:22,calls:$witness[0]},
   control:{change:"retain-superseded-failure-in-scratch-Watch.advance",case:$selector,failed:1,unselected:22,calls:$control[0]}}'
