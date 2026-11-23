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
test=packages/codemode/test/sandbox.test.ts
selector='worker backend globals groups namespaced globals and spreads arguments on request'
worker=packages/codemode/src/runtime/worker.ts
key=node_modules/quickjs-wasi
scratch=$(mktemp -d "$TMPDIR/worker-quickjs-load.XXXXXX")
trap 'rm -r -- "$scratch"' EXIT
mkdir -p "$scratch/home" "$scratch/tmp"
sha256() { sha256sum -- "$1" | cut -d' ' -f1; }

git -C "$mirror" show "$pin:package-lock.json" > "$scratch/lock.json"
lock_sha=$(sha256 "$scratch/lock.json")
jq -e --arg pin "$pin" --arg sha "$lock_sha" 'select(.kind=="manifest") | .pin==$pin and .lockSha256==$sha' \
  "$here/npm-archives.jsonl" > "$scratch/lock.check"
read -r version url integrity < <(jq -er --arg key "$key" '.packages[$key] | [.version,.resolved,.integrity] | @tsv' "$scratch/lock.json")
[[ $version == 3.6.2 && $url == 'https://registry.npmjs.org/quickjs-wasi/-/quickjs-wasi-3.6.2.tgz' ]]
read -r cache_key archive_sha < <(jq -er --arg key "$key" --arg url "$url" --arg integrity "$integrity" '
  select(.kind=="archive" and .lockKey==$key and .state=="verified-archive-bytes" and .resolved==$url and .integrity==$integrity)
  | [.archive[] | select(.cache=="user") | [.key,.sha256]] | if length==1 then .[0] | @tsv else error("no unique user cache record") end
' "$here/npm-archives.jsonl")
[[ $cache_key =~ ^content-v2/sha512/[0-9a-f]{2}/[0-9a-f]{2}/[0-9a-f]{124}$ ]]
archive="$cache/$cache_key"
[[ $(sha256 "$archive") == "$archive_sha" ]]
[[ sha512-$(openssl dgst -sha512 -binary "$archive" | openssl base64 -A) == "$integrity" ]]

# Stage the pinned original, its source/config, and only the runner closure.
git -C "$mirror" archive "$pin" package.json packages/codemode/{package.json,vitest.config.ts,src} "$test" | tar -x -C "$scratch"
libc=$(node -p "process.report.getReport().header.glibcVersionRuntime ? 'glibc' : 'musl'")
bun run "$here/copy-openai-probe-deps.ts" "$here/npm-closure.jsonl" "$lock_sha" "$oracle" "$scratch" "$libc" \
  node_modules/vitest > "$scratch/copied.json"
mkdir -p "$scratch/$key"
tar -xzf "$archive" -C "$scratch/$key" --strip-components=1 -- package/package.json package/dist package/quickjs.wasm
cp -- "$scratch/$key/quickjs.wasm" "$scratch/archive.wasm"
cp -- "$here/worker-quickjs-witness.mjs" "$scratch/witness.mjs"
vitest=$(jq -er '.version' "$scratch/node_modules/vitest/package.json")
[[ $vitest == 4.1.12 ]]

run_selected() {
  local tag=$1
  (
    cd "$scratch/packages/codemode"
    env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" WORKER_QUICKJS_ROOT="$scratch" \
      WORKER_QUICKJS_TRACE="$scratch/$tag.trace.jsonl" \
      NODE_OPTIONS="--max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons --allow-wasi --import=$scratch/witness.mjs" \
      node ../../node_modules/vitest/vitest.mjs run test/sandbox.test.ts -t "$selector" --pool=forks --maxWorkers=1 \
      --reporter=json --outputFile "$scratch/$tag.result.json"
  ) > "$scratch/$tag.log" 2>&1 || { cat "$scratch/$tag.log" >&2; return 1; }
  jq -e --arg full "$selector" '.numPassedTests==1 and .numFailedTests==0 and
    ([.testResults[].assertionResults[]|select(.status=="passed")|.fullName]==[$full])' \
    "$scratch/$tag.result.json" > "$scratch/$tag.test.check"
  # Require order on each thread without assuming when their writes interleave.
  jq -es --arg worker "$worker" --arg sdk "$key/dist/index.js" '
    length==6 and ([.[]|select(.kind=="compile-wasm" or .kind=="worker-handoff" or .kind=="worker-result")|.kind]
      ==["compile-wasm","worker-handoff","worker-result"]) and
    ([.[]|select(.kind=="load-guest-js" or .kind=="instantiate-worker-data")|.kind]
      ==["load-guest-js","load-guest-js","instantiate-worker-data"]) and
    ([.[]|select(.kind=="load-guest-js")|{entry,format}]|sort_by(.entry))==
      ( [{entry:$worker,format:"module-typescript"},{entry:$sdk,format:"module"}] | sort_by(.entry) ) and
    ([.[]|select(.kind=="worker-result")|[.ok,.value]]==[[true,"[[\"list\",\"first\"],\"a\",false]"]])
  ' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.events.check" || { cat "$scratch/$tag.trace.jsonl" >&2; return 1; }
  jq -s 'sort_by(.kind,.entry)' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.events.json"
}

verify_asset() {
  local tag=$1 key=$2 value=$3 file=$4
  jq -e --arg key "$key" --arg value "$value" --arg sha "$(sha256 "$file")" --argjson bytes "$(wc -c < "$file")" '
    [.[]|select(.[$key]==$value and ($key!="entry" or .kind=="load-guest-js"))] as $events |
      ($events|length)==1 and ($events[0]|.sha256==$sha and .bytes==$bytes)
  ' "$scratch/$tag.events.json"
}

exec 9>/tmp/go-ai/v160-focused-verify.lock
flock 9
run_selected original
printf '\000\007\005audit\000' >> "$scratch/$key/quickjs.wasm"
run_selected control
flock -u 9

for tag in original control; do
  for entry in "$worker" "$key/dist/index.js"; do
    verify_asset "$tag" entry "$entry" "$scratch/$entry" > "$scratch/$tag.js.check"
  done
done
for kind in compile-wasm worker-handoff; do
  verify_asset original kind "$kind" "$scratch/archive.wasm" > "$scratch/$kind.archive.check"
  verify_asset control kind "$kind" "$scratch/$key/quickjs.wasm" > "$scratch/$kind.control-input.check"
  if verify_asset control kind "$kind" "$scratch/archive.wasm" > "$scratch/$kind.control-archive.check"; then
    printf 'Control unexpectedly matched archive at %s\n' "$kind" >&2
    exit 1
  else
    [[ $? == 1 ]] # jq returned false, not an invocation or parse error.
  fi
done

jq -n --arg pin "$pin" --arg node "$(node --version)" --arg vitest "$vitest" --arg test "$test" \
  --arg sha "$(sha256 "$scratch/$test")" --arg selector "$selector" --arg integrity "$integrity" --arg archiveSha "$archive_sha" \
  --slurpfile original "$scratch/original.events.json" --slurpfile control "$scratch/control.events.json" \
  --slurpfile deps "$scratch/copied.json" '
  {format:1,pin:$pin,node:$node,vitest:$vitest,original:{file:$test,sha256:$sha,selector:$selector,passed:1},
    quickjsArchive:{version:"3.6.2",sha256:$archiveSha,integrity:$integrity},observed:$original[0],
    control:{change:"append-valid-wasm-custom-section",originalPassed:1,observed:$control[0],
      comparison:"archive-checks-rejected-at-compile-and-worker-handoff"},copiedDependencies:$deps[0]}'
