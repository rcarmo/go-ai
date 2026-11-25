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
base=0ec852038cec8b94d22b6a8c85b00233a9c10b5a
test=packages/chord/test/store/store.test.ts
config=packages/chord/vitest.config.ts
adapter=packages/chord/src/sqlite/node.ts
store=packages/chord/src/store/store.ts
reference=packages/durable/src/storage/sqlite/node.ts
deps="$here/../dependencies"
scratch=$(mktemp -d "$TMPDIR/chord-series-rollback.XXXXXX")
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
import_line=$(grep -nE '^import .*from "@earendil-works/chord/sqlite/node";' "$scratch/$reference" | cut -d: -f1)
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
cp -- "$here/chord-series-rollback-witness.mjs" "$scratch/witness.mjs"

run() {
  local tag=$1
  shift
  (
    cd "$scratch/packages/chord"
    env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" CHORD_SERIES_PROBE_ROOT="$scratch" \
      CHORD_SERIES_PROBE_TRACE="$scratch/$tag.trace.jsonl" CHORD_SERIES_PROBE_PHASE="$tag" \
      NODE_OPTIONS="--expose-gc --max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons --import=$scratch/witness.mjs" \
      unshare -Urn timeout 90s node ../../node_modules/vitest/vitest.mjs run test/store/store.test.ts --pool=forks --maxWorkers=1 \
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
run original -t '^a commit series ' || { failures original; exit 1; }
jq -e '.numPassedTests==12 and .numFailedTests==0 and .numPendingTests==44 and .numTodoTests==0 and
  (.testResults|length)==1 and ([.testResults[].assertionResults[]|select(.status=="passed")|.fullName]|unique|length)==12' \
  "$scratch/original.result.json" > "$scratch/result.check" || { cat "$scratch/original.log" >&2; jq 'del(.testResults)' "$scratch/original.result.json" >&2; exit 1; }
jq '[.testResults[].assertionResults[]|select(.status=="passed")|.fullName]' "$scratch/original.result.json" > "$scratch/names.json"

selector="a commit series rolls the whole series back when a commit's writes throw, the store going on"
run witness -t "$selector" || { failures witness; exit 1; }
jq -e --arg name "$selector" '.numFailedTests==0 and .numPassedTests==1 and .numPendingTests==55 and .numTodoTests==0 and
  ([.testResults[].assertionResults[]|select(.status=="passed")|.fullName]==[$name])' \
  "$scratch/witness.result.json" > "$scratch/witness.check" || { failures witness; exit 1; }

# Commit the scratch transaction on error. Keep the error and original intact.
cp -- "$scratch/$adapter" "$scratch/original-adapter.ts"
[[ $(grep -Fc 'this.database.exec("ROLLBACK");' "$scratch/$adapter") == 1 ]]
sed -i 's/this.database.exec("ROLLBACK");/this.database.exec("COMMIT");/' "$scratch/$adapter"
if run control -t "$selector"; then
  echo 'The transaction that retained the first write unexpectedly passed' >&2
  exit 1
else
  [[ $? == 1 ]] || { failures control; exit 1; }
fi
mv -- "$scratch/original-adapter.ts" "$scratch/$adapter"
flock -u 9
jq -e --arg name "$selector" '.numFailedTests==1 and .numPassedTests==0 and .numPendingTests==55 and .numTodoTests==0 and
  ([.testResults[].assertionResults[]|select(.status=="failed")|.fullName]==[$name]) and
  any(.testResults[].assertionResults[]|select(.status=="failed")|.failureMessages[];
    (split("\n")[0]=="Error: UNIQUE constraint failed: chord_documents.owner, chord_documents.namespace, chord_documents.kind, chord_documents.key"))' \
  "$scratch/control.result.json" > "$scratch/control.check" || { failures control; exit 1; }

for tag in original witness control; do
  jq -es --arg adapter "$adapter" --arg store "$store" '
    ([.[] | select(.kind=="source-evaluated")|[.file,.export]] | sort)==([[$adapter,"NodeSqliteDatabase"],[$store,"ChordStore"]]|sort)
  ' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.modules.check" || { cat "$scratch/$tag.trace.jsonl" >&2; exit 1; }
  jq -s '[.[]|select(.kind!="source-evaluated")]' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.transactions.json"
done
jq -e 'length==0' "$scratch/original.transactions.json" > "$scratch/original.transactions.check"
verify_transaction() {
  local tag=$1 end=$2 remaining=$3
  jq -e --arg end "$end" --argjson n "$remaining" 'length==1 and (.[0] | .kind=="failed-transaction" and
      .begin=="BEGIN IMMEDIATE" and .sql=="INSERT INTO no_such_table VALUES (1)" and .error=="no such table: no_such_table" and
      .documentsBeforeEnd==1 and .end==$end and .documentsAfterEnd==$n and .engine.version=="3.54.0")' \
    "$scratch/$tag.transactions.json" > "$scratch/$tag.transaction.check" || { cat "$scratch/$tag.transactions.json" >&2; return 1; }
}
verify_transaction witness ROLLBACK 0
verify_transaction control COMMIT 1

jq -n --arg pin "$pin" --arg base "$base" --arg test "$test" --arg blob "$blob" --arg testSha "$(sha256 "$scratch/$test")" \
  --arg configSha "$(sha256 "$scratch/$config")" --arg node "$(node --version)" --arg selector "$selector" \
  --arg platform "$(node -p 'process.platform + "/" + process.arch')" --arg vitest "$vitest" \
  --arg integrity "$integrity" --arg runnerSha "$(sha256 "$archive")" --arg evaluatorSha "$(sha256 "$scratch/$key/dist/module-evaluator.js")" \
  --arg reference "$reference" --arg referenceSha "$(sha256 "$scratch/$reference")" --argjson importLine "$import_line" --argjson callLine "$call_line" \
  --arg adapter "$adapter" --arg adapterSha "$(sha256 "$scratch/$adapter")" --arg storeSha "$(sha256 "$scratch/$store")" \
  --slurpfile names "$scratch/names.json" --slurpfile copied "$scratch/copied.json" --slurpfile modules "$scratch/original.trace.jsonl" \
  --slurpfile witness "$scratch/witness.transactions.json" --slurpfile control "$scratch/control.transactions.json" '
  {format:1,pin:$pin,evidenceBase:$base,node:$node,platform:$platform,configSha256:$configSha,
   original:{file:$test,blob:$blob,sha256:$testSha,selection:"a commit series",passed:12,failed:0,unselected:44,todo:0,caseNames:$names[0]},
   runner:{vitest:$vitest,integrity:$integrity,archiveSha256:$runnerSha,evaluatorSha256:$evaluatorSha,copiedDependencies:$copied[0]},
   primaryReference:{file:$reference,sha256:$referenceSha,import:"@earendil-works/chord/sqlite/node",importLine:$importLine,
     call:"openNodeSqliteDatabase",callLine:$callLine},
   observedModules:($modules | sort_by(.file) | map(. + {sourceSha256:(if .file==$adapter then $adapterSha else $storeSha end)})),
   witness:{case:$selector,passed:1,unselected:55,transaction:$witness[0][0]},
   control:{change:"commit-on-error-in-scratch-NodeSqliteDatabase.transaction",case:$selector,failed:1,unselected:55,transaction:$control[0][0]}}'
