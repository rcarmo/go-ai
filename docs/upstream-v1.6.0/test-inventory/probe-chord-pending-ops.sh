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
base=60dfad60f69141e5f7932714982a93de2f37411d
test=packages/chord/test/remote/provider.test.ts
config=packages/chord/vitest.config.ts
pending=packages/chord/src/delta/pending.ts
provider=packages/chord/src/remote/provider.ts
reference=packages/durable/src/session/session.ts
deps="$here/../dependencies"
scratch=$(mktemp -d "$TMPDIR/chord-pending-ops.XXXXXX")
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
import_line=$(grep -nE '^import .*PendingOps.*from "@earendil-works/chord/delta";' "$scratch/$reference" | cut -d: -f1)
call_line=$(grep -nF 'hold.pending.push(ops);' "$scratch/$reference" | cut -d: -f1)
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
cp -- "$here/chord-pending-ops-witness.mjs" "$scratch/witness.mjs"

run() {
  local tag=$1
  shift
  (
    cd "$scratch/packages/chord"
    env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" CHORD_PENDING_PROBE_ROOT="$scratch" \
      CHORD_PENDING_PROBE_TRACE="$scratch/$tag.trace.jsonl" CHORD_PENDING_PROBE_PHASE="$tag" \
      NODE_OPTIONS="--max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons --import=$scratch/witness.mjs" \
      unshare -Urn timeout 90s node ../../node_modules/vitest/vitest.mjs run test/remote/provider.test.ts --pool=forks --maxWorkers=1 \
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
selector='the window at the provider holds at most two unacknowledged bound and state messages; what waits goes out folded, encoded once'
run original -t "$selector" || { failures original; exit 1; }
jq -e --arg name "$selector" '.numFailedTests==0 and .numPassedTests==2 and .numPendingTests>0 and .numTodoTests==0 and
  ([.testResults[].assertionResults[]|select(.status=="passed")|.fullName]|sort)==(["binary codec "+$name,"text codec "+$name]|sort)' \
  "$scratch/original.result.json" > "$scratch/original.check" || { failures original; jq 'del(.testResults)' "$scratch/original.result.json" >&2; exit 1; }
jq '[.testResults[].assertionResults[]|select(.status=="passed")|.fullName]' "$scratch/original.result.json" > "$scratch/names.json"
unselected=$(jq -er '.numPendingTests' "$scratch/original.result.json")
control_unselected=$((unselected + 1))

# Keep the first root replacement instead of letting the later one replace it.
cp -- "$scratch/$pending" "$scratch/original-pending.ts"
[[ $(grep -Fc 'if (ops[0]![0] === "r") {' "$scratch/$pending") == 1 ]]
sed -i '/if (ops\[0\]!\[0\] === "r") {/a\            if (state.kind !== "empty") return;' "$scratch/$pending"
control_name="binary codec $selector"
if run control -t "$control_name"; then
  echo 'The stale scratch replacement unexpectedly passed' >&2
  exit 1
else
  [[ $? == 1 ]] || { failures control; exit 1; }
fi
mv -- "$scratch/original-pending.ts" "$scratch/$pending"
flock -u 9
jq -e --arg name "$control_name" --argjson unselected "$control_unselected" '.numFailedTests==1 and .numPassedTests==0 and .numPendingTests==$unselected and .numTodoTests==0 and
  ([.testResults[].assertionResults[]|select(.status=="failed")|.fullName]==[$name]) and
  any(.testResults[].assertionResults[]|select(.status=="failed")|.failureMessages[]; contains("provider.test.ts:671:"))' \
  "$scratch/control.result.json" > "$scratch/control.check" || { failures control; exit 1; }

verify_trace() {
  local tag=$1 count=$2 last=$3
  jq -es --arg pending "$pending" --arg provider "$provider" --argjson count "$count" --argjson last "$last" '
    ([.[] | select(.kind=="source-evaluated")|[.file,.export]] | sort)==([[$pending,"PendingOps"],[$provider,"ProviderSide"]]|sort) and
    ([.[]|select(.kind=="queue-take")] | length==$count and all(.[]; .=={kind:"queue-take",pushed:[2,3,4],result:{kind:"ops",ops:[["r",$last]]}}))
  ' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.trace.check" || { cat "$scratch/$tag.trace.jsonl" >&2; return 1; }
  jq -s 'map(select(.kind=="queue-take"))' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.observed.json"
}
verify_trace original 2 4
verify_trace control 1 2

jq -n --arg pin "$pin" --arg base "$base" --arg test "$test" --arg blob "$blob" --arg testSha "$(sha256 "$scratch/$test")" \
  --arg configSha "$(sha256 "$scratch/$config")" --arg node "$(node --version)" --arg selector "$selector" \
  --arg platform "$(node -p 'process.platform + "/" + process.arch')" --arg vitest "$vitest" \
  --arg integrity "$integrity" --arg runnerSha "$(sha256 "$archive")" --arg evaluatorSha "$(sha256 "$scratch/$key/dist/module-evaluator.js")" \
  --arg pending "$pending" --arg pendingSha "$(sha256 "$scratch/$pending")" --arg providerSha "$(sha256 "$scratch/$provider")" \
  --arg reference "$reference" --arg referenceSha "$(sha256 "$scratch/$reference")" --argjson importLine "$import_line" --argjson callLine "$call_line" \
  --argjson unselected "$unselected" --arg controlName "$control_name" --argjson controlUnselected "$control_unselected" \
  --slurpfile names "$scratch/names.json" --slurpfile copied "$scratch/copied.json" --slurpfile modules "$scratch/original.trace.jsonl" \
  --slurpfile witness "$scratch/original.observed.json" --slurpfile control "$scratch/control.observed.json" '
  {format:1,pin:$pin,evidenceBase:$base,node:$node,platform:$platform,configSha256:$configSha,
   original:{file:$test,blob:$blob,sha256:$testSha,selection:$selector,caseNames:$names[0],passed:2,failed:0,unselected:$unselected,todo:0},
   runner:{vitest:$vitest,integrity:$integrity,archiveSha256:$runnerSha,evaluatorSha256:$evaluatorSha,copiedDependencies:$copied[0]},
   primaryReference:{file:$reference,sha256:$referenceSha,import:"@earendil-works/chord/delta",importLine:$importLine,
     call:"PendingOps.push",callLine:$callLine},
   observedModules:($modules | map(select(.kind=="source-evaluated") | . + {sourceSha256:(if .file==$pending then $pendingSha else $providerSha end)}) | sort_by(.file)),
   witness:$witness[0],control:{change:"retain-first-root-replacement-in-scratch-pending-ops",case:$controlName,failed:1,unselected:$controlUnselected,observed:$control[0]}}'
