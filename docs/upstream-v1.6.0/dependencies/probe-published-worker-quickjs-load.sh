#!/usr/bin/env bash
set -euo pipefail

if [[ $# != 3 ]]; then
  printf 'Usage: bash %s <pinned-git-mirror> <user-npm-cacache> <release-archive-dir>\n' "$0" >&2
  exit 1
fi
mirror=$(realpath -- "$1")
cache=$(realpath -- "$2")
releases=$(realpath -- "$3")
here=$(cd -- "$(dirname -- "$0")" && pwd -P)
cd "$(git -C "$here" rev-parse --show-toplevel)"
# shellcheck source=/dev/null
source scripts/project-env.sh

pin=faac585884ae2c013a6b1400f3e9e77f10b34bac
upstream=packages/codemode/test/sandbox.test.ts
case=published-worker-namespaced-case.mjs
codemode=node_modules/@earendil-works/pi-codemode
worker="$codemode/dist/runtime/worker.js"
quickjs=node_modules/quickjs-wasi
scratch=$(mktemp -d "$TMPDIR/published-worker-quickjs-load.XXXXXX")
trap 'rm -r -- "$scratch"' EXIT
mkdir -p "$scratch/home" "$scratch/tmp" "$scratch/$codemode" "$scratch/$quickjs"
sha256() { sha256sum -- "$1" | cut -d' ' -f1; }

git -C "$mirror" show "$pin:package-lock.json" > "$scratch/lock.json"
git -C "$mirror" show "$pin:$upstream" > "$scratch/source-test.ts"
lock_sha=$(sha256 "$scratch/lock.json")
for receipt in npm-closure npm-archives; do
  jq -e --arg pin "$pin" --arg sha "$lock_sha" 'select(.kind=="manifest") | .pin==$pin and
    (.lockSha256 // .inputs["package-lock.json"])==$sha' "$here/$receipt.jsonl" > "$scratch/$receipt.check"
done
read -r codemode_file codemode_sha < <(jq -er --arg key "$codemode" '
  select(.kind=="workspace" and .linkKey==$key and .version=="1.6.0" and .archive.proof=="bytes-match-release-receipt")
  | .archive | [.file,.sha256] | @tsv' "$here/npm-closure.jsonl")
[[ $codemode_file == earendil-works-pi-codemode-1.6.0.tgz && $(sha256 "$releases/$codemode_file") == "$codemode_sha" ]]
tar -xzf "$releases/$codemode_file" -C "$scratch/$codemode" --strip-components=1 -- package/dist package/package.json
read -r version url integrity < <(jq -er --arg key "$quickjs" '.packages[$key] | [.version,.resolved,.integrity] | @tsv' "$scratch/lock.json")
[[ $version == 3.6.2 && $url == 'https://registry.npmjs.org/quickjs-wasi/-/quickjs-wasi-3.6.2.tgz' ]]
read -r cache_key quickjs_sha < <(jq -er --arg key "$quickjs" --arg url "$url" --arg integrity "$integrity" '
  select(.kind=="archive" and .lockKey==$key and .state=="verified-archive-bytes" and .integrity==$integrity and .resolved==$url)
  | [.archive[] | select(.cache=="user") | [.key,.sha256]] | if length==1 then .[0] | @tsv else error("no unique user cache record") end
' "$here/npm-archives.jsonl")
[[ $cache_key =~ ^content-v2/sha512/[0-9a-f]{2}/[0-9a-f]{2}/[0-9a-f]{124}$ ]]
archive="$cache/$cache_key"
[[ $(sha256 "$archive") == "$quickjs_sha" ]]
[[ sha512-$(openssl dgst -sha512 -binary "$archive" | openssl base64 -A) == "$integrity" ]]
tar -xzf "$archive" -C "$scratch/$quickjs" --strip-components=1 -- package/dist package/quickjs.wasm package/package.json
cp -- "$scratch/$quickjs/quickjs.wasm" "$scratch/archive.wasm"
cp -- "$here/worker-quickjs-witness.mjs" "$scratch/witness.mjs"
cp -- "$here/$case" "$scratch/$case"

run_selected() {
  local tag=$1
  env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" WORKER_QUICKJS_ROOT="$scratch" \
    WORKER_QUICKJS_LAYOUT=published WORKER_QUICKJS_TRACE="$scratch/$tag.trace.jsonl" \
    NODE_OPTIONS="--permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-worker --allow-wasi --import=$scratch/witness.mjs" \
    timeout 25s node "$scratch/$case" > "$scratch/$tag.result.json" 2> "$scratch/$tag.log" || { cat "$scratch/$tag.log" >&2; return 1; }
  jq -e --arg worker "$worker" '.=={worker:$worker,value:[["list","first"],"a",false],spreadCalls:[["classifier",null,3],[]]}' \
    "$scratch/$tag.result.json" > "$scratch/$tag.behavior.check"
  jq -es --arg dist "$codemode/dist" --arg worker "$worker" --arg sdk "$quickjs/dist/index.js" '
    length==9 and ([.[]|select(.kind=="compile-wasm" or .kind=="worker-handoff" or .kind=="worker-result")|.kind]
      ==["compile-wasm","worker-handoff","worker-result"]) and
    ([.[]|select(.kind=="load-guest-js" or .kind=="instantiate-worker-data")|.kind]
      ==["load-guest-js","load-guest-js","instantiate-worker-data"]) and
    ([.[]|select(.kind=="load-host-js")|.entry]|sort)==([$dist+"/index.js",$dist+"/runtime/host.js",$dist+"/wasm.js"]|sort) and
    ([.[]|select(.kind=="load-guest-js")|.entry]|sort)==([$worker,$sdk]|sort) and
    ([.[]|select(.kind=="worker-result")|[.ok,.value]]==[[true,"[[\"list\",\"first\"],\"a\",false]"]])
  ' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.events.check" || { cat "$scratch/$tag.trace.jsonl" >&2; return 1; }
  jq -s 'sort_by(.kind,.entry)' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.events.json"
}

verify_asset() {
  local tag=$1 kind=$2 entry=$3 file=$4
  jq -e --arg kind "$kind" --arg entry "$entry" --arg sha "$(sha256 "$file")" --argjson bytes "$(wc -c < "$file")" '
    [.[]|select(.kind==$kind and ($entry=="" or (.entry==$entry and .format=="module")))] as $events |
      ($events|length)==1 and ($events[0]|.sha256==$sha and .bytes==$bytes)
  ' "$scratch/$tag.events.json"
}

exec 9>/tmp/go-ai/v160-focused-verify.lock
flock 9
run_selected original
printf '\000\007\005audit\000' >> "$scratch/$quickjs/quickjs.wasm"
run_selected control
flock -u 9

for tag in original control; do
  for entry in "$codemode/dist/index.js" "$codemode/dist/runtime/host.js" "$codemode/dist/wasm.js"; do
    verify_asset "$tag" load-host-js "$entry" "$scratch/$entry" > "$scratch/$tag.host.check"
  done
  for entry in "$worker" "$quickjs/dist/index.js"; do
    verify_asset "$tag" load-guest-js "$entry" "$scratch/$entry" > "$scratch/$tag.guest.check"
  done
done
for kind in compile-wasm worker-handoff; do
  verify_asset original "$kind" "" "$scratch/archive.wasm" > "$scratch/$kind.archive.check"
  verify_asset control "$kind" "" "$scratch/$quickjs/quickjs.wasm" > "$scratch/$kind.control-input.check"
  if verify_asset control "$kind" "" "$scratch/archive.wasm" > "$scratch/$kind.control-archive.check"; then
    printf 'Control unexpectedly matched archive at %s\n' "$kind" >&2
    exit 1
  else
    [[ $? == 1 ]] # jq returned false, not an invocation or parse error.
  fi
done
cmp "$scratch/original.result.json" "$scratch/control.result.json"

jq -n --arg pin "$pin" --arg node "$(node --version)" --arg upstream "$upstream" --arg sha "$(sha256 "$scratch/source-test.ts")" \
  --arg platform "$(node -p 'process.platform + "/" + process.arch')" --arg codemodeFile "$codemode_file" \
  --arg codemodeSha "$codemode_sha" --arg quickjsSha "$quickjs_sha" --arg integrity "$integrity" \
  --slurpfile result "$scratch/original.result.json" --slurpfile original "$scratch/original.events.json" \
  --slurpfile control "$scratch/control.events.json" '
  {format:1,pin:$pin,node:$node,platform:$platform,
    upstreamReference:{file:$upstream,sha256:$sha,lines:"506-523",execution:"standalone-replay-of-selected-script-and-assertions"},
    codemodeArchive:{file:$codemodeFile,sha256:$codemodeSha},quickjsArchive:{version:"3.6.2",sha256:$quickjsSha,integrity:$integrity},
    behavior:$result[0],observed:$original[0],control:{change:"append-valid-wasm-custom-section",behavior:"unchanged",
      observed:$control[0],comparison:"archive-checks-rejected-at-compile-and-worker-handoff"}}'
