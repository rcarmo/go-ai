#!/usr/bin/env bash
# Usage: bash probe-photon-codec.sh <upstream git mirror> <staged oracle> <npm _cacache> <release archive dir>
# Uses a private copy and an empty Linux network namespace (sudo -n unshare); prints a JSON receipt.
set -euo pipefail
if [[ $# != 4 ]]; then
  printf 'Usage: bash %s <upstream git mirror> <staged oracle> <npm _cacache> <release archive dir>\n' "$0" >&2
  exit 2
fi

here=$(cd "$(dirname -- "$0")" && pwd)
mirror=$(realpath -- "$1")
oracle=$(realpath -- "$2")
cache=$(realpath -- "$3")
releases=$(realpath -- "$4")
pin=ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe
photon=node_modules/@silvia-odwyer/photon
suite=packages/durable/test/tools-images.test.ts
selector='read of images, with the Photon image processor converts a format providers do not take inline to PNG'
work=$(mktemp -d "${TMPDIR:-/tmp}/photon-v161.XXXXXXXX")
trap 'rm -r -- "$work"' EXIT
mkdir -p "$work/home" "$work/tmp"
sha() { sha256sum < "$1" | cut -d ' ' -f 1; }
size() { wc -c < "$1" | tr -d ' '; }

# Exact source from Git; dependency files from a private copy of the existing Node staging.
git -C "$mirror" archive "$pin" package{,-lock}.json packages/{ai,chord,codemode,durable,schema,telemetry}/{package.json,src} \
  packages/durable/vitest.config.ts packages/durable/test | tar -x -C "$work"
cmp "$work/package-lock.json" "$oracle/package-lock.json"
cp -a --reflink=auto "$oracle/node_modules" "$work/"
rm -rf -- "$work/node_modules/.vite" "$work/node_modules/.vite-temp"
cp "$here/photon-codec-witness.mjs" "$work/witness.mjs"

lock=$(jq -ce --arg k "$photon" '.packages[$k] | select(.version=="0.3.3") | {version,resolved,integrity}' "$work/package-lock.json")
sri=$(jq -er '.integrity | select(startswith("sha512-"))' <<< "$lock")
hex=$(printf '%s' "${sri#sha512-}" | base64 -d | od -An -v -tx1 | tr -d ' \n')
archive=$cache/content-v2/sha512/${hex:0:2}/${hex:2:2}/${hex:4}
printf '%s  %s\n' "$hex" "$archive" | sha512sum --check --status
for member in package.json photon_rs.js photon_rs_bg.wasm; do
  tar -xOzf "$archive" "package/$member" > "$work/member"
  cmp "$work/member" "$work/$photon/$member"
  jq -cn --arg name "$member" --arg sha "$(sha "$work/member")" --argjson bytes "$(size "$work/member")" \
    '{name:$name,sha256:$sha,bytes:$bytes}' >> "$work/members.jsonl"
done
glue=$work/$photon/photon_rs.js
glue_sha=$(sha "$glue")
wasm_sha=$(sha "$work/$photon/photon_rs_bg.wasm")

# The pinned AI main entry points to its released dist tree.
read -r ai_name ai_sha < <(jq -er 'select(.workspace=="packages/ai" and .version=="1.6.1") | [.file,.sha256] | @tsv' "$here/package-archives.jsonl")
[[ $ai_name == earendil-works-pi-ai-1.6.1.tgz && $(sha "$releases/$ai_name") == "$ai_sha" ]]
tar -xzf "$releases/$ai_name" -C "$work/packages/ai" --strip-components=1 package/dist
runner=$(jq -er '.version | select(.=="4.1.12")' "$work/node_modules/vitest/package.json")
for source in "$suite" packages/durable/{test/{image-fixtures,setup-document-retention}.ts,vitest.config.ts,src/images/{index,node,node-worker}.ts}; do
  jq -cn --arg path "$source" --arg sha "$(sha "$work/$source")" '{path:$path,sha256:$sha}' >> "$work/sources.jsonl"
done

run_case() {
  local tag=$1
  if ! (
    cd "$work/packages/durable"
    sudo -n unshare --net --setuid="$(id -u)" --setgid="$(id -g)" \
      env -i PATH="$PATH" HOME="$work/home" TMPDIR="$work/tmp" PHOTON_ROOT="$work" PHOTON_TRACE="$work/$tag.trace.jsonl" \
      NODE_OPTIONS="--max-old-space-size=512 --permission --allow-fs-read=$work --allow-fs-write=$work --allow-child-process --allow-worker --allow-addons --import=$work/witness.mjs" \
      node ../../node_modules/vitest/vitest.mjs run "${suite#packages/durable/}" -t "$selector" --pool=forks --maxWorkers=1 \
      --reporter=json --outputFile "$work/$tag.report.json"
  ) > "$work/$tag.log" 2>&1; then
    cat "$work/$tag.log" >&2
    if [[ -f $work/$tag.report.json ]]; then
      jq '.testResults[].assertionResults[] | select(.status=="failed") | {fullName,failureMessages}' "$work/$tag.report.json" >&2
    fi
    return 1
  fi
  jq -e --arg selected "$selector" '.numPassedTests==1 and .numFailedTests==0 and .numTodoTests==0 and
    ([.testResults[].assertionResults[] | select(.status=="passed") | .fullName] == [$selected])' "$work/$tag.report.json" > /dev/null
  # Vitest transforms the fixture import. Only the image worker exposes a native JS module load.
  if ! jq -es --arg wasm "$wasm_sha" '
    {fork: map(select(.thread=="vitest-fork")|del(.thread)), worker: map(select(.thread=="worker")|del(.thread))}
    | select(([.fork[].kind]==["instantiate-wasm"]) and
       ([.worker[].kind]==["load-glue","instantiate-wasm","request","reply"]) and
       (.fork[0].sha256==$wasm and .worker[1].sha256==$wasm) and
       (.worker[2].mimeType=="image/bmp" and .worker[2].id==.worker[3].id) and
       (.worker[3].tag=="prepared" and .worker[3].mimeType=="image/png" and
        .worker[3].convertedFrom=="image/bmp" and .worker[3].signature=="89504e470d0a1a0a"))' \
    "$work/$tag.trace.jsonl" > "$work/$tag.trace.json"; then
    echo "unexpected Photon trace ($tag)" >&2
    cat "$work/$tag.trace.jsonl" >&2
    return 1
  fi
}
checks_glue() {
  jq -e --arg sha "$glue_sha" '.worker[0].sha256==$sha' "$1" > /dev/null
}

run_case pinned
checks_glue "$work/pinned.trace.json"
# A valid comment changes only the archive identity. The unchanged test must still pass.
printf '\n// Photon archive identity control.\n' >> "$glue"
run_case changed
if checks_glue "$work/changed.trace.json"; then
  echo 'changed glue incorrectly matches the archive' >&2; exit 1
fi
jq -e --arg sha "$(sha "$glue")" '.worker[0].sha256==$sha' "$work/changed.trace.json" > /dev/null
for tag in pinned changed; do jq 'del(.worker[0])' "$work/$tag.trace.json" > "$work/$tag.no-glue.json"; done
cmp "$work/pinned.no-glue.json" "$work/changed.no-glue.json"

jq -n --arg pin "$pin" --arg lockSha "$(sha "$work/package-lock.json")" --arg lockKey "$photon" --argjson lock "$lock" \
  --arg archiveSha "$(sha "$archive")" --argjson archiveBytes "$(size "$archive")" --slurpfile members "$work/members.jsonl" \
  --arg aiSha "$ai_sha" --arg node "$(node --version)" --arg vitest "$runner" --arg suite "$suite" --arg selected "$selector" \
  --slurpfile sources "$work/sources.jsonl" --slurpfile pinned "$work/pinned.trace.json" --slurpfile changed "$work/changed.trace.json" \
  --argjson filtered "$(jq .numPendingTests "$work/pinned.report.json")" \
  '{pin:$pin,lockSHA256:$lockSha,photon:{lock:($lock+{key:$lockKey}),archiveSHA256:$archiveSha,archiveBytes:$archiveBytes,
    matchedStagedMembers:$members},aiArchiveSHA256:$aiSha,sourceFiles:$sources,
    execution:{node:$node,vitest:$vitest,network:"isolated Linux network namespace",suite:$suite,selector:$selected,
      passed:1,failed:0,selectedSkipped:0,filteredOut:$filtered,observed:$pinned[0]},
    scope:"One pinned Node original; no workerd, timing, idle restart, failure-tag or Go encoder coverage.",
    control:{change:"append valid JS comment to the private staged glue",passed:1,archiveMatch:false,otherTraceEventsUnchanged:true,
      workerGlue:$changed[0].worker[0]}}'
