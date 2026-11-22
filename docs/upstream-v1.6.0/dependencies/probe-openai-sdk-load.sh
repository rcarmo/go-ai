#!/usr/bin/env bash
set -euo pipefail

if [[ $# != 3 ]]; then
  printf 'Usage: bash %s <pinned-git-mirror> <oracle-dir> <retained-archive-dir>\n' "$0" >&2
  exit 1
fi
here=$(cd -- "$(dirname -- "$0")" && pwd -P)
mirror=$(realpath -- "$1")
oracle=$(realpath -- "$2")
archives=$(realpath -- "$3")
pin=faac585884ae2c013a6b1400f3e9e77f10b34bac
base=c188d0d208b7cfafbb5618b55ccdba4a7aa46722
receipt=docs/upstream-v1.6.0/dependencies/provider-archives.jsonl
key='node_modules/openai'
member='package/index.mjs'
entry="$key/${member#package/}"
test='test/openai-responses-mid-convo-effort.test.ts'
selector='keeps reasoning.effort constant and only appends input when the level changes'
full_name="OpenAI Responses configuration_update $selector"
scratch=$(mktemp -d /tmp/v160-openai-sdk-load.XXXXXX)
trap 'rm -r -- "$scratch"' EXIT
mkdir -p "$scratch/packages" "$scratch/home" "$scratch/tmp"

# Bind the archive to the official lock and the earlier acquisition receipt.
git -C "$mirror" show "$pin:package-lock.json" > "$scratch/lock.json"
git -C "$here" show "$base:$receipt" > "$scratch/receipt.jsonl"
lock_sha=$(sha256sum "$scratch/lock.json" | cut -d' ' -f1)
jq -e --arg pin "$pin" --arg sha "$lock_sha" 'select(.kind=="manifest") | .pin==$pin and .lockSha256==$sha' "$scratch/receipt.jsonl" > "$scratch/receipt-pin.check"
read -r version url integrity < <(jq -er --arg key "$key" '.packages[$key] | [.version,.resolved,.integrity] | @tsv' "$scratch/lock.json")
[[ "$version" == 7.59.0 && "$url" == 'https://registry.npmjs.org/openai/-/openai-7.59.0.tgz' ]]
archive="$archives/${url##*/}"
archive_sha=$(sha256sum "$archive" | cut -d' ' -f1)
jq -e --arg key "$key" --arg sha "$archive_sha" --arg integrity "$integrity" --arg url "$url" 'select(.kind=="archive" and .lockKey==$key) | .state=="verified-archive-bytes" and .sha256==$sha and .integrity==$integrity and .url==$url' "$scratch/receipt.jsonl" > "$scratch/archive-receipt.check"
[[ "sha512-$(openssl dgst -sha512 -binary "$archive" | openssl base64 -A)" == "$integrity" ]]
tar -xOzf "$archive" -- "$member" > "$scratch/archive-module"
member_sha=$(sha256sum "$scratch/archive-module" | cut -d' ' -f1)
member_bytes=$(wc -c < "$scratch/archive-module")

# Stage only the pinned source subset and host-compatible placements reachable from
# Vitest and this adapter's external roots. No generated data or whole install clone.
git -C "$mirror" archive "$pin" package.json packages/ai/package.json packages/ai/vitest.config.ts \
  packages/ai/src "packages/ai/$test" packages/schema/{package.json,src} packages/telemetry/{package.json,src} \
  | tar -x -C "$scratch"
git -C "$here" show "$base:docs/upstream-v1.6.0/dependencies/npm-closure.jsonl" > "$scratch/closure.jsonl"
libc=$(node -p "process.report.getReport().header.glibcVersionRuntime ? 'glibc' : 'musl'")
bun run "$here/copy-openai-probe-deps.ts" "$scratch/closure.jsonl" "$lock_sha" "$oracle" "$scratch" "$libc" > "$scratch/copied.json"
mkdir -p "$scratch/node_modules/@earendil-works"
ln -s ../../packages/schema "$scratch/node_modules/@earendil-works/pi-schema"
ln -s ../../packages/telemetry "$scratch/node_modules/@earendil-works/pi-telemetry"
cp "$here/sdk-load-witness.mjs" "$scratch/witness.mjs"
[[ "$(realpath -- "$scratch/$entry")" == "$scratch/$entry" ]]
cmp "$scratch/archive-module" "$scratch/$entry"
jq -e --arg version "$version" '.version==$version and .exports["."].default=="./index.mjs"' "$scratch/$key/package.json" > "$scratch/package.check"
node_version=$(node --version)
vitest_version=$(jq -er '.version' "$scratch/node_modules/vitest/package.json")
[[ "$vitest_version" == 4.1.12 ]]

# The unchanged case supplies a stub fetch and dummy key. No network permission or credentials are passed to Node.
exec 8>/tmp/go-ai/v160-focused-verify.lock
flock 8
run_selected() {
  local tag=$1
  if ! (cd "$scratch/packages/ai" && env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" \
      SDK_LOAD_ROOT="$scratch" SDK_LOAD_ENTRY="$entry" SDK_LOAD_TRACE="$scratch/$tag.trace.jsonl" \
      NODE_OPTIONS="--max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons --import=$scratch/witness.mjs" \
      node ../../node_modules/vitest/vitest.mjs run "$test" -t "$selector" --pool=forks --maxWorkers=1 --reporter=json \
      --outputFile "$scratch/$tag.result.json") > "$scratch/$tag.log" 2>&1; then
    cat "$scratch/$tag.log" >&2
    return 1
  fi
  jq -e --arg full "$full_name" '
    .numPassedTests==1 and .numFailedTests==0 and
    ([.testResults[].assertionResults[] | select(.status=="passed") | .fullName]==[$full])
  ' "$scratch/$tag.result.json" > "$scratch/$tag.pass.check"
}
verify_witness() {
  local tag=$1
  if ! jq -es --arg entry "$entry" 'length==1 and .[0].entry==$entry and .[0].format=="module"' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.load.check"; then
    printf 'Expected one observed SDK ESM load\n' >&2
    return 1
  fi
  if ! jq -e --arg sha "$member_sha" --argjson bytes "$member_bytes" '.sha256==$sha and .bytes==$bytes' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.digest.check"; then
    printf 'Loaded SDK bytes differ from the pinned archive member\n' >&2
    return 13
  fi
}
run_selected original
verify_witness original

# The source assertion must still pass with a legal JS byte mutation. Provenance must reject the same load.
printf '\n' >> "$scratch/$entry"
run_selected mutated
mutation_result=0
verify_witness mutated || mutation_result=$?
[[ "$mutation_result" == 13 ]]
mutated_sha=$(sha256sum "$scratch/$entry" | cut -d' ' -f1)
jq -e --arg sha "$mutated_sha" --argjson n "$((member_bytes + 1))" '.sha256==$sha and .bytes==$n' "$scratch/mutated.trace.jsonl" > "$scratch/mutation.check"
flock -u 8

git -C "$mirror" show "$pin:packages/ai/$test" | cmp "$scratch/packages/ai/$test" -
test_sha=$(sha256sum "$scratch/packages/ai/$test" | cut -d' ' -f1)
jq -n --arg pin "$pin" --arg base "$base" --arg node "$node_version" --arg vitest "$vitest_version" \
  --arg test "packages/ai/$test" --arg testSha "$test_sha" --arg selector "$selector" --arg full "$full_name" \
  --arg url "$url" --arg integrity "$integrity" --arg archiveSha "$archive_sha" --arg member "$member" \
  --slurpfile copied "$scratch/copied.json" --slurpfile original "$scratch/original.trace.jsonl" --slurpfile mutated "$scratch/mutated.trace.jsonl" '
  {format:1,pin:$pin,evidenceBase:$base,node:$node,vitest:$vitest,
    selectedOriginal:{test:$test,sha256:$testSha,selector:$selector,fullName:$full,passed:1},
    archive:{url:$url,integrity:$integrity,sha256:$archiveSha,member:$member},loaded:$original[0],
    control:{change:"append-one-LF-to-copied-SDK-entry",selectedOriginalPassed:1,loaded:$mutated[0],result:"archive-digest-rejected"},
    copiedDependencies:$copied[0],scope:"new-focused-run-only"}'
