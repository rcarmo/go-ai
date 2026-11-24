#!/usr/bin/env bash
set -euo pipefail

if [[ $# != 4 ]]; then
  printf 'Usage: bash %s <pinned-git-mirror> <prepared-oracle-dir> <user-npm-cacache> <release-archive-dir>\n' "$0" >&2
  exit 1
fi
mirror=$(realpath -- "$1")
oracle=$(realpath -- "$2")
cache=$(realpath -- "$3")
releases=$(realpath -- "$4")
here=$(cd -- "$(dirname -- "$0")" && pwd -P)
cd "$(git -C "$here" rev-parse --show-toplevel)"
# shellcheck source=/dev/null
source scripts/project-env.sh

pin=faac585884ae2c013a6b1400f3e9e77f10b34bac
base=1f3655a205424ccf7eeb709435666c9658e29bde
test=packages/chord/test/telemetry.test.ts
memory=packages/telemetry/src/memory.ts
renderer=packages/telemetry/src/render.ts
config=packages/chord/vitest.config.ts
deps="$here/../dependencies"
scratch=$(mktemp -d "$TMPDIR/chord-telemetry.XXXXXX")
trap 'rm -r -- "$scratch"' EXIT
mkdir -p "$scratch/home" "$scratch/tmp"
sha256() { sha256sum -- "$1" | cut -d' ' -f1; }

git -C "$here" show "$base:docs/upstream-v1.6.0/test-inventory/files.jsonl" > "$scratch/inventory.jsonl"
blob=$(git -C "$mirror" rev-parse "$pin:$test")
jq -e --arg test "$test" --arg blob "$blob" '
  select(.kind=="path" and .path==$test) | .scope=="shared-dependency" and .role=="suite" and .change=="M" and .pinned.object==$blob
' "$scratch/inventory.jsonl" > "$scratch/inventory.check"
git -C "$mirror" show "$pin:package-lock.json" > "$scratch/lock.json"
lock_sha=$(sha256 "$scratch/lock.json")
for receipt in npm-closure npm-archives; do
  jq -e --arg pin "$pin" --arg sha "$lock_sha" 'select(.kind=="manifest") | .pin==$pin and
    (.lockSha256 // .inputs["package-lock.json"])==$sha' "$deps/$receipt.jsonl" > "$scratch/$receipt.check"
done

git -C "$mirror" archive "$pin" package.json packages/{chord,telemetry,schema}/{src,package.json} "$test" "$config" \
  packages/chord/test/support.ts | tar -x -C "$scratch"
for source in packages/ai/src/telemetry.ts packages/durable/src/harness/telemetry.ts; do
  git -C "$mirror" show "$pin:$source" > "$scratch/primary-reference.ts"
  line=$(grep -nF 'from "@earendil-works/pi-telemetry";' "$scratch/primary-reference.ts" | cut -d: -f1)
  jq -nc --arg file "$source" --arg sha "$(sha256 "$scratch/primary-reference.ts")" --argjson line "$line" \
    '{file:$file,sourceSha256:$sha,line:$line,import:"@earendil-works/pi-telemetry"}'
done > "$scratch/primary-refs.jsonl"
# The pinned config selects workspace source. Keep the published archives and
# metadata as separate evidence; the source run does not load their dist files.
for name in chord pi-telemetry; do
  read -r file digest < <(jq -er --arg name "@earendil-works/$name" '
    select(.kind=="workspace" and .name==$name and .version=="1.6.0") | .archive | [.file,.sha256]|@tsv
  ' "$deps/npm-closure.jsonl")
  [[ $file == "earendil-works-$name-1.6.0.tgz" && $(sha256 "$releases/$file") == "$digest" ]]
  jq -nc --arg file "$file" --arg digest "$digest" '{file:$file,sha256:$digest}'
done > "$scratch/published.jsonl"
libc=$(node -p "process.report.getReport().header.glibcVersionRuntime ? 'glibc' : 'musl'")
bun run "$deps/copy-openai-probe-deps.ts" "$deps/npm-closure.jsonl" "$lock_sha" "$oracle" "$scratch" "$libc" \
  node_modules/vitest > "$scratch/copied.json"
mkdir -p "$scratch/node_modules/@earendil-works"
for p in pi-telemetry pi-schema; do ln -s "../../packages/${p#pi-}" "$scratch/node_modules/@earendil-works/$p"; done

# Match the retained Vitest entry, CLI and witnessed evaluator to its archive.
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
cp -- "$here/chord-telemetry-runtime-witness.mjs" "$scratch/witness.mjs"

run() {
  local tag=$1
  shift
  (
    cd "$scratch/packages/chord"
    env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" CHORD_PROBE_ROOT="$scratch" \
      CHORD_TELEMETRY_TRACE="$scratch/$tag.sqlite.jsonl" CHORD_MODULE_TRACE="$scratch/$tag.modules.jsonl" \
      NODE_OPTIONS="--expose-gc --max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons --import=$scratch/witness.mjs" \
      unshare -Urn timeout 90s node ../../node_modules/vitest/vitest.mjs run test/telemetry.test.ts --pool=forks --maxWorkers=1 \
        --reporter=json --outputFile "$scratch/$tag.result.json" "$@"
  ) > "$scratch/$tag.log" 2>&1
}
exec 9>/tmp/go-ai/v160-focused-verify.lock
flock 9
run original || { cat "$scratch/original.log" >&2; exit 1; }
jq -e '.numPassedTests==23 and .numFailedTests==0 and .numPendingTests==0 and .numTodoTests==0 and
  (.testResults|length)==1 and ([.testResults[].assertionResults[]|select(.status=="passed")|.fullName]|unique|length)==23' \
  "$scratch/original.result.json" > "$scratch/result.check" || { cat "$scratch/original.log" >&2; exit 1; }
jq '[.testResults[].assertionResults[]|.fullName]' "$scratch/original.result.json" > "$scratch/cases.json"
jq -es 'length==3 and all(.[]; .sqlite.version==.nodeReportedVersion) and (unique|length)==1' \
  "$scratch/original.sqlite.jsonl" > "$scratch/sqlite.check"
jq -s '{connections:length,engine:.[0]}' "$scratch/original.sqlite.jsonl" > "$scratch/sqlite.json"
jq -e -s --arg mem "$memory" --arg render "$renderer" '
  length==2 and (map([.file,.export])|sort)==([[$mem,"InMemoryTelemetryContext"],[$render,"renderTrace"]]|sort)
' "$scratch/original.modules.jsonl" > "$scratch/modules.check" || { cat "$scratch/original.modules.jsonl" >&2; exit 1; }
jq -s 'sort_by(.file)' "$scratch/original.modules.jsonl" > "$scratch/modules.json"

# Change the scratch dependency, never the source test, and require the
# same unchanged cross-host assertion to reject the wrong trace tree.
selector="across hosts follows a call from the tab through the server's relay into the worker's method and its commit"
cp -- "$scratch/$renderer" "$scratch/original-render.ts"
[[ $(grep -Fc 'return lines.join("\n");' "$scratch/$renderer") == 1 ]]
sed -i 's/return lines.join("\\n");/return "wrong trace tree";/' "$scratch/$renderer"
if run control -t "$selector"; then
  echo 'The wrong trace tree unexpectedly passed' >&2
  exit 1
else
  [[ $? == 1 ]] || { cat "$scratch/control.log" >&2; exit 1; }
fi
mv -- "$scratch/original-render.ts" "$scratch/$renderer"
flock -u 9
jq -e --arg name "$selector" '.numFailedTests==1 and .numPassedTests==0 and
  ([.testResults[].assertionResults[]|select(.status=="failed")|.fullName]==[$name]) and
  any(.testResults[].assertionResults[]|select(.status=="failed")|.failureMessages[]; contains("wrong trace tree"))' \
  "$scratch/control.result.json" > "$scratch/control.check" || { cat "$scratch/control.log" >&2; exit 1; }

jq -n --arg pin "$pin" --arg base "$base" --arg test "$test" --arg blob "$blob" --arg testSha "$(sha256 "$scratch/$test")" \
  --arg configSha "$(sha256 "$scratch/$config")" --arg node "$(node --version)" --arg vitest "$vitest" \
  --arg runnerIntegrity "$integrity" --arg runnerArchiveSha "$(sha256 "$archive")" --arg evaluatorSha "$(sha256 "$scratch/$key/dist/module-evaluator.js")" \
  --arg memory "$memory" --arg memorySha "$(sha256 "$scratch/$memory")" --arg renderer "$renderer" --arg rendererSha "$(sha256 "$scratch/$renderer")" \
  --arg control "$selector" --slurpfile cases "$scratch/cases.json" --slurpfile sqlite "$scratch/sqlite.json" \
  --slurpfile modules "$scratch/modules.json" --slurpfile packages "$scratch/published.jsonl" --slurpfile copied "$scratch/copied.json" \
  --slurpfile refs "$scratch/primary-refs.jsonl" '
  {format:1,pin:$pin,evidenceBase:$base,node:$node,configSha256:$configSha,
   original:{file:$test,blob:$blob,sha256:$testSha,selection:"whole-file",passed:23,failed:0,skipped:0,todo:0,caseNames:$cases[0]},
   runner:{vitest:$vitest,integrity:$runnerIntegrity,archiveSha256:$runnerArchiveSha,observedEvaluatorSha256:$evaluatorSha,copiedDependencies:$copied[0]},
   observed:{telemetryModules:($modules[0] | map(. + {sourceSha256:(if .file==$memory then $memorySha else $rendererSha end)})),sqlite:$sqlite[0]},
   control:{change:"replace-pinned-rendered-trace-with-wrong-tree-in-scratch",case:$control,failed:1},
   primaryReferences:$refs,publishedComparisonOnly:$packages}'
