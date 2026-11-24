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

[[ $(uname -s)/$(uname -m) == Linux/aarch64 ]] || { echo 'This receipt needs Linux arm64' >&2; exit 1; }
pin=faac585884ae2c013a6b1400f3e9e77f10b34bac
base=04a700eb4fba36df946354a6993ec173e8bfc509
test=packages/chord/test/contracts/schema.test.ts
config=packages/chord/vitest.config.ts
checker=packages/schema/src/checks.ts
validator=packages/schema/src/validator.ts
compiler=node_modules/@typescript/typescript-linux-arm64/lib/tsc
deps="$here/../dependencies"
scratch=$(mktemp -d "$TMPDIR/chord-contract-schema.XXXXXX")
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
jq -e --arg pin "$pin" --arg sha "$lock_sha" 'select(.kind=="manifest") | .pin==$pin and .inputs["package-lock.json"]==$sha' \
  "$deps/npm-closure.jsonl" > "$scratch/lock.check"
git -C "$mirror" archive "$pin" package.json packages/{chord,schema,telemetry}/{src,package.json} "$test" "$config" \
  packages/chord/test/support.ts | tar -x -C "$scratch"
for source in packages/ai/src/utils/validation.ts packages/durable/src/harness/tool.ts; do
  git -C "$mirror" show "$pin:$source" > "$scratch/ref.ts"
  line=$(grep -nF 'from "@earendil-works/pi-schema";' "$scratch/ref.ts" | cut -d: -f1)
  jq -nc --arg file "$source" --arg sha "$(sha256 "$scratch/ref.ts")" --argjson line "$line" \
    '{file:$file,sourceSha256:$sha,line:$line,import:"@earendil-works/pi-schema"}'
done > "$scratch/refs.jsonl"
libc=$(node -p "process.report.getReport().header.glibcVersionRuntime ? 'glibc' : 'musl'")
bun run "$deps/copy-openai-probe-deps.ts" "$deps/npm-closure.jsonl" "$lock_sha" "$oracle" "$scratch" "$libc" \
  node_modules/vitest > "$scratch/copied.json"
mkdir -p "$scratch/node_modules/@earendil-works"
for p in pi-schema pi-telemetry; do ln -s "../../packages/${p#pi-}" "$scratch/node_modules/@earendil-works/$p"; done

# Extract the exact compiler packages, and compare the retained runner files
# at the launch/evaluation seams against their compressed lock-verified bytes.
verify_locked() {
  local key=$1 member=$2 version integrity hex archive
  read -r version integrity < <(jq -er --arg key "$key" '.packages[$key] | [.version,.integrity] | @tsv' "$scratch/lock.json")
  [[ $integrity == sha512-* ]]
  hex=$(printf '%s' "${integrity#sha512-}" | base64 -d | od -An -v -tx1 | tr -d ' \n')
  [[ $hex =~ ^[0-9a-f]{128}$ ]]
  archive="$cache/content-v2/sha512/${hex:0:2}/${hex:2:2}/${hex:4}"
  [[ sha512-$(openssl dgst -sha512 -binary "$archive" | openssl base64 -A) == "$integrity" ]]
  if [[ $key == node_modules/vitest ]]; then
    [[ $version == 4.1.12 ]]
    for member in vitest.mjs dist/cli.js dist/module-evaluator.js; do
      tar -xOzf "$archive" -- "package/$member" | cmp - "$scratch/$key/$member"
    done
  else
    [[ $version == 7.0.2 ]]
    mkdir -p "$scratch/$key"
    tar -xzf "$archive" -C "$scratch/$key" --strip-components=1
    cmp "$scratch/$key/$member" "$oracle/$key/$member"
  fi
  jq -nc --arg key "$key" --arg version "$version" --arg sha "$(sha256 "$archive")" --arg integrity "$integrity" \
    '{package:$key,version:$version,archiveSha256:$sha,integrity:$integrity}'
}
{
  verify_locked node_modules/vitest vitest.mjs
  verify_locked node_modules/typescript bin/tsc
  verify_locked node_modules/@typescript/typescript-linux-arm64 lib/tsc
} > "$scratch/archives.jsonl"
cp -- "$here/chord-contract-schema-witness.mjs" "$scratch/witness.mjs"

# The unchanged declaration test creates a symlink, which Node's restricted
# filesystem mode refuses. The process and its compiler get no network via
# the kernel namespace, and all their inputs are staged in this scratch tree.
run() {
  local tag=$1
  shift
  (
    cd "$scratch/packages/chord"
    env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" CHORD_SCHEMA_PROBE_ROOT="$scratch" \
      CHORD_SCHEMA_PROBE_TRACE="$scratch/$tag.trace.jsonl" \
      NODE_OPTIONS="--max-old-space-size=768 --import=$scratch/witness.mjs" \
      unshare -Urn timeout 90s node ../../node_modules/vitest/vitest.mjs run test/contracts/schema.test.ts --pool=forks --maxWorkers=1 \
        --reporter=json --outputFile "$scratch/$tag.result.json" "$@"
  ) > "$scratch/$tag.log" 2>&1
}
exec 9>/tmp/go-ai/v160-focused-verify.lock
flock 9
run original || {
  cat "$scratch/original.log" >&2
  if [[ -s $scratch/original.result.json ]]; then
    jq '[.testResults[].assertionResults[] | select(.status=="failed") | {fullName,failureMessages}]' "$scratch/original.result.json" >&2
  fi
  exit 1
}
jq -e '.numPassedTests==9 and .numFailedTests==0 and .numPendingTests==0 and .numTodoTests==0 and
  (.testResults|length)==1 and ([.testResults[].assertionResults[]|select(.status=="passed")|.fullName]|unique|length)==9' \
  "$scratch/original.result.json" > "$scratch/result.check" || { cat "$scratch/original.log" >&2; exit 1; }
jq '[.testResults[].assertionResults[]|.fullName]' "$scratch/original.result.json" > "$scratch/names.json"

selector='bytes checks instanceof Uint8Array and the length bounds'
cp -- "$scratch/$checker" "$scratch/original-checks.ts"
[[ $(grep -Fc 'v instanceof Uint8Array && v.length >= min && v.length <= max;' "$scratch/$checker") == 1 ]]
sed -i 's/v instanceof Uint8Array \&\& v.length >= min \&\& v.length <= max;/v instanceof Uint8Array \&\& v.length >= min;/' "$scratch/$checker"
if run control -t "$selector"; then
  echo 'The missing byte bound unexpectedly passed' >&2
  exit 1
else
  [[ $? == 1 ]] || { cat "$scratch/control.log" >&2; exit 1; }
fi
mv -- "$scratch/original-checks.ts" "$scratch/$checker"
flock -u 9
jq -e --arg name "$selector" '.numFailedTests==1 and .numPassedTests==0 and
  ([.testResults[].assertionResults[]|select(.status=="failed")|.fullName]==[$name]) and
  any(.testResults[].assertionResults[]|select(.status=="failed")|.failureMessages[]; contains("expected true to be false"))' \
  "$scratch/control.result.json" > "$scratch/control.check" || { cat "$scratch/control.log" >&2; exit 1; }

# Verify the native compiler path even if the Node permission model refused
# execve and the unchanged tsc launcher used its execFileSync fallback.
jq -es --arg native "$compiler" --arg sha "$(sha256 "$scratch/$compiler")" --arg checker "$checker" --arg validator "$validator" '
  ([.[] | select(.kind=="source-evaluated")|[.file,.export]] | sort)==([[$checker,"bytesCheck"],[$validator,"compile"]]|sort) and
  ([.[] | select(.kind=="declaration")]|length)==1 and
  any(.[]; .kind=="declaration" and .status==0 and .mentionsMethod and (.leaksNodeModules|not) and
    .config.compilerOptions.emitDeclarationOnly and .config.compilerOptions.customConditions==["source"]) and
  ([.[]|select(.kind=="compiler-executable")]|last|.file==$native and .sha256==$sha)
' "$scratch/original.trace.jsonl" > "$scratch/trace.check" || { cat "$scratch/original.trace.jsonl" >&2; exit 1; }
jq -s '{modules:([.[]|select(.kind=="source-evaluated")]|sort_by(.file)),compiler:([.[]|select(.kind=="compiler-executable")]|last),
  declaration:([.[]|select(.kind=="declaration")]|first)}' "$scratch/original.trace.jsonl" > "$scratch/observed.json"

jq -n --arg pin "$pin" --arg base "$base" --arg test "$test" --arg blob "$blob" --arg testSha "$(sha256 "$scratch/$test")" \
  --arg configSha "$(sha256 "$scratch/$config")" --arg node "$(node --version)" --arg checker "$checker" \
  --arg checkerSha "$(sha256 "$scratch/$checker")" --arg validatorSha "$(sha256 "$scratch/$validator")" --arg control "$selector" \
  --slurpfile names "$scratch/names.json" --slurpfile archives "$scratch/archives.jsonl" --slurpfile copied "$scratch/copied.json" \
  --slurpfile refs "$scratch/refs.jsonl" --slurpfile observed "$scratch/observed.json" '
  {format:1,pin:$pin,evidenceBase:$base,node:$node,configSha256:$configSha,
   original:{file:$test,blob:$blob,sha256:$testSha,selection:"whole-file",passed:9,failed:0,skipped:0,todo:0,caseNames:$names[0]},
   archives:$archives,copiedDependencies:$copied[0],primaryReferences:$refs,
   observed:($observed[0] | .modules |= map(. + {sourceSha256:(if .file==$checker then $checkerSha else $validatorSha end)})),
   control:{change:"remove-scratch-Uint8Array-maximum-length-check",case:$control,failed:1}}'
