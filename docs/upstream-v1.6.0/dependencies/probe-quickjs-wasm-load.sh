#!/usr/bin/env bash
set -euo pipefail

if [[ $# != 3 ]]; then
  printf 'Usage: bash %s <pinned-git-mirror> <oracle-dir> <user-npm-cacache>\n' "$0" >&2
  exit 1
fi
here=$(cd -- "$(dirname -- "$0")" && pwd -P)
mirror=$(realpath -- "$1")
oracle=$(realpath -- "$2")
cache=$(realpath -- "$3")
pin=faac585884ae2c013a6b1400f3e9e77f10b34bac
base=10a9f34c60549069026e02930279f9fdf5228a7f
catalogue=docs/upstream-v1.6.0/dependencies
key=node_modules/quickjs-wasi
member=package/quickjs.wasm
entry="$key/${member#package/}"
test=test/sandbox.test.ts
selector="inline backend script execution returns the script's return value after a JSON round trip"
source=packages/codemode/src/wasm.ts
native_asset=internal/quickjs/guest/quickjs.wasm
scratch=$(mktemp -d /tmp/v160-quickjs-wasm-load.XXXXXX)
trap 'rm -r -- "$scratch"' EXIT
mkdir -p "$scratch/home" "$scratch/tmp"

# Verify the retained compressed archive before inspecting any member.
git -C "$mirror" show "$pin:package-lock.json" > "$scratch/lock.json"
git -C "$here" show "$base:$catalogue/npm-archives.jsonl" > "$scratch/receipt.jsonl"
lock_sha=$(sha256sum "$scratch/lock.json" | cut -d' ' -f1)
jq -e --arg pin "$pin" --arg sha "$lock_sha" 'select(.kind=="manifest") | .pin==$pin and .lockSha256==$sha' "$scratch/receipt.jsonl" > "$scratch/receipt-pin.check"
read -r version url integrity < <(jq -er --arg key "$key" '.packages[$key] | [.version,.resolved,.integrity] | @tsv' "$scratch/lock.json")
[[ "$version" == 3.6.2 && "$url" == 'https://registry.npmjs.org/quickjs-wasi/-/quickjs-wasi-3.6.2.tgz' ]]
read -r cache_key archive_sha < <(jq -er --arg key "$key" --arg integrity "$integrity" --arg url "$url" '
  select(.kind=="archive" and .lockKey==$key and .state=="verified-archive-bytes" and .integrity==$integrity and .resolved==$url)
  | [.archive[] | select(.cache=="user") | [.key,.sha256]] | if length==1 then .[0] | @tsv else error("no unique user cache record") end
' "$scratch/receipt.jsonl")
[[ "$cache_key" =~ ^content-v2/sha512/[0-9a-f]{2}/[0-9a-f]{2}/[0-9a-f]{124}$ ]]
archive="$cache/$cache_key"
[[ "$(sha256sum "$archive" | cut -d' ' -f1)" == "$archive_sha" ]]
[[ "sha512-$(openssl dgst -sha512 -binary "$archive" | openssl base64 -A)" == "$integrity" ]]
tar -xOzf "$archive" -- "$member" > "$scratch/archive-wasm"
member_sha=$(sha256sum "$scratch/archive-wasm" | cut -d' ' -f1)
member_bytes=$(wc -c < "$scratch/archive-wasm")
git -C "$here" show "$base:$native_asset" | cmp "$scratch/archive-wasm" -

# Stage exact source and only the local Vitest/QuickJS closure in disposable scratch.
git -C "$mirror" archive "$pin" package.json packages/codemode/{package.json,vitest.config.ts,src} "packages/codemode/$test" | tar -x -C "$scratch"
git -C "$here" show "$base:$catalogue/npm-closure.jsonl" > "$scratch/closure.jsonl"
libc=$(node -p "process.report.getReport().header.glibcVersionRuntime ? 'glibc' : 'musl'")
bun run "$here/copy-openai-probe-deps.ts" "$scratch/closure.jsonl" "$lock_sha" "$oracle" "$scratch" "$libc" node_modules/vitest "$key" > "$scratch/copied.json"
cp "$here/wasm-compile-witness.mjs" "$scratch/witness.mjs"
[[ "$(realpath -- "$scratch/$entry")" == "$scratch/$entry" ]]
cmp "$scratch/archive-wasm" "$scratch/$entry"
node_version=$(node --version)
vitest_version=$(jq -er '.version' "$scratch/node_modules/vitest/package.json")
[[ "$vitest_version" == 4.1.12 ]]

exec 8>/tmp/go-ai/v160-focused-verify.lock
flock 8
run_selected() {
  local tag=$1
  if ! (cd "$scratch/packages/codemode" && env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" \
      WASM_LOAD_TRACE="$scratch/$tag.trace.jsonl" \
      NODE_OPTIONS="--max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons --allow-wasi --import=$scratch/witness.mjs" \
      node ../../node_modules/vitest/vitest.mjs run "$test" -t "$selector" --pool=forks --maxWorkers=1 --reporter=json \
      --outputFile "$scratch/$tag.result.json") > "$scratch/$tag.log" 2>&1; then
    cat "$scratch/$tag.log" >&2
    return 1
  fi
  jq -e --arg full "$selector" '
    .numPassedTests==1 and .numFailedTests==0 and
    ([.testResults[].assertionResults[] | select(.status=="passed") | .fullName]==[$full])
  ' "$scratch/$tag.result.json" > "$scratch/$tag.pass.check"
}
verify_witness() {
  local tag=$1
  if ! jq -es --arg source "$source" 'length==1 and .[0].source==$source and .[0].api=="WebAssembly.compile"' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.load.check"; then
    printf 'Expected one successful compile at the pinned QuickJS loader seam\n' >&2
    return 1
  fi
  if ! jq -e --arg sha "$member_sha" --argjson bytes "$member_bytes" '.sha256==$sha and .bytes==$bytes' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.digest.check"; then
    printf 'Compiled WASM bytes differ from the pinned archive member\n' >&2
    return 13
  fi
}
run_selected original
verify_witness original

# A valid custom section (id 0, payload size 1, empty name) changes bytes without changing execution.
printf '\x00\x01\x00' >> "$scratch/$entry"
run_selected mutated
mutation_result=0
verify_witness mutated || mutation_result=$?
[[ "$mutation_result" == 13 ]]
mutated_sha=$(sha256sum "$scratch/$entry" | cut -d' ' -f1)
jq -e --arg sha "$mutated_sha" --argjson n "$((member_bytes + 3))" '.sha256==$sha and .bytes==$n' "$scratch/mutated.trace.jsonl" > "$scratch/mutation.check"
flock -u 8

git -C "$mirror" show "$pin:packages/codemode/$test" | cmp "$scratch/packages/codemode/$test" -
git -C "$mirror" show "$pin:$source" | cmp "$scratch/$source" -
test_sha=$(sha256sum "$scratch/packages/codemode/$test" | cut -d' ' -f1)
source_sha=$(sha256sum "$scratch/$source" | cut -d' ' -f1)
jq -n --arg pin "$pin" --arg base "$base" --arg node "$node_version" --arg vitest "$vitest_version" \
  --arg test "packages/codemode/$test" --arg testSha "$test_sha" --arg selector "$selector" --arg sourceSha "$source_sha" \
  --arg url "$url" --arg integrity "$integrity" --arg archiveSha "$archive_sha" --arg member "$member" \
  --arg cacheKey "$cache_key" --arg entry "$entry" --arg nativeAsset "$native_asset" --arg memberSha "$member_sha" \
  --slurpfile copied "$scratch/copied.json" --slurpfile original "$scratch/original.trace.jsonl" --slurpfile mutated "$scratch/mutated.trace.jsonl" '
  {format:1,pin:$pin,evidenceBase:$base,node:$node,vitest:$vitest,
    selectedOriginal:{test:$test,sha256:$testSha,selector:$selector,passed:1},
    archive:{url:$url,integrity:$integrity,sha256:$archiveSha,cacheKey:$cacheKey,member:$member},
    compiled:($original[0]+{inputFile:$entry,sourceSha256:$sourceSha}),
    nativeEmbedded:{file:$nativeAsset,sha256:$memberSha,commit:$base,state:"same-bytes-not-a-native-load-trace"},
    control:{change:"append-valid-empty-custom-section-to-copied-wasm",selectedOriginalPassed:1,compiled:$mutated[0],result:"archive-digest-rejected"},
    copiedDependencies:$copied[0],scope:"new-focused-Oracle-inline-run-only"}'
