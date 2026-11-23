#!/usr/bin/env bash
set -euo pipefail

if [[ $# != 4 && ( $# != 5 || $5 != preparation ) ]]; then
  printf 'Usage: bash %s <pinned-git-mirror> <prepared-oracle-dir> <user-npm-cacache> <release-archive-dir> [preparation]\n' "$0" >&2
  exit 1
fi
mirror=$(realpath -- "$1")
oracle=$(realpath -- "$2")
cache=$(realpath -- "$3")
releases=$(realpath -- "$4")
here=$(cd -- "$(dirname -- "$0")" && pwd -P)
deps="$here/../dependencies"
cd "$(git -C "$here" rev-parse --show-toplevel)"
# shellcheck source=/dev/null
source scripts/project-env.sh

pin=faac585884ae2c013a6b1400f3e9e77f10b34bac
base=4cff3ac164d79b3acb8399e311c342d59c91f6dc
mode=${5:-read}
[[ "$mode" != preparation ]] || base=67f9d63a4f79b3e730d14289193f7c147726f08f
queue=docs/upstream-v1.6.0/test-inventory/primary-queue-r21.jsonl
test=packages/durable/test/tools-images.test.ts
adapter=packages/durable/src/tools/read.ts
selector='^read of (images, without an image processor|an image that changes while it is read) '
key=node_modules/@silvia-odwyer/photon
js=photon_rs.js
wasm=photon_rs_bg.wasm
scratch=$(mktemp -d "$TMPDIR/tools-images.XXXXXX")
trap 'rm -r -- "$scratch"' EXIT
mkdir -p "$scratch/home" "$scratch/tmp"
for ledger in npm-archives.jsonl provider-catalog-assets.jsonl npm-closure.jsonl; do
  git show "$base:docs/upstream-v1.6.0/dependencies/$ledger" > "$scratch/$ledger"
done
sha256() { sha256sum -- "$1" | cut -d' ' -f1; }

# Verify compressed Photon bytes against the lock and the retained receipt.
git -C "$mirror" show "$pin:package-lock.json" > "$scratch/lock.json"
lock_sha=$(sha256 "$scratch/lock.json")
jq -e --arg pin "$pin" --arg sha "$lock_sha" 'select(.kind=="manifest") | .pin==$pin and .lockSha256==$sha' \
  "$scratch/npm-archives.jsonl" > "$scratch/lock.check"
read -r version url integrity < <(jq -er --arg key "$key" '.packages[$key] | [.version,.resolved,.integrity] | @tsv' "$scratch/lock.json")
[[ $version == 0.3.3 && $url == 'https://registry.npmjs.org/@silvia-odwyer/photon/-/photon-0.3.3.tgz' ]]
read -r cache_key archive_sha < <(jq -er --arg key "$key" --arg url "$url" --arg integrity "$integrity" '
  select(.kind=="archive" and .lockKey==$key and .state=="verified-archive-bytes" and .resolved==$url and .integrity==$integrity)
  | [.archive[] | select(.cache=="user") | [.key,.sha256]] | if length==1 then .[0] | @tsv else error("no unique user cache record") end
' "$scratch/npm-archives.jsonl")
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
' "$scratch/provider-catalog-assets.jsonl")
[[ $ai_file == earendil-works-pi-ai-1.6.0.tgz && $(sha256 "$releases/$ai_file") == "$ai_sha" ]]
tar -xzf "$releases/$ai_file" -C "$scratch/packages/ai" --strip-components=1 package/dist
libc=$(node -p "process.report.getReport().header.glibcVersionRuntime ? 'glibc' : 'musl'")
bun run "$deps/copy-openai-probe-deps.ts" "$scratch/npm-closure.jsonl" "$lock_sha" "$oracle" "$scratch" "$libc" \
  node_modules/vitest node_modules/diff "$key" node_modules/quickjs-wasi node_modules/partial-json > "$scratch/copied.json"
mkdir -p "$scratch/node_modules/@earendil-works"
for p in chord pi-schema pi-telemetry pi-codemode pi-ai pi-durable; do
  ln -s "../../packages/${p#pi-}" "$scratch/node_modules/@earendil-works/$p"
done
for member in "$js" "$wasm"; do cmp "$scratch/$member" "$scratch/$key/$member"; done
vitest=$(jq -er '.version' "$scratch/node_modules/vitest/package.json")
[[ $vitest == 4.1.12 ]]

sources=("$test" "$adapter" packages/durable/{vitest.config.ts,test/{image-fixtures,setup-document-retention}.ts,src/{env/node.ts,images/node.ts,tools/{image,image-processor}.ts,truncate.ts}})
check_sources() {
  local file
  for file in "${sources[@]}"; do git -C "$mirror" show "$pin:$file" | cmp "$scratch/$file" -; done
}
if [[ "$mode" == preparation ]]; then
  sources+=(packages/durable/src/images/{exif,index,node-worker}.ts)
fi
check_sources
sha=$(sha256 "$scratch/$test")
git show "$base:$queue" | jq -e --arg file "$test" --arg sha "$sha" '
  select(.kind=="suite" and .path==$file) | .upstream.sha256==$sha and .original.status=="none-found"
' > "$scratch/queue.check"

run() {
  local tag=$1 pattern=${2:-$selector} heap=${3:-768}
  (cd "$scratch/packages/durable" && env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" \
    NODE_OPTIONS="--max-old-space-size=$heap --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons" \
    node ../../node_modules/vitest/vitest.mjs run test/tools-images.test.ts -t "$pattern" --pool=forks --maxWorkers=1 \
    --reporter=json --outputFile "$scratch/$tag.json") > "$scratch/$tag.log" 2>&1
}
exec 9>/tmp/go-ai/v160-focused-verify.lock
flock 9
if [[ "$mode" == preparation ]]; then
  processor=packages/durable/src/images/index.ts
  prior=docs/upstream-v1.6.0/test-inventory/tools-images.json
  stable="createNodePhotonImages gives the same result as the calling thread, and keeps the caller's bytes"
  timing1='createNodePhotonImages prepares in a worker thread, leaving the event loop free'
  timing2='createNodePhotonImages prepares concurrent images, and starts a new worker after the idle one exited'
  selector='^(read of images, with the Photon image processor|EXIF orientation|imageDimensions|toBase64) '
  worker_selector="^$stable$"
  worker_heap=1856 # The original deep-compares a multi-MiB Uint8Array in Vitest.
  git show "$base:$prior" > "$scratch/prior.json"
  jq -e --arg file "$test" --arg pin "$pin" --arg sha "$sha" '
    .pin==$pin and .selection.file==$file and any(.sourceDigests[];.file==$file and .sha256==$sha) and
    .result=={passed:9,failed:0,selectedSkipped:0} and ((.selection.caseNames+.selection.filteredOut)|unique|length)==34
  ' "$scratch/prior.json" > "$scratch/prior.check"
  jq -e --arg stable "$stable" --arg timing1 "$timing1" --arg timing2 "$timing2" '
    .selection.filteredOut-[$stable,$timing1,$timing2]|select(length==22)
  ' "$scratch/prior.json" > "$scratch/cases.json"
  show_failure() {
    local tag=$1
    cat "$scratch/$tag.log" >&2
    jq -c '[.testResults[] | {message,failed:[.assertionResults[]|select(.status=="failed")|{fullName,failureMessages}]}]' "$scratch/$tag.json" >&2 || true
  }
  if ! run original; then show_failure original; exit 1; fi
  jq -e --slurpfile cases "$scratch/cases.json" '
    .numPassedTests==22 and .numFailedTests==0 and .numPendingTests==12 and .numTodoTests==0 and
    ([.testResults[].assertionResults[]|select(.status!="skipped")|[.fullName,.status]]==($cases[0]|map([.,"passed"])))
  ' "$scratch/original.json" > "$scratch/original.check"
  if ! run worker "$worker_selector" "$worker_heap"; then show_failure worker; exit 1; fi
  jq -e --arg stable "$stable" '
    .numPassedTests==1 and .numFailedTests==0 and .numPendingTests==33 and .numTodoTests==0 and
    ([.testResults[].assertionResults[]|select(.status!="skipped")|[.fullName,.status]]==[[$stable,"passed"]])
  ' "$scratch/worker.json" > "$scratch/worker.check"
  jq -ne --slurpfile original "$scratch/original.json" --slurpfile worker "$scratch/worker.json" --slurpfile prior "$scratch/prior.json" \
    --arg timing1 "$timing1" --arg timing2 "$timing2" '
    [$original[0].testResults[].assertionResults[]] as $main |
    [$worker[0].testResults[].assertionResults[]] as $workerCases |
    ($main|map(.fullName)) as $names |
    ($main|map(select(.status=="passed")|.fullName)) + ($workerCases|map(select(.status=="passed")|.fullName)) as $selected |
    ($names-$selected) as $unselected |
    if $names==($workerCases|map(.fullName)) and ($selected|length)==23 and ($selected|unique|length)==23 and
      $unselected==($prior[0].selection.caseNames+[$timing1,$timing2]) then {caseNames:$selected,filteredOut:$unselected}
    else error("preparation partitions differ from the audited case names") end
  ' > "$scratch/selection.json"
  failed='EXIF orientation turns orientation 7 upright'
  [[ $(grep -Fc 'orientation === 5 || orientation === 8 ? y' "$scratch/$processor") == 1 ]]
  sed -i 's/orientation === 5 || orientation === 8 ? y/orientation === 5 || orientation === 8 || orientation === 7 ? y/' "$scratch/$processor"
  if run mutated; then printf 'Incorrect orientation 7 unexpectedly passed\n' >&2; exit 1; fi
  if ! jq -e --arg failed "$failed" --slurpfile original "$scratch/original.json" '
    .numPassedTests==21 and .numFailedTests==1 and .numPendingTests==12 and .numTodoTests==0 and
    ([.testResults[].assertionResults[]|[.fullName,.status]]==[$original[0].testResults[].assertionResults[]|[.fullName,if .fullName==$failed then "failed" else .status end]]) and
    any(.testResults[].assertionResults[]; .fullName==$failed and (.failureMessages|join("\n")|contains("tools-images.test.ts:313:")))
  ' "$scratch/mutated.json" > "$scratch/mutated.check"; then show_failure mutated; exit 1; fi
  flock -u 9
  git -C "$mirror" show "$pin:$processor" > "$scratch/$processor"
  check_sources
  for member in "$js" "$wasm"; do cmp "$scratch/$member" "$scratch/$key/$member"; done
  for file in "${sources[@]}"; do
    jq -cn --arg file "$file" --arg sha "$(sha256 "$scratch/$file")" '{file:$file,sha256:$sha}'
  done > "$scratch/sources.jsonl"
  jq -n --arg pin "$pin" --arg base "$base" --arg test "$test" --arg prior "$prior" --arg selector "$selector" --arg workerSelector "$worker_selector" \
    --argjson workerHeap "$worker_heap" --arg failed "$failed" --arg node "$(node --version)" --arg vitest "$vitest" \
    --arg url "$url" --arg integrity "$integrity" --arg archiveSha "$archive_sha" --arg aiSha "$ai_sha" \
    --arg jsSha "$(sha256 "$scratch/$js")" --arg wasmSha "$(sha256 "$scratch/$wasm")" \
    --slurpfile selection "$scratch/selection.json" --slurpfile sources "$scratch/sources.jsonl" --slurpfile deps "$scratch/copied.json" \
    --slurpfile control "$scratch/mutated.json" '
    {format:1,pin:$pin,evidenceBase:$base,node:$node,vitest:$vitest,
     selection:({file:$test,selector:$selector,workerSelector:$workerSelector}+$selection[0]),
     result:{passed:23,failed:0,selectedSkipped:0},runs:[{selection:"preparation",passed:22,maxOldSpaceMiB:768},{selection:"stable-worker",passed:1,maxOldSpaceMiB:$workerHeap}],
     sourceDigests:$sources,photonArchive:{url:$url,integrity:$integrity,sha256:$archiveSha,jsSha256:$jsSha,wasmSha256:$wasmSha},aiArchiveSha256:$aiSha,
     control:{change:"use the orientation 8 pixel transform for orientation 7 in scratch",failed:[$failed],passed:[$control[0].testResults[].assertionResults[]|select(.status=="passed")|.fullName]},
     copiedDependencies:$deps[0],priorReadReceipt:{file:$prior,commit:$base},exclusions:[],scope:"offline-preparation-exif-and-stable-worker"}'
  exit 0
fi
if ! run original; then cat "$scratch/original.log" >&2; jq -c '[.testResults[] | {message,failed:[.assertionResults[]|select(.status=="failed")|{fullName,failureMessages}]}]' "$scratch/original.json" >&2 || true; exit 1; fi
jq -n '[
  ("read of images, without an image processor " + ([
    "returns a supported image within the limits as it is",
    "refuses an image too large to send without reading it",
    "refuses an image wider or taller than the limits, by its header",
    "refuses a format that needs converting",
    "names a text-only model in details for UIs, not in what the model gets",
    "applies the model\u0027s image limits instead of the defaults",
    "lists bmp in the description only with a processor"][])),
  ("read of an image that changes while it is read " + ([
    "reads it again rather than return a cut-off image, even when it grew",
    "fails when it changes during the second read too"][]))
]' > "$scratch/cases.json"
jq -e --slurpfile cases "$scratch/cases.json" '
  .numPassedTests==9 and .numFailedTests==0 and .numPendingTests==25 and .numTodoTests==0 and
  ([.testResults[].assertionResults[]|select(.status!="skipped")|[.fullName,.status]]==($cases[0]|map([.,"passed"]))) and
  ([.testResults[].assertionResults[] | .fullName]|unique|length)==34
' "$scratch/original.json" > "$scratch/original.check"
failed='read of images, without an image processor refuses an image too large to send without reading it'
[[ $(grep -Fc 'if (base64Length(info.size) > limits.maxBytes)' "$scratch/$adapter") == 1 ]]
sed -i 's/if (base64Length(info.size) > limits.maxBytes)/if (false)/' "$scratch/$adapter"
if run mutated; then printf 'Byte guard removal unexpectedly passed\n' >&2; exit 1; fi
if ! jq -e --arg failed "$failed" --slurpfile original "$scratch/original.json" '
  .numPassedTests==8 and .numFailedTests==1 and .numPendingTests==25 and .numTodoTests==0 and
  ([.testResults[].assertionResults[] | [.fullName,.status]]==[$original[0].testResults[].assertionResults[]|[.fullName,if .fullName==$failed then "failed" else .status end]]) and
  any(.testResults[].assertionResults[]; .fullName==$failed and (.failureMessages|join("\n")|contains("tools-images.test.ts:123:")))
' "$scratch/mutated.json" > "$scratch/mutated.check"; then
  jq -c '[.testResults[].assertionResults[]|select(.status=="failed")|{fullName,failureMessages}]' "$scratch/mutated.json" >&2
  exit 1
fi
flock -u 9
git -C "$mirror" show "$pin:$adapter" > "$scratch/$adapter"
check_sources
for member in "$js" "$wasm"; do cmp "$scratch/$member" "$scratch/$key/$member"; done
for file in "${sources[@]}"; do
  jq -cn --arg file "$file" --arg sha "$(sha256 "$scratch/$file")" '{file:$file,sha256:$sha}'
done > "$scratch/sources.jsonl"
jq -n --arg pin "$pin" --arg base "$base" --arg test "$test" --arg queue "$queue" --arg selector "$selector" --arg failed "$failed" \
  --arg node "$(node --version)" --arg vitest "$vitest" --arg url "$url" --arg integrity "$integrity" \
  --arg archiveSha "$archive_sha" --arg aiSha "$ai_sha" --arg jsSha "$(sha256 "$scratch/$js")" --arg wasmSha "$(sha256 "$scratch/$wasm")" \
  --slurpfile report "$scratch/original.json" --slurpfile sources "$scratch/sources.jsonl" --slurpfile deps "$scratch/copied.json" '
  [$report[0].testResults[].assertionResults[]] as $cases |
  {format:1,pin:$pin,evidenceBase:$base,node:$node,vitest:$vitest,
   selection:{file:$test,selector:$selector,caseNames:[$cases[]|select(.status=="passed")|.fullName],filteredOut:[$cases[]|select(.status=="skipped")|.fullName]},
   result:{passed:9,failed:0,selectedSkipped:0},sourceDigests:$sources,
   photonArchive:{url:$url,integrity:$integrity,sha256:$archiveSha,jsSha256:$jsSha,wasmSha256:$wasmSha},
   aiArchiveSha256:$aiSha,control:{change:"bypass the inline base64 size guard in scratch read.ts",failed:[$failed],passed:($cases|map(select(.status=="passed" and .fullName!=$failed)|.fullName))},
   copiedDependencies:$deps[0],priorQueue:{file:$queue,commit:$base},exclusions:[],scope:"offline-read-image-guard-and-retry-originals"}'
