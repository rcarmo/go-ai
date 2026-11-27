#!/usr/bin/env bash
# Reuse the historical workerd harness with the exact v1.6.1 Git example and published Codemode runtime.
set -euo pipefail
if [[ $# != 3 ]]; then
  printf 'Usage: bash %s <pinned git mirror> <npm _cacache> <npm release archives>\n' "$0" >&2
  exit 2
fi
mirror=$(realpath -- "$1")
cache=$(realpath -- "$2")
releases=$(realpath -- "$3")
here=$(cd -- "$(dirname -- "$0")" && pwd -P)
fixture=$(realpath -- "$here/../../upstream-v1.6.0/dependencies")
old="$fixture/cloudflare-codemode-example.json"
pin=ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe
example=packages/codemode/examples/cloudflare
codemode=node_modules/@earendil-works/pi-codemode
[[ $(uname -s)/$(uname -m) == Linux/aarch64 ]] || { echo 'The locked workerd binary needs Linux arm64' >&2; exit 1; }
# shellcheck source=/dev/null
source "$here/../../../scripts/project-env.sh"
scratch=$(mktemp -d "$TMPDIR/v161-workerd.XXXXXXXX")
trap 'rm -r -- "$scratch"' EXIT
cd "$scratch"
mkdir -p home tmp example "$codemode" node_modules/quickjs-wasi
sha() { sha256sum < "$1" | cut -d ' ' -f 1; }
bytes() { wc -c < "$1" | tr -d ' '; }
sri() { printf 'sha512-%s\n' "$(openssl dgst -sha512 -binary "$1" | openssl base64 -A)"; }

git -C "$mirror" show "$pin:package-lock.json" > lock.json
lock_sha=$(sha lock.json)
jq -e --arg pin "$pin" --arg sha "$lock_sha" 'select(.kind=="manifest") | .pin==$pin and .inputs["package-lock.json"]==$sha' "$here/npm-closure.jsonl" > lock.check
read -r package package_sha package_sri < <(jq -er 'select(.workspace=="packages/codemode" and .version=="1.6.1") | [.file,.sha256,.integrity] | @tsv' "$here/package-archives.jsonl")
[[ $package == earendil-works-pi-codemode-1.6.1.tgz && $(sha "$releases/$package") == "$package_sha" && $(sri "$releases/$package") == "$package_sri" ]]
tar -xzf "$releases/$package" -C "$codemode" --strip-components=1 -- package/dist package/package.json
jq -e '.version=="1.6.1" and .name=="@earendil-works/pi-codemode" and .dependencies["quickjs-wasi"]=="3.6.2"' "$codemode/package.json" > codemode.check

# The current Node staging need not contain workerd. Resolve directly by lock SRI and check the retained receipt.
archive_for() {
  local integrity=$1 hex
  [[ $integrity =~ ^sha512-[A-Za-z0-9+/]{86}==$ ]]
  hex=$(printf '%s' "${integrity#sha512-}" | base64 -d | od -An -v -tx1 | tr -d ' \n')
  printf '%s\n' "$cache/content-v2/sha512/${hex:0:2}/${hex:2:2}/${hex:4}"
}
extract() {
  local key=$1 member=$2 target=$3 prior=$4 version integrity resolved archive
  read -r version integrity resolved < <(jq -er --arg key "$key" '.packages[$key] | [.version,.integrity,.resolved] | @tsv' lock.json)
  archive=$(archive_for "$integrity")
  if [[ ! -f $archive ]]; then echo "Missing retained archive $key@$version ($integrity)" >&2; return 1; fi
  [[ $(sri "$archive") == "$integrity" ]]
  tar -xOzf "$archive" -- "package/$member" > "$target"
  jq -nc --arg key "$key" --arg version "$version" --arg integrity "$integrity" --arg archiveSha "$(sha "$archive")" \
    --arg member "package/$member" --arg memberSha "$(sha "$target")" --argjson bytes "$(bytes "$target")" \
    '{key:$key,version:$version,integrity:$integrity,archiveSha256:$archiveSha,member:$member,sha256:$memberSha,bytes:$bytes}' > "$prior.json"
  cmp <(jq -S "$prior" "$old") <(jq -S . "$prior.json")
  jq --arg resolved "$resolved" '.+{resolved:$resolved}' "$prior.json"
}
extract node_modules/@cloudflare/workerd-linux-arm64 bin/workerd workerd .workerd.binary > engine.json
extract node_modules/workerd workerd.capnp workerd.capnp .workerd.schema > schema.json
extract node_modules/@esbuild/linux-arm64 bin/esbuild esbuild .esbuild.binary > bundler.json
extract node_modules/quickjs-wasi quickjs.wasm quickjs.wasm .quickjsWasm > wasm.json
chmod u+x workerd esbuild
quickjs_archive=$(archive_for "$(jq -r .integrity wasm.json)")
tar -xzf "$quickjs_archive" -C node_modules/quickjs-wasi --strip-components=1 -- package/dist package/package.json

historical_pin=$(jq -er .pin "$old")
git -C "$mirror" diff --quiet "$historical_pin" "$pin" -- "$example"
git -C "$mirror" archive "$pin" "$example" | tar -x -C example --strip-components=4
for path in README.md {agent,sandbox}/{src/index.ts,wrangler.jsonc}; do
  git -C "$mirror" show "$pin:$example/$path" | cmp - "example/$path"
  jq -nc --arg path "$example/$path" --arg sha "$(sha "example/$path")" '{path:$path,sha256:$sha}' >> sources.jsonl
done
sed -n "s/^curl -X POST .* --data '\(.*\)'$/\1/p" example/README.md > request.line
request=$(cat request.line)
[[ -n $request && $request != *$'\n'* && $request == "$(jq -r .example.request "$old")" ]]
printf '%s' "$request" > request.txt
cp -- "$fixture/cloudflare-codemode-probe.capnp" config.capnp
cp -- "$fixture/cloudflare-codemode-sandbox-witness.mjs" sandbox-entry.mjs
cp -- "$fixture/cloudflare-codemode-example-test.mjs" test.mjs
[[ $(sha config.capnp) == "$(jq -r .harness.configSha256 "$old")" ]]

# The historical harness calls internal service bindings, with no listener or deployment.
offline() { env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" unshare -Urn "$@"; }
exec 9>/tmp/go-ai/v160-focused-verify.lock
flock 9
workerd=$(offline ./workerd --version)
esbuild=$(offline ./esbuild --version)
[[ $workerd == 'workerd 2026-10-23' && $esbuild == '0.28.2' ]]
offline ./esbuild example/agent/src/index.ts --bundle --format=esm --platform=browser '--external:cloudflare:*' --outfile=agent.mjs
offline ./esbuild sandbox-entry.mjs --bundle --format=esm --platform=browser '--external:cloudflare:*' --external:quickjs-wasi/quickjs.wasm --outfile=sandbox.mjs
run() {
  local tag=$1
  if ! offline timeout 25s ./workerd test config.capnp 'probe:*' > "$tag.log" 2>&1; then cat "$tag.log" >&2; return 1; fi
  [[ $(grep -c 'CODEMODE_WITNESS ' "$tag.log") == 1 ]]
  sed -n 's/^.*CODEMODE_WITNESS //p' "$tag.log" > "$tag.json"
}
run original
cmp <(jq -S .original "$old") <(jq -S . original.json)
# The same functional result survives a valid custom section; the module witness must see that section.
printf '\000\007\005audit\000' >> quickjs.wasm
run control
jq -e '.module.auditSections==[[0]]' control.json > control.check
cmp <(jq -S 'del(.module.auditSections)' original.json) <(jq -S 'del(.module.auditSections)' control.json)
[[ $(sha quickjs.wasm) != "$(jq -r .sha256 wasm.json)" ]]
flock -u 9

jq -n --arg pin "$pin" --arg prior "$historical_pin" --arg lockSha "$lock_sha" --arg request "$request" \
  --arg package "$package" --arg packageSha "$package_sha" --arg packageSri "$package_sri" --arg workerd "$workerd" --arg esbuild "$esbuild" \
  --arg config "$(sha config.capnp)" --arg test "$(sha test.mjs)" --arg witness "$(sha sandbox-entry.mjs)" \
  --arg agentBundle "$(sha agent.mjs)" --arg sandboxBundle "$(sha sandbox.mjs)" --arg controlSha "$(sha quickjs.wasm)" \
  --slurpfile sources sources.jsonl --slurpfile engine engine.json --slurpfile schema schema.json --slurpfile bundler bundler.json \
  --slurpfile wasm wasm.json --slurpfile original original.json --slurpfile control control.json '
  {pin:$pin,lockSHA256:$lockSha,platform:"linux/arm64",archiveMembersMatchHistoricalReceipt:true,
   engineAvailability:{workerd:{version:$workerd,binary:$engine[0],schema:$schema[0]},esbuild:{version:$esbuild,binary:$bundler[0]}},
   quickjsWasm:$wasm[0],codemodeArchive:{file:$package,sha256:$packageSha,integrity:$packageSri},
   fixture:{historicalPin:$prior,exampleFilesIdenticalAcrossPins:true,sourceFiles:$sources,request:$request,
     configSHA256:$config,testSHA256:$test,witnessSHA256:$witness,agentBundleSHA256:$agentBundle,sandboxBundleSHA256:$sandboxBundle},
   execution:{command:"workerd test config.capnp probe:*",network:"private network namespace; internal bindings only",observed:$original[0]},
   control:{change:"append valid audit WASM custom section in scratch",archiveWasmMatches:false,sha256:$controlSha,observed:$control[0]},
   scope:"Pinned two-Worker example with published v1.6.1 Codemode; no Wrangler, hosted deployment, Durable test-workers suite, or native Go parity claim."}'
