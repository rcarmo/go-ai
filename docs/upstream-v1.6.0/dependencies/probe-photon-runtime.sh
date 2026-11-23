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
test=packages/durable/test/images-fit.test.ts
selector='fitImages scales an image beyond the limits down, keeps the text in place, and says how to map coordinates back'
key=node_modules/@silvia-odwyer/photon
js=photon_rs.js
wasm=photon_rs_bg.wasm
scratch=$(mktemp -d "$TMPDIR/photon-runtime.XXXXXX")
trap 'rm -r -- "$scratch"' EXIT
mkdir -p "$scratch/home" "$scratch/tmp"
sha256() { sha256sum -- "$1" | cut -d' ' -f1; }

# Verify compressed Photon bytes against the lock and the retained receipt.
git -C "$mirror" show "$pin:package-lock.json" > "$scratch/lock.json"
lock_sha=$(sha256 "$scratch/lock.json")
jq -e --arg pin "$pin" --arg sha "$lock_sha" 'select(.kind=="manifest") | .pin==$pin and .lockSha256==$sha' \
  "$here/npm-archives.jsonl" > "$scratch/lock.check"
read -r version url integrity < <(jq -er --arg key "$key" '.packages[$key] | [.version,.resolved,.integrity] | @tsv' "$scratch/lock.json")
[[ $version == 0.3.3 && $url == 'https://registry.npmjs.org/@silvia-odwyer/photon/-/photon-0.3.3.tgz' ]]
read -r cache_key archive_sha < <(jq -er --arg key "$key" --arg url "$url" --arg integrity "$integrity" '
  select(.kind=="archive" and .lockKey==$key and .state=="verified-archive-bytes" and .resolved==$url and .integrity==$integrity)
  | [.archive[] | select(.cache=="user") | [.key,.sha256]] | if length==1 then .[0] | @tsv else error("no unique user cache record") end
' "$here/npm-archives.jsonl")
[[ $cache_key =~ ^content-v2/sha512/[0-9a-f]{2}/[0-9a-f]{2}/[0-9a-f]{124}$ ]]
archive="$cache/$cache_key"
[[ $(sha256 "$archive") == "$archive_sha" ]]
[[ sha512-$(openssl dgst -sha512 -binary "$archive" | openssl base64 -A) == "$integrity" ]]
for member in "$js" "$wasm"; do tar -xOzf "$archive" -- "package/$member" > "$scratch/$member"; done

# The original imports the published AI entry point; stage that verified dist.
git -C "$mirror" archive "$pin" package.json packages/{durable,chord,schema,telemetry,codemode,ai}/{package.json,src} \
  packages/durable/vitest.config.ts packages/durable/test | tar -x -C "$scratch"
read -r ai_file ai_sha < <(jq -er --arg pin "$pin" --arg sha "$lock_sha" '
  select(.kind=="manifest" and .pin==$pin and .lockSha256==$sha) | .archive | [.file,.sha256] | @tsv
' "$here/provider-catalog-assets.jsonl")
[[ $ai_file == earendil-works-pi-ai-1.6.0.tgz && $(sha256 "$releases/$ai_file") == "$ai_sha" ]]
tar -xzf "$releases/$ai_file" -C "$scratch/packages/ai" --strip-components=1 package/dist
libc=$(node -p "process.report.getReport().header.glibcVersionRuntime ? 'glibc' : 'musl'")
bun run "$here/copy-openai-probe-deps.ts" "$here/npm-closure.jsonl" "$lock_sha" "$oracle" "$scratch" "$libc" \
  node_modules/vitest node_modules/diff "$key" node_modules/quickjs-wasi node_modules/partial-json > "$scratch/copied.json"
mkdir -p "$scratch/node_modules/@earendil-works"
for p in chord pi-schema pi-telemetry pi-codemode pi-ai pi-durable; do
  ln -s "../../packages/${p#pi-}" "$scratch/node_modules/@earendil-works/$p"
done
cp -- "$here/photon-runtime-witness.mjs" "$scratch/witness.mjs"
for member in "$js" "$wasm"; do cmp "$scratch/$member" "$scratch/$key/$member"; done
vitest=$(jq -er '.version' "$scratch/node_modules/vitest/package.json")
[[ $vitest == 4.1.12 ]]

run_selected() {
  local tag=$1
  (
    cd "$scratch/packages/durable"
    env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" PHOTON_PROBE_ROOT="$scratch" \
      PHOTON_RUNTIME_TRACE="$scratch/$tag.trace.jsonl" \
      NODE_OPTIONS="--max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons --import=$scratch/witness.mjs" \
      node ../../node_modules/vitest/vitest.mjs run test/images-fit.test.ts -t "$selector" --pool=forks --maxWorkers=1 \
      --reporter=json --outputFile "$scratch/$tag.result.json"
  ) > "$scratch/$tag.log" 2>&1 || { cat "$scratch/$tag.log" >&2; return 1; }
  jq -e --arg full "$selector" '.numPassedTests==1 and .numFailedTests==0 and
    ([.testResults[].assertionResults[]|select(.status=="passed")|.fullName]==[$full])' \
    "$scratch/$tag.result.json" > "$scratch/$tag.test.check"
  # Validate and normalise per-thread event order while ignoring which thread
  # reached the trace first. Require the actual reply, ruling out fallback.
  jq -es '
    ([.[]|select(.thread=="test-thread")|.kind]==["instantiate-wasm"]) and
    ([.[]|select(.thread=="image-worker")|.kind]==["load-js","instantiate-wasm","worker-reply"]) and
    length==4 and
    ([.[]|select(.kind=="worker-reply")|{id,mimeType,resized}]==[
      {id:0,mimeType:"image/png",resized:{from:{width:400,height:100},to:{width:200,height:50}}}
    ])' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.events.check" || { cat "$scratch/$tag.trace.jsonl" >&2; return 1; }
  jq -s 'sort_by(.thread)' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.events.json"
}

# Check both loaded JS and instantiated WASM against the actual compared files.
verify_asset() {
  local tag=$1 kind=$2 file=$3 count=2
  if [[ $kind == load-js ]]; then count=1; fi # Node sees the worker ESM load, not Vitest's test-thread load.
  jq -e --arg kind "$kind" --arg sha "$(sha256 "$file")" --argjson bytes "$(wc -c < "$file")" --argjson count "$count" '
    [.[]|select(.kind==$kind)] as $events | ($events|length)==$count and all($events[]; .sha256==$sha and .bytes==$bytes)
  ' "$scratch/$tag.events.json"
}

exec 9>/tmp/go-ai/v160-focused-verify.lock
flock 9
run_selected original
verify_asset original load-js "$scratch/$js" > "$scratch/js.check"
verify_asset original instantiate-wasm "$scratch/$wasm" > "$scratch/wasm.check"

# Keep behavior intact while changing both provenance inputs in scratch.
printf '\n// Photon provenance control.\n' >> "$scratch/$key/$js"
printf '\000\007\005audit\000' >> "$scratch/$key/$wasm"
run_selected control
flock -u 9
for entry in "load-js:$js" "instantiate-wasm:$wasm"; do
  kind=${entry%:*} member=${entry#*:}
  verify_asset control "$kind" "$scratch/$key/$member" > "$scratch/$kind.control-input.check"
  if verify_asset control "$kind" "$scratch/$member" > "$scratch/$kind.control-archive.check"; then
    printf 'Control unexpectedly matched archive %s\n' "$member" >&2
    exit 1
  else
    [[ $? == 1 ]] # jq returned false, not a parsing or invocation error.
  fi
done

jq -n --arg pin "$pin" --arg test "$test" --arg testSha "$(sha256 "$scratch/$test")" --arg selector "$selector" \
  --arg node "$(node --version)" --arg vitest "$vitest" --arg url "$url" --arg integrity "$integrity" \
  --arg archiveSha "$archive_sha" --arg aiSha "$ai_sha" \
  --slurpfile deps "$scratch/copied.json" --slurpfile observed "$scratch/original.events.json" \
  --slurpfile control "$scratch/control.events.json" '
  {format:1,pin:$pin,node:$node,vitest:$vitest,
    original:{test:$test,sha256:$testSha,selector:$selector,passed:1},
    photonArchive:{url:$url,integrity:$integrity,sha256:$archiveSha},aiArchiveSha256:$aiSha,
    observed:$observed[0],control:{changed:["glue-comment","wasm-custom-section"],originalPassed:1,
      observed:$control[0],comparisons:"both-archive-checks-rejected"},copiedDependencies:$deps[0]}'
