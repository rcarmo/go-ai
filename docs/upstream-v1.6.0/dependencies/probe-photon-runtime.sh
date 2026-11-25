#!/usr/bin/env bash
set -euo pipefail

if [[ $# != 4 && ( $# -lt 5 || $# -gt 6 || ( $5 != stable-worker && $5 != fit-resize && $5 != fit-passthrough && $5 != fit-omissions && $5 != fit-declared-type ) ) ]]; then
  printf 'Usage: bash %s <pinned-git-mirror> <prepared-oracle-dir> <user-npm-cacache> <release-archive-dir> [stable-worker|fit-resize|fit-passthrough|fit-omissions|fit-declared-type [new-fixture-path]]\n' "$0" >&2
  exit 1
fi
mirror=$(realpath -- "$1")
oracle=$(realpath -- "$2")
cache=$(realpath -- "$3")
releases=$(realpath -- "$4")
fixture_out=${6:-}
if [[ -n "$fixture_out" && "$fixture_out" != /* ]]; then fixture_out="$PWD/$fixture_out"; fi
here=$(cd -- "$(dirname -- "$0")" && pwd -P)
cd "$(git -C "$here" rev-parse --show-toplevel)"
# shellcheck source=/dev/null
source scripts/project-env.sh

pin=faac585884ae2c013a6b1400f3e9e77f10b34bac
test=packages/durable/test/images-fit.test.ts
selector='fitImages scales an image beyond the limits down, keeps the text in place, and says how to map coordinates back'
mode=${5:-resize}
base=1f3655a205424ccf7eeb709435666c9658e29bde
heap=768
if [[ "$mode" == stable-worker ]]; then
  test=packages/durable/test/tools-images.test.ts
  selector="createNodePhotonImages gives the same result as the calling thread, and keeps the caller's bytes"
  heap=1856 # Vitest deep-compares a multi-MiB Uint8Array.
elif [[ "$mode" == fit-passthrough ]]; then
  selector='fitImages is unchanged when every image comes back as it was, and asks the processor only about images'
elif [[ "$mode" == fit-omissions ]]; then
  selector='fitImages leaves out what cannot be prepared, with a note: damaged images, other bytes, malformed base64'
elif [[ "$mode" == fit-declared-type ]]; then
  selector='fitImages corrects a declared type the bytes contradict, without a note'
fi
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

show_failure() {
  local tag=$1
  cat "$scratch/$tag.log" >&2
  if [[ -f "$scratch/$tag.result.json" ]]; then
    jq -c '[.testResults[]|{message,failed:[.assertionResults[]|select(.status=="failed")|{fullName,failureMessages:(.failureMessages|map(.[0:500]))}]}]' "$scratch/$tag.result.json" >&2
  fi
}

run_test() {
  local tag=$1 fixture=
  if [[ "$mode" != resize && "$mode" != fit-omissions ]]; then fixture="$scratch/$tag.input.png"; fi
  (
    cd "$scratch/packages/durable"
    env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" PHOTON_PROBE_ROOT="$scratch" \
      PHOTON_RUNTIME_TRACE="$scratch/$tag.trace.jsonl" PHOTON_STABLE_FIXTURE="$fixture" \
      NODE_OPTIONS="--max-old-space-size=$heap --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons --import=$scratch/witness.mjs" \
      node ../../node_modules/vitest/vitest.mjs run "${test#packages/durable/}" -t "$selector" --pool=forks --maxWorkers=1 \
      --reporter=json --outputFile "$scratch/$tag.result.json"
  ) > "$scratch/$tag.log" 2>&1
}

run_selected() {
  local tag=$1
  if ! run_test "$tag"; then show_failure "$tag"; return 1; fi
  jq -e --arg full "$selector" '.numPassedTests==1 and .numFailedTests==0 and
    ([.testResults[].assertionResults[]|select(.status=="passed")|.fullName]==[$full])' \
    "$scratch/$tag.result.json" > "$scratch/$tag.test.check"
  # Validate and normalise per-thread event order, ignoring which thread
  # reached the trace first. Require the actual reply, ruling out fallback.
  if [[ "$mode" == stable-worker ]]; then
    jq -e '.numPendingTests==33 and .numTodoTests==0' "$scratch/$tag.result.json" > "$scratch/$tag.pending.check"
    # The calling-thread processor reuses the fixture thread's initialized WASM.
    jq -es --arg sha "$(sha256 "$scratch/$tag.input.png")" --argjson bytes "$(wc -c < "$scratch/$tag.input.png")" '
      ([.[]|select(.thread=="test-thread")|.kind]==["instantiate-wasm"]) and
      ([.[]|select(.thread=="image-worker")|.kind]==["load-js","instantiate-wasm","worker-request","worker-reply"]) and length==5 and
      ([.[]|select(.kind=="worker-request")|del(.thread,.kind)]==[
        {id:0,mimeType:"image/png",limits:{maxWidth:2000,maxHeight:2000,maxBytes:4718592},sha256:$sha,bytes:$bytes}
      ]) and any(.[]; .kind=="worker-reply" and .id==0 and .mimeType=="image/jpeg" and
        .convertedFrom=="image/png" and (has("resized")|not) and .data.bytes>0 and .base64.bytes<=4718592)
    ' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.events.check" || { cat "$scratch/$tag.trace.jsonl" >&2; return 1; }
  elif [[ "$mode" == fit-resize || "$mode" == fit-passthrough || "$mode" == fit-declared-type ]]; then
    local width=200 mime=image/png
    case "$mode" in
      fit-passthrough) width=2000 ;;
      fit-declared-type) width=2000; mime=image/jpeg ;;
    esac
    jq -e '.numPendingTests==10 and .numTodoTests==0' "$scratch/$tag.result.json" > "$scratch/$tag.pending.check"
    jq -es --arg mode "$mode" --argjson width "$width" --arg mime "$mime" \
      --arg sha "$(sha256 "$scratch/$tag.input.png")" --argjson bytes "$(wc -c < "$scratch/$tag.input.png")" '
      ([.[]|select(.thread=="test-thread")|.kind]==["instantiate-wasm"]) and
      ([.[]|select(.thread=="image-worker")|.kind]==["load-js","instantiate-wasm","worker-request","worker-reply"]) and length==5 and
      ([.[]|select(.kind=="worker-request")|del(.thread,.kind)]==[
        {id:0,mimeType:$mime,limits:{maxWidth:$width,maxHeight:2000,maxBytes:4718592},sha256:$sha,bytes:$bytes}
      ]) and any(.[]; .kind=="worker-reply" and .id==0 and .mimeType==$mime and
        (has("convertedFrom")|not) and .base64.bytes<=4718592 and
        (if $mode=="fit-resize" then .resized=={from:{width:400,height:100},to:{width:200,height:50}}
         else (has("resized")|not) and .data=={bytes:$bytes,sha256:$sha} end))
    ' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.events.check" || { cat "$scratch/$tag.trace.jsonl" >&2; return 1; }
  elif [[ "$mode" == fit-omissions ]]; then
    jq -e '.numPendingTests==10 and .numTodoTests==0' "$scratch/$tag.result.json" > "$scratch/$tag.pending.check"
    jq -es '
      ([.[]|select(.thread=="test-thread")|.kind]==["instantiate-wasm"]) and
      ([.[]|select(.thread=="image-worker")|.kind]==["load-js","instantiate-wasm","worker-reply","worker-reply"]) and length==5 and
      ([.[]|select(.kind=="worker-reply")|del(.thread,.kind)]==[{id:0},{id:1}])
    ' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.events.check" || { cat "$scratch/$tag.trace.jsonl" >&2; return 1; }
  else
    jq -es '
    ([.[]|select(.thread=="test-thread")|.kind]==["instantiate-wasm"]) and
    ([.[]|select(.thread=="image-worker")|.kind]==["load-js","instantiate-wasm","worker-reply"]) and
    length==4 and
    ([.[]|select(.kind=="worker-reply")|{id,mimeType,resized}]==[
      {id:0,mimeType:"image/png",resized:{from:{width:400,height:100},to:{width:200,height:50}}}
    ])' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.events.check" || { cat "$scratch/$tag.trace.jsonl" >&2; return 1; }
  fi
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

if [[ "$mode" == fit-resize || "$mode" == fit-passthrough || "$mode" == fit-omissions || "$mode" == fit-declared-type ]]; then
  fixture="$scratch/original.input.png"
  if [[ "$mode" == fit-resize ]]; then
    base=4db6f2123184ab22e0fe6d09b8e3aa40cfa90183
    adapter=packages/durable/src/tools/image-processor.ts
    assertion=111
    change='format the coordinate multiplier to one decimal place'
    scope=offline-fit-resize-and-ordered-notes
    [[ $(grep -Fc '(from.width / to.width).toFixed(2)' "$scratch/$adapter") == 1 ]]
    sed -i 's/(from.width \/ to.width)\.toFixed(2)/(from.width \/ to.width).toFixed(1)/' "$scratch/$adapter"
  elif [[ "$mode" == fit-passthrough ]]; then
    base=0ec852038cec8b94d22b6a8c85b00233a9c10b5a
    adapter=packages/durable/src/images/fit.ts
    assertion=91
    change='start with the changed flag true even when no image changes'
    scope=offline-fit-passthrough
    [[ $(grep -Fc 'let changed = false;' "$scratch/$adapter") == 1 ]]
    sed -i 's/let changed = false;/let changed = true;/' "$scratch/$adapter"
  elif [[ "$mode" == fit-declared-type ]]; then
    base=10156a49eca7c5dca47d8a782f30c48c141c33b5
    adapter=packages/durable/src/images/fit.ts
    assertion=136
    change='label the fitted block GIF instead of using the prepared MIME'
    scope=offline-fit-declared-type
    [[ $(grep -Fc 'mimeType: prepared.mimeType' "$scratch/$adapter") == 1 ]]
    sed -i 's/mimeType: prepared\.mimeType/mimeType: "image\/gif"/' "$scratch/$adapter"
  else
    base=3d0a45b2185b7177439d0cfe8ba60b5c23309b81
    adapter=packages/durable/src/images/fit.ts
    assertion=149
    change='omit the base64 decoding error from the omission note'
    scope=offline-fit-omission-diagnostics
    [[ $(grep -Fc 'return omitted(image.mimeType, `:' "$scratch/$adapter") == 1 ]]
    sed -i '/return omitted(image\.mimeType, `:/c\	\treturn omitted(image.mimeType, "");' "$scratch/$adapter"
    git show "$base:docs/upstream-v1.6.0/test-inventory/tools-images-read-contract.json" |
      jq -er --arg pin "$pin" --arg sha "$(sha256 "$scratch/packages/durable/test/image-fixtures.ts")" '
        select(.pin==$pin and any(.sourceDigests[]; .file=="packages/durable/test/image-fixtures.ts" and .sha256==$sha)) | .fixturesBase64.png4x4
      ' | base64 -d > "$fixture"
  fi
  if run_test note; then printf 'Control unexpectedly passed: %s\n' "$change" >&2; exit 1; fi
  if ! jq -e --arg full "$selector" --arg site "images-fit.test.ts:$assertion:" --slurpfile original "$scratch/original.result.json" '
    .numPassedTests==0 and .numFailedTests==1 and .numPendingTests==10 and .numTodoTests==0 and
    ([.testResults[].assertionResults[]|[.fullName,.status]]==[$original[0].testResults[].assertionResults[]|[.fullName,if .status=="passed" then "failed" else .status end]]) and
    any(.testResults[].assertionResults[]; .fullName==$full and (.failureMessages|join("\n")|contains($site)))
  ' "$scratch/note.result.json" > "$scratch/note.check"; then show_failure note; exit 1; fi
  jq -s 'sort_by(.thread)' "$scratch/note.trace.jsonl" > "$scratch/note.events.json"
  cmp "$scratch/original.events.json" "$scratch/note.events.json"
  if [[ "$mode" != fit-omissions ]]; then cmp "$fixture" "$scratch/note.input.png"; fi
  git -C "$mirror" show "$pin:$adapter" > "$scratch/$adapter"
  flock -u 9
  for file in "$test" "$adapter" packages/durable/{test/image-fixtures.ts,src/images/{index,node,node-worker}.ts}; do
    git -C "$mirror" show "$pin:$file" | cmp "$scratch/$file" -
    jq -cn --arg file "$file" --arg sha "$(sha256 "$scratch/$file")" '{file:$file,sha256:$sha}'
  done > "$scratch/source.jsonl"
  if [[ -n "$fixture_out" ]]; then (set -o noclobber; cat "$fixture" > "$fixture_out"); fi
  jq -n --arg pin "$pin" --arg base "$base" --arg test "$test" --arg selected "$selector" \
    --arg change "$change" --argjson assertion "$assertion" --arg scope "$scope" \
    --arg node "$(node --version)" --arg vitest "$vitest" --arg url "$url" --arg integrity "$integrity" \
    --arg archiveSha "$archive_sha" --arg aiSha "$ai_sha" --arg png "$(base64 -w 0 "$fixture")" \
    --slurpfile deps "$scratch/copied.json" --slurpfile sources "$scratch/source.jsonl" \
    --slurpfile observed "$scratch/original.events.json" --slurpfile report "$scratch/original.result.json" '
    {format:1,pin:$pin,evidenceBase:$base,node:$node,vitest:$vitest,
      selection:{file:$test,caseNames:[$selected],filteredOut:[$report[0].testResults[].assertionResults[]|select(.status=="skipped")|.fullName]},
      result:{passed:1,failed:0,selectedSkipped:0},sourceDigests:$sources,fixtureBase64:$png,
      photonArchive:{url:$url,integrity:$integrity,sha256:$archiveSha},aiArchiveSha256:$aiSha,
      observed:$observed[0],control:{change:$change,failed:[$selected],assertions:[$assertion],workerTrace:"identical"},
      copiedDependencies:$deps[0],exclusions:[],scope:$scope}'
  exit 0
fi

# Keep behavior intact while changing both provenance inputs in scratch.
printf '\n// Photon provenance control.\n' >> "$scratch/$key/$js"
printf '\000\007\005audit\000' >> "$scratch/$key/$wasm"
run_selected control
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

if [[ "$mode" == stable-worker ]]; then
  cmp "$scratch/original.input.png" "$scratch/control.input.png"
  jq -ne --slurpfile orig "$scratch/original.events.json" --slurpfile control "$scratch/control.events.json" '
    ($orig[0]|map(select(.kind=="worker-reply")))==($control[0]|map(select(.kind=="worker-reply")))
  ' > "$scratch/control.reply.check"
  src=("$test" packages/durable/{test/image-fixtures.ts,src/{images/{index,node,node-worker}.ts,tools/image-processor.ts}})
  adapter=packages/durable/src/images/node.ts
  for member in "$js" "$wasm"; do cp "$scratch/$member" "$scratch/$key/$member"; done
  [[ $(grep -Fc 'const copy = bytes.slice();' "$scratch/$adapter") == 1 ]]
  sed -i 's/const copy = bytes\.slice();/const copy = bytes;/' "$scratch/$adapter"
  if run_test transfer; then printf 'Transferring the original input unexpectedly passed\n' >&2; exit 1; fi
  if ! jq -e --arg full "$selector" '
    .numPassedTests==0 and .numFailedTests==1 and .numPendingTests==33 and .numTodoTests==0 and
    ([.testResults[].assertionResults[]|select(.status=="failed")|.fullName]==[$full]) and
    any(.testResults[].assertionResults[]; .fullName==$full and (.failureMessages|join("\n")|test("tools-images\\.test\\.ts:44[01]:")))
  ' "$scratch/transfer.result.json" > "$scratch/transfer.check"; then show_failure transfer; exit 1; fi
  jq -er --arg full "$selector" '
    .testResults[].assertionResults[]|select(.fullName==$full)|.failureMessages|join("\n")|match("tools-images\\.test\\.ts:44[01]:[0-9]+").string
  ' "$scratch/transfer.result.json" > "$scratch/transfer.location"
  git -C "$mirror" show "$pin:$adapter" > "$scratch/$adapter"
  for file in "${src[@]}"; do
    git -C "$mirror" show "$pin:$file" | cmp "$scratch/$file" -
    jq -cn --arg file "$file" --arg sha "$(sha256 "$scratch/$file")" '{file:$file,sha256:$sha}'
  done > "$scratch/source.jsonl"
  if [[ -n "$fixture_out" ]]; then (set -o noclobber; cat "$scratch/original.input.png" > "$fixture_out"); fi
fi
flock -u 9

jq -n --arg pin "$pin" --arg test "$test" --arg testSha "$(sha256 "$scratch/$test")" --arg selector "$selector" \
  --arg node "$(node --version)" --arg vitest "$vitest" --arg url "$url" --arg integrity "$integrity" \
  --arg archiveSha "$archive_sha" --arg aiSha "$ai_sha" \
  --slurpfile deps "$scratch/copied.json" --slurpfile observed "$scratch/original.events.json" \
  --slurpfile control "$scratch/control.events.json" '
  {format:1,pin:$pin,node:$node,vitest:$vitest,
    original:{test:$test,sha256:$testSha,selector:$selector,passed:1},
    photonArchive:{url:$url,integrity:$integrity,sha256:$archiveSha},aiArchiveSha256:$aiSha,
    observed:$observed[0],control:{changed:["glue-comment","wasm-custom-section"],originalPassed:1,
      observed:$control[0],comparisons:"both-archive-checks-rejected"},copiedDependencies:$deps[0]}' > "$scratch/receipt.json"
if [[ "$mode" == stable-worker ]]; then
  jq --arg base "$base" --argjson heap "$heap" --slurpfile sources "$scratch/source.jsonl" --slurpfile result "$scratch/original.result.json" \
    --rawfile location "$scratch/transfer.location" '
    .evidenceBase=$base | .exclusions=[] |
    .original += {failed:0,selectedSkipped:0,filteredOut:[$result[0].testResults[].assertionResults[]|select(.status=="skipped")|.fullName],maxOldSpaceMiB:$heap} |
    .sourceDigests=$sources |
    .ownershipControl={change:"transfer the original caller buffer without making a copy",failed:[.original.selector],assertion:($location|rtrimstr("\n"))}
  ' "$scratch/receipt.json"
else
  cat "$scratch/receipt.json"
fi
