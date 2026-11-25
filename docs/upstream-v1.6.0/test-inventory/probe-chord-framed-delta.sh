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
base=d2742a93489bf8be895f3d9a86bb4e4b39e6a547
test=packages/chord/test/state/framed.test.ts
config=packages/chord/vitest.config.ts
delta=packages/chord/src/delta/index.ts
framed=packages/chord/src/state/framed.ts
deps="$here/../dependencies"
scratch=$(mktemp -d "$TMPDIR/chord-framed-delta.XXXXXX")
trap 'rm -r -- "$scratch"' EXIT
mkdir -p "$scratch/home" "$scratch/tmp"
sha256() { sha256sum -- "$1" | cut -d' ' -f1; }

git -C "$here" show "$base:docs/upstream-v1.6.0/test-inventory/files.jsonl" > "$scratch/inventory.jsonl"
blob=$(git -C "$mirror" rev-parse "$pin:$test")
jq -e --arg test "$test" --arg blob "$blob" '
  select(.kind=="path" and .path==$test) | .scope=="shared-dependency" and .role=="suite" and .change=="A" and .pinned.object==$blob
' "$scratch/inventory.jsonl" > "$scratch/inventory.check"
git -C "$mirror" show "$pin:package-lock.json" > "$scratch/lock.json"
git -C "$here" show "$base:docs/upstream-v1.6.0/dependencies/npm-closure.jsonl" > "$scratch/closure.jsonl"
lock_sha=$(sha256 "$scratch/lock.json")
jq -e --arg pin "$pin" --arg sha "$lock_sha" 'select(.kind=="manifest") | .pin==$pin and .inputs["package-lock.json"]==$sha' \
  "$scratch/closure.jsonl" > "$scratch/lock.check"
git -C "$mirror" archive "$pin" package.json packages/{chord,schema,telemetry}/{src,package.json} "$test" "$config" | tar -x -C "$scratch"
for source in packages/durable/src/harness/task-graph.ts packages/durable/src/harness/view.ts; do
  git -C "$mirror" show "$pin:$source" > "$scratch/ref.ts"
  line=$(grep -nE 'import \{ .*applyImmutable.*from "@earendil-works/chord/delta";' "$scratch/ref.ts" | cut -d: -f1)
  call=$(grep -nF '= applyImmutable(' "$scratch/ref.ts" | cut -d: -f1)
  jq -nc --arg file "$source" --arg sha "$(sha256 "$scratch/ref.ts")" --argjson line "$line" --argjson call "$call" \
    '{file:$file,sourceSha256:$sha,import:"@earendil-works/chord/delta",importLine:$line,call:"applyImmutable",callLine:$call}'
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
cp -- "$here/chord-framed-delta-witness.mjs" "$scratch/witness.mjs"

run() {
  local tag=$1
  shift
  (
    cd "$scratch/packages/chord"
    env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" CHORD_FRAMED_PROBE_ROOT="$scratch" \
      CHORD_FRAMED_PROBE_TRACE="$scratch/$tag.trace.jsonl" \
      NODE_OPTIONS="--max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons --import=$scratch/witness.mjs" \
      unshare -Urn timeout 90s node ../../node_modules/vitest/vitest.mjs run test/state/framed.test.ts --pool=forks --maxWorkers=1 \
        --reporter=json --outputFile "$scratch/$tag.result.json" "$@"
  ) > "$scratch/$tag.log" 2>&1
}
exec 9>/tmp/go-ai/v160-focused-verify.lock
flock 9
run original || { cat "$scratch/original.log" >&2; if [[ -s $scratch/original.result.json ]]; then jq '[.testResults[].assertionResults[]|select(.status=="failed")|{fullName,failureMessages}]' "$scratch/original.result.json" >&2; fi; exit 1; }
jq -e '.numPassedTests==8 and .numFailedTests==0 and .numPendingTests==0 and .numTodoTests==0 and
  (.testResults|length)==1 and ([.testResults[].assertionResults[]|select(.status=="passed")|.fullName]|unique|length)==8' \
  "$scratch/original.result.json" > "$scratch/result.check" || { cat "$scratch/original.log" >&2; exit 1; }
jq '[.testResults[].assertionResults[]|.fullName]' "$scratch/original.result.json" > "$scratch/names.json"

# Keep the emitted frame and test intact. Return the prior value from the
# scratch replay function, so only the recomposition assertion rejects it.
selector='a framed source set several times in one batch delivers one frame: its last value, diffed from the value before the batch'
cp -- "$scratch/$delta" "$scratch/original-delta.ts"
sed -i '/^export function applyImmutable</,/return root as unknown as T;/ s/return root as unknown as T;/return target as T;/' "$scratch/$delta"
[[ $(grep -Fc 'return target as T;' "$scratch/$delta") == 1 ]]
if run control -t "$selector"; then
  echo 'The replay that ignored changes unexpectedly passed' >&2
  exit 1
else
  [[ $? == 1 ]] || { cat "$scratch/control.log" >&2; exit 1; }
fi
mv -- "$scratch/original-delta.ts" "$scratch/$delta"
flock -u 9
jq -e --arg name "$selector" '.numFailedTests==1 and .numPassedTests==0 and .numPendingTests==7 and .numTodoTests==0 and
  ([.testResults[].assertionResults[]|select(.status=="failed")|.fullName]==[$name]) and
  any(.testResults[].assertionResults[]|select(.status=="failed")|.failureMessages[]; contains("framed.test.ts:68:"))' \
  "$scratch/control.result.json" > "$scratch/control.check" || { cat "$scratch/control.log" >&2; jq '[.testResults[].assertionResults[]|select(.status=="failed")|{fullName,failureMessages}]' "$scratch/control.result.json" >&2; exit 1; }

verify_trace() {
  local tag=$1 value=$2
  jq -es --arg delta "$delta" --arg framed "$framed" --argjson value "$value" '
    length==3 and ([.[] | select(.kind=="source-evaluated")|[.file,.export]] | sort)==([[$delta,"applyImmutable"],[$framed,"FramedSource"]]|sort) and
    (.[2]=={kind:"delta-replay",before:{a:1,b:1,c:1},ops:[["s",["a"],2],["s",["b"],3],["s",["c"],4]],after:$value,inputUnchanged:true})
  ' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.trace.check" || { cat "$scratch/$tag.trace.jsonl" >&2; return 1; }
  jq -s '{modules:([.[]|select(.kind=="source-evaluated")]|sort_by(.file)),replay:.[2]}' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.observed.json"
}
verify_trace original '{"a":2,"b":3,"c":4}'
verify_trace control '{"a":1,"b":1,"c":1}'

jq -n --arg pin "$pin" --arg base "$base" --arg test "$test" --arg blob "$blob" --arg testSha "$(sha256 "$scratch/$test")" \
  --arg configSha "$(sha256 "$scratch/$config")" --arg node "$(node --version)" --arg delta "$delta" \
  --arg deltaSha "$(sha256 "$scratch/$delta")" --arg framedSha "$(sha256 "$scratch/$framed")" --arg control "$selector" \
  --arg vitest "$vitest" --arg integrity "$integrity" --arg runnerSha "$(sha256 "$archive")" \
  --arg evaluatorSha "$(sha256 "$scratch/$key/dist/module-evaluator.js")" \
  --slurpfile names "$scratch/names.json" --slurpfile copied "$scratch/copied.json" --slurpfile refs "$scratch/refs.jsonl" \
  --slurpfile observed "$scratch/original.observed.json" --slurpfile mutated "$scratch/control.observed.json" '
  {format:1,pin:$pin,evidenceBase:$base,node:$node,configSha256:$configSha,
   original:{file:$test,blob:$blob,sha256:$testSha,selection:"whole-file",passed:8,failed:0,skipped:0,todo:0,caseNames:$names[0]},
   runner:{vitest:$vitest,integrity:$integrity,archiveSha256:$runnerSha,evaluatorSha256:$evaluatorSha,copiedDependencies:$copied[0]},
   primaryReferences:$refs,observed:($observed[0] | .modules |= map(. + {sourceSha256:(if .file==$delta then $deltaSha else $framedSha end)})),
   control:{change:"return-unchanged-prior-value-from-immutable-replay-in-scratch",case:$control,failed:1,unselected:7,replay:$mutated[0].replay}}'
