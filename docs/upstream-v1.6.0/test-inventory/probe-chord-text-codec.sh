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
base=3d0a45b2185b7177439d0cfe8ba60b5c23309b81
test=packages/chord/test/link/message.test.ts
config=packages/chord/vitest.config.ts
validator=packages/schema/src/validator.ts
message=packages/chord/src/link/message.ts
deps="$here/../dependencies"
scratch=$(mktemp -d "$TMPDIR/chord-text-codec.XXXXXX")
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
for source in packages/ai/src/utils/validation.ts packages/durable/src/harness/tool.ts; do
  git -C "$mirror" show "$pin:$source" > "$scratch/ref.ts"
  line=$(grep -nE '^import \{ .*compile.*from "@earendil-works/pi-schema";' "$scratch/ref.ts" | cut -d: -f1)
  jq -nc --arg file "$source" --arg sha "$(sha256 "$scratch/ref.ts")" --argjson line "$line" \
    '{file:$file,sourceSha256:$sha,line:$line,import:"@earendil-works/pi-schema",binding:"compile"}'
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
cp -- "$here/chord-text-codec-witness.mjs" "$scratch/witness.mjs"

run() {
  local tag=$1
  shift
  (
    cd "$scratch/packages/chord"
    env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" CHORD_CODEC_PROBE_ROOT="$scratch" \
      CHORD_CODEC_PROBE_TRACE="$scratch/$tag.trace.jsonl" CHORD_CODEC_PROBE_PHASE="$tag" \
      NODE_OPTIONS="--max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons --import=$scratch/witness.mjs" \
      unshare -Urn timeout 90s node ../../node_modules/vitest/vitest.mjs run test/link/message.test.ts --pool=forks --maxWorkers=1 \
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
run original -t '^the text codec ' || { failures original; exit 1; }
jq -e '.numPassedTests==12 and .numFailedTests==0 and .numPendingTests>0 and .numTodoTests==0 and
  (.testResults|length)==1 and ([.testResults[].assertionResults[]|select(.status=="passed")|.fullName]|unique|length)==12' \
  "$scratch/original.result.json" > "$scratch/result.check" || { cat "$scratch/original.log" >&2; jq 'del(.testResults)' "$scratch/original.result.json" >&2; exit 1; }
jq '[.testResults[].assertionResults[]|select(.status=="passed")|.fullName]' "$scratch/original.result.json" > "$scratch/names.json"
file_unselected=$(jq -er '.numPendingTests' "$scratch/original.result.json")
target_unselected=$((file_unselected + 11))

selector="the text codec leaves the old-work flag's value to the message schema: only true passes"
run witness -t "$selector" || { failures witness; exit 1; }
jq -e --arg name "$selector" --argjson unselected "$target_unselected" '.numFailedTests==0 and .numPassedTests==1 and .numPendingTests==$unselected and .numTodoTests==0 and
  ([.testResults[].assertionResults[]|select(.status=="passed")|.fullName]==[$name])' \
  "$scratch/witness.result.json" > "$scratch/witness.check" || { failures witness; exit 1; }

# Widen the scratch schema by one literal, leaving decoding and the test intact.
cp -- "$scratch/$message" "$scratch/original-message.ts"
[[ $(grep -Fc 'old: opt(lit(true)),' "$scratch/$message") == 1 ]]
sed -i 's/old: opt(lit(true)),/old: opt(oneOf(true, 1)),/' "$scratch/$message"
if run control -t "$selector"; then
  echo 'The widened scratch message schema unexpectedly passed' >&2
  exit 1
else
  [[ $? == 1 ]] || { failures control; exit 1; }
fi
mv -- "$scratch/original-message.ts" "$scratch/$message"
flock -u 9
jq -e --arg name "$selector" --argjson unselected "$target_unselected" '.numFailedTests==1 and .numPassedTests==0 and .numPendingTests==$unselected and .numTodoTests==0 and
  ([.testResults[].assertionResults[]|select(.status=="failed")|.fullName]==[$name]) and
  any(.testResults[].assertionResults[]|select(.status=="failed")|.failureMessages[]; contains("message.test.ts:688:"))' \
  "$scratch/control.result.json" > "$scratch/control.check" || { failures control; exit 1; }

for tag in original witness control; do
  jq -es --arg validator "$validator" --arg message "$message" '
    ([.[] | select(.kind=="source-evaluated")|[.file,.export]] | sort)==([[$validator,"compile"],[$message,"checkMessage"]]|sort)
  ' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.modules.check" || { cat "$scratch/$tag.trace.jsonl" >&2; exit 1; }
  jq -s '[.[]|select(.kind!="source-evaluated")]' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.checks.json"
done
jq -e 'length==0' "$scratch/original.checks.json" > "$scratch/original.decisions.check"
verify_decision() {
  local tag=$1 valid=$2
  jq -e --argjson valid "$valid" '.==[{kind:"schema-decision",api:"pi-schema compile().check",decoded:{kind:"call",call:0,hold:1,method:2,old:1},valid:$valid}]' \
    "$scratch/$tag.checks.json" > "$scratch/$tag.decision.check" || { cat "$scratch/$tag.checks.json" >&2; return 1; }
}
verify_decision witness false
verify_decision control true

jq -n --arg pin "$pin" --arg base "$base" --arg test "$test" --arg blob "$blob" --arg testSha "$(sha256 "$scratch/$test")" \
  --arg configSha "$(sha256 "$scratch/$config")" --arg node "$(node --version)" --arg selector "$selector" \
  --arg platform "$(node -p 'process.platform + "/" + process.arch')" --arg vitest "$vitest" \
  --arg integrity "$integrity" --arg runnerSha "$(sha256 "$archive")" --arg evaluatorSha "$(sha256 "$scratch/$key/dist/module-evaluator.js")" \
  --arg validator "$validator" --arg validatorSha "$(sha256 "$scratch/$validator")" --arg messageSha "$(sha256 "$scratch/$message")" \
  --argjson fileUnselected "$file_unselected" --argjson targetUnselected "$target_unselected" \
  --slurpfile names "$scratch/names.json" --slurpfile copied "$scratch/copied.json" --slurpfile modules "$scratch/original.trace.jsonl" \
  --slurpfile refs "$scratch/refs.jsonl" --slurpfile witness "$scratch/witness.checks.json" --slurpfile control "$scratch/control.checks.json" '
  {format:1,pin:$pin,evidenceBase:$base,node:$node,platform:$platform,configSha256:$configSha,
   original:{file:$test,blob:$blob,sha256:$testSha,selection:"the text codec",passed:12,failed:0,unselected:$fileUnselected,todo:0,caseNames:$names[0]},
   runner:{vitest:$vitest,integrity:$integrity,archiveSha256:$runnerSha,evaluatorSha256:$evaluatorSha,copiedDependencies:$copied[0]},
   primaryReferences:$refs, observedModules:($modules | sort_by(.file) | map(. + {sourceSha256:(if .file==$validator then $validatorSha else $messageSha end)})),
   witness:{case:$selector,passed:1,unselected:$targetUnselected,decision:$witness[0][0]},
   control:{change:"allow-numeric-one-as-old-in-scratch-message-schema",case:$selector,failed:1,unselected:$targetUnselected,decision:$control[0][0]}}'
