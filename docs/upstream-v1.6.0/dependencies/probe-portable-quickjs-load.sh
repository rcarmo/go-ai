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
export CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1
export GOTOOLCHAIN=go1.27.3 GOPROXY=off GOSUMDB=off GOWORK=off GOVCS='*:off'

pin=faac585884ae2c013a6b1400f3e9e77f10b34bac
native=TestSourceV160CodemodePublishedWorkerNamespacedGlobals
native_file=durable/codemode_worker_published_v160_test.go
bridge=durable/testdata/worker-remote-120.mjs
upstream=packages/codemode/test/sandbox.test.ts
codemode=node_modules/@earendil-works/pi-codemode
quickjs=node_modules/quickjs-wasi
scratch=$(mktemp -d "$TMPDIR/portable-quickjs-load.XXXXXX")
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
cmp "$scratch/archive.wasm" internal/quickjs/guest/quickjs.wasm
cp -- "$here/portable-quickjs-witness.mjs" "$scratch/witness.mjs"

run_selected() {
  local tag=$1
  (
    cd durable
    env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" \
      GOAI_CODEMODE_V160_PACKAGE_ROOT="$scratch" PORTABLE_QUICKJS_TRACE="$scratch/$tag.trace.jsonl" \
      NODE_OPTIONS="--permission --allow-fs-read=$scratch --allow-fs-read=$PWD/$bridge_rel --allow-fs-write=$scratch --allow-wasi --import=$scratch/witness.mjs" \
      "$scratch/native.test" -test.run "^$native$" -test.timeout 110s -test.v
  ) > "$scratch/$tag.log" 2>&1 || { cat "$scratch/$tag.log" >&2; return 1; }
  [[ $(grep -Ec -- "^--- PASS: $native \\([0-9.]+s\\)$" "$scratch/$tag.log") == 1 ]]
  jq -es --arg portable "$codemode/dist/portable.js" --arg loader "$codemode/dist/wasm.js" --arg runtime "$quickjs/dist/index.js" '
    ([.[] | select(.kind=="load-js") | .entry] | sort)==([$portable,$loader,$runtime] | sort) and
    ([.[] | select(.kind!="load-js")] | map(.kind))==["compile-wasm","instantiate-wasm"] and
    ([.[] | select(.kind!="load-js") | .caller])==[$loader,$runtime] and length==5
  ' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.events.check" || { cat "$scratch/$tag.trace.jsonl" >&2; return 1; }
  jq -s 'sort_by(.kind,.entry)' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.events.json"
}

verify_asset() {
  local tag=$1 key=$2 value=$3 file=$4
  jq -e --arg key "$key" --arg value "$value" --arg sha "$(sha256 "$file")" --argjson bytes "$(wc -c < "$file")" '
    [.[]|select(.[$key]==$value)] as $events | ($events|length)==1 and ($events[0]|.sha256==$sha and .bytes==$bytes)
  ' "$scratch/$tag.events.json"
}

bridge_rel=${bridge#durable/}
exec 9>/tmp/go-ai/v160-focused-verify.lock
flock 9
[[ $(go env GOVERSION) == go1.27.3 ]]
go test -mod=readonly -c -o "$scratch/native.test" ./durable
run_selected original
printf '\000\007\005audit\000' >> "$scratch/$quickjs/quickjs.wasm"
run_selected control
platform="$(go env GOOS)/$(go env GOARCH)"
flock -u 9

for tag in original control; do
  for entry in "$codemode/dist/portable.js" "$codemode/dist/wasm.js" "$quickjs/dist/index.js"; do
    verify_asset "$tag" entry "$entry" "$scratch/$entry" > "$scratch/$tag.js.check"
  done
done
for kind in compile-wasm instantiate-wasm; do
  verify_asset original kind "$kind" "$scratch/archive.wasm" > "$scratch/$kind.archive.check"
  verify_asset control kind "$kind" "$scratch/$quickjs/quickjs.wasm" > "$scratch/$kind.control-input.check"
  if verify_asset control kind "$kind" "$scratch/archive.wasm" > "$scratch/$kind.control-archive.check"; then
    printf 'Control unexpectedly matched archive at %s\n' "$kind" >&2
    exit 1
  else
    [[ $? == 1 ]] # jq evaluated false; errors do not count as a rejection.
  fi
done

jq -n --arg pin "$pin" --arg node "$(node --version)" --arg platform "$platform" --arg native "$native" \
  --arg nativeFile "$native_file" --arg nativeSha "$(sha256 "$native_file")" --arg bridge "$bridge" --arg bridgeSha "$(sha256 "$bridge")" \
  --arg upstream "$upstream" --arg upstreamSha "$(sha256 "$scratch/source-test.ts")" --arg codemodeFile "$codemode_file" \
  --arg codemodeSha "$codemode_sha" --arg quickjsSha "$quickjs_sha" --arg integrity "$integrity" \
  --slurpfile original "$scratch/original.events.json" --slurpfile control "$scratch/control.events.json" '
  {format:1,pin:$pin,node:$node,go:"go1.27.3",cgoEnabled:false,platform:$platform,
    upstreamReference:{file:$upstream,sha256:$upstreamSha,lines:"506-523",execution:"not-rerun-by-this-probe"},
    native:{test:$native,file:$nativeFile,sha256:$nativeSha,bridge:$bridge,bridgeSha256:$bridgeSha,passed:1},
    codemodeArchive:{file:$codemodeFile,sha256:$codemodeSha},
    quickjsArchive:{version:"3.6.2",sha256:$quickjsSha,integrity:$integrity},observed:$original[0],
    control:{change:"append-valid-wasm-custom-section",nativePassed:1,observed:$control[0],
      comparison:"archive-checks-rejected-at-compile-and-instantiation"}}'
