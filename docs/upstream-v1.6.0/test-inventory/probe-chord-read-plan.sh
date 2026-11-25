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
base=c6a6625b2db95221616a118adab10a851032b3d9
test=packages/chord/test/store/read-plan.test.ts
config=packages/chord/vitest.config.ts
adapter=packages/chord/src/sqlite/node.ts
planner=packages/chord/src/store/read-plan.ts
reference=packages/durable/src/storage/sqlite/node.ts
deps="$here/../dependencies"
scratch=$(mktemp -d "$TMPDIR/chord-read-plan.XXXXXX")
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
git -C "$mirror" archive "$pin" package.json packages/{chord,schema,telemetry}/{src,package.json} "$test" "$config" \
  packages/chord/test/support.ts "$reference" | tar -x -C "$scratch"
import_line=$(grep -nE 'import \{ .*openNodeSqliteDatabase.*from "@earendil-works/chord/sqlite/node";' "$scratch/$reference" | cut -d: -f1)
call_line=$(grep -nF 'return SqliteStorage.open(await openNodeSqliteDatabase(' "$scratch/$reference" | cut -d: -f1)
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
cp -- "$here/chord-read-plan-witness.mjs" "$scratch/witness.mjs"

run() {
  local tag=$1
  shift
  (
    cd "$scratch/packages/chord"
    env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" CHORD_READ_PLAN_PROBE_ROOT="$scratch" \
      CHORD_READ_PLAN_PROBE_TRACE="$scratch/$tag.trace.jsonl" \
      NODE_OPTIONS="--max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons --import=$scratch/witness.mjs" \
      unshare -Urn timeout 90s node ../../node_modules/vitest/vitest.mjs run test/store/read-plan.test.ts --pool=forks --maxWorkers=1 \
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
jq -e '.numPassedTests==21 and .numFailedTests==0 and .numPendingTests==0 and .numTodoTests==0 and
  (.testResults|length)==1 and ([.testResults[].assertionResults[]|select(.status=="passed")|.fullName]|unique|length)==21' \
  "$scratch/original.result.json" > "$scratch/result.check" || { failures original; exit 1; }
jq '[.testResults[].assertionResults[]|.fullName]' "$scratch/original.result.json" > "$scratch/names.json"

# Reverse the scratch CROSS JOIN inputs, keeping the match condition intact.
# SQLite must now drive the query from the partner instead of the own table.
selector="join is driven from the query's own table, whatever the planner would prefer"
cp -- "$scratch/$planner" "$scratch/original-plan.ts"
# Match literal TypeScript template interpolations, not shell variables.
# shellcheck disable=SC2016
before='${quote(table)} AS ${OWN} CROSS JOIN ${quote(partner.table)} AS ${PARTNER} ON ${on}'
# shellcheck disable=SC2016
after='${quote(partner.table)} AS ${PARTNER} CROSS JOIN ${quote(table)} AS ${OWN} ON ${on}'
[[ $(grep -Fc "$before" "$scratch/$planner") == 1 ]]
sed -i "s/$before/$after/" "$scratch/$planner"
if run control -t "$selector"; then
  echo 'The partner-driven plan unexpectedly passed' >&2
  exit 1
else
  [[ $? == 1 ]] || { failures control; exit 1; }
fi
mv -- "$scratch/original-plan.ts" "$scratch/$planner"
flock -u 9
jq -e --arg name "$selector" '.numFailedTests==1 and .numPassedTests==0 and .numPendingTests==20 and .numTodoTests==0 and
  ([.testResults[].assertionResults[]|select(.status=="failed")|.fullName]==[$name]) and
  any(.testResults[].assertionResults[]|select(.status=="failed")|.failureMessages[]; contains("read-plan.test.ts:278:"))' \
  "$scratch/control.result.json" > "$scratch/control.check" || { failures control; exit 1; }

verify_trace() {
  local tag=$1 steps=$2
  jq -es --arg adapter "$adapter" --arg planner "$planner" --argjson steps "$steps" '
    length==3 and ([.[] | select(.kind=="source-evaluated")|[.file,.export]] | sort)==
      ([[$adapter,"openNodeSqliteDatabase"],[$planner,"ReadPlan"]]|sort) and
    (.[2] | .kind=="explain-plan" and .params==["u12","club",51] and .steps==$steps and .engine.version=="3.54.0")
  ' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.trace.check" || { cat "$scratch/$tag.trace.jsonl" >&2; return 1; }
  jq -s '{modules:([.[]|select(.kind=="source-evaluated")]|sort_by(.file)),plan:.[2]}' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.observed.json"
}
verify_trace original '["SEARCH t USING PRIMARY KEY (team=?)","SEARCH p USING PRIMARY KEY (id=?)","USE TEMP B-TREE FOR ORDER BY"]'
verify_trace control '["SEARCH p USING INDEX app.store:sessions:by_app (app=?)","SEARCH t USING PRIMARY KEY (team=? AND session=?)","USE TEMP B-TREE FOR LAST 2 TERMS OF ORDER BY"]'

jq -n --arg pin "$pin" --arg base "$base" --arg test "$test" --arg blob "$blob" --arg testSha "$(sha256 "$scratch/$test")" \
  --arg configSha "$(sha256 "$scratch/$config")" --arg node "$(node --version)" --arg adapter "$adapter" \
  --arg platform "$(node -p 'process.platform + "/" + process.arch')" --arg nodeSha "$(sha256 "$(realpath "$(command -v node)")")" \
  --arg adapterSha "$(sha256 "$scratch/$adapter")" --arg plannerSha "$(sha256 "$scratch/$planner")" --arg control "$selector" \
  --arg reference "$reference" --arg referenceSha "$(sha256 "$scratch/$reference")" --argjson importLine "$import_line" --argjson callLine "$call_line" \
  --arg vitest "$vitest" --arg integrity "$integrity" --arg runnerSha "$(sha256 "$archive")" \
  --arg evaluatorSha "$(sha256 "$scratch/$key/dist/module-evaluator.js")" \
  --slurpfile names "$scratch/names.json" --slurpfile copied "$scratch/copied.json" \
  --slurpfile observed "$scratch/original.observed.json" --slurpfile mutated "$scratch/control.observed.json" '
  {format:1,pin:$pin,evidenceBase:$base,node:$node,nodeExecutableSha256:$nodeSha,platform:$platform,configSha256:$configSha,
   original:{file:$test,blob:$blob,sha256:$testSha,selection:"whole-file",passed:21,failed:0,skipped:0,todo:0,caseNames:$names[0]},
   runner:{vitest:$vitest,integrity:$integrity,archiveSha256:$runnerSha,evaluatorSha256:$evaluatorSha,copiedDependencies:$copied[0]},
   primaryReference:{file:$reference,sha256:$referenceSha,import:"@earendil-works/chord/sqlite/node",importLine:$importLine,
     call:"openNodeSqliteDatabase",callLine:$callLine},
   observed:($observed[0] | .modules |= map(. + {sourceSha256:(if .file==$adapter then $adapterSha else $plannerSha end)})),
   control:{change:"reverse-scratch-CROSS-JOIN-inputs",case:$control,failed:1,unselected:20,plan:$mutated[0].plan}}'
