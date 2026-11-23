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

[[ $(uname -s)/$(uname -m) == Linux/aarch64 ]] || { echo 'This receipt needs Linux arm64' >&2; exit 1; }
pin=faac585884ae2c013a6b1400f3e9e77f10b34bac
example=packages/codemode/examples/cloudflare
codemode=node_modules/@earendil-works/pi-codemode
scratch=$(mktemp -d "$TMPDIR/cloudflare-codemode.XXXXXX")
trap 'rm -r -- "$scratch"' EXIT
mkdir -p "$scratch/home" "$scratch/tmp" "$scratch/example" "$scratch/$codemode" "$scratch/node_modules/quickjs-wasi"
sha256() { sha256sum -- "$1" | cut -d' ' -f1; }

git -C "$mirror" show "$pin:package-lock.json" > "$scratch/lock.json"
lock_sha=$(sha256 "$scratch/lock.json")
jq -e --arg pin "$pin" --arg sha "$lock_sha" 'select(.kind=="manifest") | .pin==$pin and .inputs["package-lock.json"]==$sha' \
  "$here/npm-closure.jsonl" > "$scratch/lock.check"
read -r codemode_file codemode_sha < <(jq -er --arg key "$codemode" '
  select(.kind=="workspace" and .linkKey==$key and .version=="1.6.0" and .archive.proof=="bytes-match-release-receipt")
  | .archive | [.file,.sha256] | @tsv' "$here/npm-closure.jsonl")
[[ $codemode_file == earendil-works-pi-codemode-1.6.0.tgz && $(sha256 "$releases/$codemode_file") == "$codemode_sha" ]]
tar -xzf "$releases/$codemode_file" -C "$scratch/$codemode" --strip-components=1 -- package/dist package/package.json

# Resolve each archive directly from its lock integrity. Compare its selected
# member to the prepared Oracle before executing or embedding it.
extract_locked() {
  local key=$1 member=$2 target=$3 version integrity hex archive
  read -r version integrity < <(jq -er --arg key "$key" '.packages[$key] | [.version,.integrity] | @tsv' "$scratch/lock.json")
  [[ $integrity == sha512-* ]]
  hex=$(printf '%s' "${integrity#sha512-}" | base64 -d | od -An -v -tx1 | tr -d ' \n')
  [[ $hex =~ ^[0-9a-f]{128}$ ]]
  archive="$cache/content-v2/sha512/${hex:0:2}/${hex:2:2}/${hex:4}"
  if [[ ! -f $archive ]]; then printf 'Missing retained archive for %s@%s\n' "$key" "$version" >&2; return 1; fi
  [[ sha512-$(openssl dgst -sha512 -binary "$archive" | openssl base64 -A) == "$integrity" ]]
  tar -xOzf "$archive" -- "package/$member" > "$scratch/$target"
  cmp "$scratch/$target" "$oracle/$key/$member"
  jq -nc --arg key "$key" --arg version "$version" --arg integrity "$integrity" --arg archiveSha "$(sha256 "$archive")" \
    --arg member "package/$member" --arg memberSha "$(sha256 "$scratch/$target")" --argjson bytes "$(wc -c < "$scratch/$target")" \
    '{key:$key,version:$version,integrity:$integrity,archiveSha256:$archiveSha,member:$member,sha256:$memberSha,bytes:$bytes}'
}
extract_locked node_modules/@cloudflare/workerd-linux-arm64 bin/workerd workerd > "$scratch/workerd.json"
extract_locked node_modules/workerd workerd.capnp workerd.capnp > "$scratch/schema.json"
extract_locked node_modules/@esbuild/linux-arm64 bin/esbuild esbuild > "$scratch/esbuild.json"
extract_locked node_modules/quickjs-wasi quickjs.wasm quickjs.wasm > "$scratch/wasm.json"
extract_locked node_modules/quickjs-wasi package.json node_modules/quickjs-wasi/package.json > "$scratch/quickjs-manifest.json"
chmod u+x "$scratch/workerd" "$scratch/esbuild"
cp -- "$scratch/quickjs.wasm" "$scratch/archive.wasm"
# The wasm import is external to the bundle, but the QuickJS JS runtime is not.
# Extract the remaining archived JS modules without resolving package installs.
quickjs_integrity=$(jq -r '.integrity' "$scratch/wasm.json")
hex=$(printf '%s' "${quickjs_integrity#sha512-}" | base64 -d | od -An -v -tx1 | tr -d ' \n')
tar -xzf "$cache/content-v2/sha512/${hex:0:2}/${hex:2:2}/${hex:4}" -C "$scratch/node_modules/quickjs-wasi" \
  --strip-components=1 -- package/dist

git -C "$mirror" archive "$pin" "$example" | tar -x -C "$scratch/example" --strip-components=4
request=$(sed -n "s/^curl -X POST .* --data '\(.*\)'$/\1/p" "$scratch/example/README.md")
[[ -n $request && $request != *$'\n'* ]]
printf '%s' "$request" > "$scratch/request.txt"
cp -- "$here/cloudflare-codemode-probe.capnp" "$scratch/config.capnp"
cp -- "$here/cloudflare-codemode-sandbox-witness.mjs" "$scratch/sandbox-entry.mjs"
cp -- "$here/cloudflare-codemode-example-test.mjs" "$scratch/test.mjs"

# No HTTP listener or network is required: the test calls the agent's internal
# service binding. A new network namespace also blocks accidental outbound IO.
unshare -Urn true
exec 9>/tmp/go-ai/v160-focused-verify.lock
flock 9
cd "$scratch"
offline() { env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" unshare -Urn "$@"; }
workerd_version=$(offline ./workerd --version)
esbuild_version=$(offline ./esbuild --version)
[[ $workerd_version == 'workerd 2026-10-23' && $esbuild_version == '0.28.2' ]]
offline ./esbuild example/agent/src/index.ts --bundle --format=esm --platform=browser \
  '--external:cloudflare:*' --outfile=agent.mjs
offline ./esbuild sandbox-entry.mjs --bundle --format=esm --platform=browser \
  '--external:cloudflare:*' --external:quickjs-wasi/quickjs.wasm --outfile=sandbox.mjs

run() {
  local tag=$1
  if ! offline timeout 25s ./workerd test config.capnp 'probe:*' > "$tag.log" 2>&1; then cat "$tag.log" >&2; return 1; fi
  [[ $(grep -c 'CODEMODE_WITNESS ' "$tag.log") == 1 ]]
  sed -n 's/^.*CODEMODE_WITNESS //p' "$tag.log" > "$tag.json"
  jq -e '.status==200 and .value==2 and .output==[{type:"text",text:"n=2"}] and .calls==[{name:"count",status:"ok"}] and .module.exports==450' \
    "$tag.json" > "$tag.behavior.check"
}
run original
jq -e '.module.auditSections==[]' original.json > original.module.check
printf '\000\007\005audit\000' >> quickjs.wasm
run control
jq -e '.module.auditSections==[[0]]' control.json > control.module.check
flock -u 9
if cmp -s archive.wasm quickjs.wasm; then
  echo 'Control unexpectedly matched the archive' >&2
  exit 1
else
  [[ $? == 1 ]] # A byte difference, not a comparison error.
fi
cmp <(jq -c 'del(.module.auditSections)' original.json) <(jq -c 'del(.module.auditSections)' control.json)

jq -n --arg pin "$pin" --arg example "$example" --arg codemode "$codemode_sha" --arg request "$request" \
  --arg agent "$(sha256 example/agent/src/index.ts)" --arg sandbox "$(sha256 example/sandbox/src/index.ts)" \
  --arg config "$(sha256 config.capnp)" --arg agentBundle "$(sha256 agent.mjs)" --arg sandboxBundle "$(sha256 sandbox.mjs)" \
  --arg workerd "$workerd_version" --arg esbuild "$esbuild_version" --arg changedWasm "$(sha256 quickjs.wasm)" \
  --slurpfile runtime workerd.json --slurpfile schema schema.json --slurpfile bundler esbuild.json --slurpfile wasm wasm.json \
  --slurpfile original original.json --slurpfile control control.json '
  {format:1,pin:$pin,platform:"linux/arm64",example:{directory:$example,agentSha256:$agent,sandboxSha256:$sandbox,
     request:$request,execution:"unchanged-example-source-under-local-workerd-test-harness"},
   harness:{configSha256:$config,agentBundleSha256:$agentBundle,sandboxBundleSha256:$sandboxBundle},
   workerd:{version:$workerd,binary:$runtime[0],schema:$schema[0]},esbuild:{version:$esbuild,binary:$bundler[0]},
   codemodeArchiveSha256:$codemode,quickjsWasm:$wasm[0],original:$original[0],
   control:{change:"append-valid-wasm-custom-section",sha256:$changedWasm,observed:$control[0]}}'
