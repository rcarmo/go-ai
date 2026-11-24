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

[[ $(uname -s)/$(uname -m) == Linux/aarch64 ]] || { echo 'This receipt needs Linux arm64' >&2; exit 1; }
pin=faac585884ae2c013a6b1400f3e9e77f10b34bac
spec=packages/durable/test/spec-usage.test.ts
writer=packages/durable/src/session/writer.ts
member=packages/durable/src/storage/indexeddb.ts
compiler_package=@typescript/typescript-linux-arm64
scratch=$(mktemp -d "$TMPDIR/spec-usage-types.XXXXXX")
trap 'rm -r -- "$scratch"' EXIT
mkdir -p "$scratch/home" "$scratch/tmp" "$scratch/node_modules"
sha256() { sha256sum -- "$1" | cut -d' ' -f1; }

git -C "$mirror" archive "$pin" | tar -x -C "$scratch"
cd "$scratch"
restored=(@anthropic-ai/sdk partial-json diff)
pinned=("$compiler_package" @types/node undici-types "${restored[@]}")
# Resolve retained declarations read-only; the named packages are freshly
# restored from their compressed lock-verified archives in this scratch tree.
for placement in "$oracle/node_modules"/*; do
  name=${placement##*/}
  case $name in
    partial-json|diff|undici-types) ;;
    @typescript|@types|@anthropic-ai)
      mkdir -- "node_modules/$name"
      for nested in "$placement"/*; do
        key="$name/${nested##*/}"
        case $key in "$compiler_package"|@types/node|@anthropic-ai/sdk) ;; *) ln -s -- "$nested" "node_modules/$key" ;; esac
      done ;;
    *) ln -s -- "$placement" "node_modules/$name" ;;
  esac
done
extract_locked() {
  local name=$1 version integrity hex archive
  read -r version integrity < <(jq -er --arg key "node_modules/$name" '.packages[$key] | [.version,.integrity] | @tsv' package-lock.json)
  [[ $integrity == sha512-* ]]
  hex=$(printf '%s' "${integrity#sha512-}" | base64 -d | od -An -v -tx1 | tr -d ' \n')
  [[ $hex =~ ^[0-9a-f]{128}$ ]]
  archive="$cache/content-v2/sha512/${hex:0:2}/${hex:2:2}/${hex:4}"
  if [[ ! -f $archive ]]; then printf 'Missing retained archive for %s@%s\n' "$name" "$version" >&2; return 1; fi
  [[ sha512-$(openssl dgst -sha512 -binary "$archive" | openssl base64 -A) == "$integrity" ]]
  mkdir -p -- "node_modules/$name"
  tar -xzf "$archive" -C "node_modules/$name" --strip-components=1
  [[ $(jq -er '.version' "node_modules/$name/package.json") == "$version" ]]
  jq -nc --arg name "$name" --arg version "$version" --arg integrity "$integrity" --arg digest "$(sha256 "$archive")" \
    '{package:$name,version:$version,integrity:$integrity,archiveSha256:$digest}'
}
for name in "${pinned[@]}"; do extract_locked "$name"; done > locked-packages.jsonl
compiler="$scratch/node_modules/$compiler_package/lib/tsc"
node_types=node_modules/@types/node/worker_threads.d.ts
cmp -- "$compiler" "$oracle/node_modules/$compiler_package/lib/tsc"
cmp -- "$node_types" "$oracle/$node_types"

# Keep the inherited compiler options; select only the original spec usage
# test and the tracked ambient declarations needed by its import closure.
jq -n --arg spec "$spec" '{extends:"./tsconfig.json",files:[$spec,"packages/ai/src/providers/data-json.d.ts",
  "packages/durable/src/images/photon-wasm.d.ts","packages/codemode/src/cloudflare-modules.d.ts"],include:[]}' > tsconfig.focus.json
jq --arg member "$member" '.files+=[$member]' tsconfig.focus.json > tsconfig.with-member.json
[[ $(head -1 "$member" | tr -d '\r') == '/// <reference lib="dom" />' ]]

exec 9>/tmp/go-ai/v160-focused-verify.lock
flock 9
# The compiler is a native binary. Isolate it from network and credentials.
offline() {
  env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" GOMAXPROCS=2 GOMEMLIMIT=650MiB \
    unshare -Urn timeout 60s "$compiler" "$@"
}
version=$(offline --version)
[[ $version == 'Version 7.0.2' ]]
offline -p tsconfig.json --showConfig > full-options.json
offline -p tsconfig.focus.json --showConfig > focus-options.json
offline -p tsconfig.with-member.json --showConfig > member-options.json
for config in focus member; do
  cmp <(jq -S '.compilerOptions' full-options.json) <(jq -S '.compilerOptions' "$config-options.json")
done
jq -e '.compilerOptions | .lib==["es2024"] and .types==["node"] and .module=="nodenext" and
  .moduleResolution=="nodenext" and .noEmit==true' full-options.json > options.check
jq -e --arg member "./$member" --arg spec "./$spec" '.files | index($member)!=null and index($spec)!=null' full-options.json > roots.check

# Enumerate the unmodified root's graph only; do not check or emit the rest
# of the workspace. Its DOM library is reached even under ES2024 options.
offline -p tsconfig.json --pretty false --listFilesOnly > root.files

tsc() {
  local tag=$1 config=$2 status=0
  offline -p "$config" --pretty false --listFiles > "$tag.log" 2>&1 || status=$?
  if [[ $status != 0 && $status != 1 ]]; then cat "$tag.log" >&2; return "$status"; fi
  printf '%s\n' "$status" > "$tag.status"
  awk '/error TS[0-9]+:/ {print}' "$tag.log" > "$tag.errors"
  awk '/^\// {print}' "$tag.log" > "$tag.files"
}
file_seen() { grep -Fxq -- "$scratch/$2" "$1.files"; }
dom="node_modules/$compiler_package/lib/lib.dom.d.ts"
for entry in "$spec" "$member" "$dom"; do file_seen root "$entry"; done
tsc focus tsconfig.focus.json
[[ $(<focus.status) == 1 ]]
[[ $(<focus.errors) == "$writer(355,17): error TS2339: Property 'onmessage' does not exist on type 'MessagePort'." ]] || { cat focus.errors >&2; exit 1; }
for entry in "$spec" "$writer" "$node_types"; do file_seen focus "$entry"; done
if file_seen focus "$dom"; then echo 'The focused Node-only program unexpectedly included DOM' >&2; exit 1; fi

tsc member tsconfig.with-member.json
[[ $(<member.status) == 0 && ! -s member.errors ]] || { cat member.errors >&2; exit 1; }
for entry in "$spec" "$member" "$dom"; do file_seen member "$entry"; done

# Fault the real expect-type assertion after the unmodified cases ran.
cp -- "$spec" original-spec.ts
[[ $(grep -Fc 'expectTypeOf(examples).returns.toHaveProperty("sequences");' "$spec") == 1 ]]
sed -i 's/expectTypeOf(examples).returns.toHaveProperty("sequences");/expectTypeOf(examples).returns.toHaveProperty("notARealProperty");/' "$spec"
tsc control tsconfig.with-member.json
mv -- original-spec.ts "$spec"
[[ $(<control.status) == 1 && $(wc -l < control.errors) == 1 ]]
expected_control="$spec(641,48): error TS2345: Argument of type '\"notARealProperty\"' is not assignable to parameter of type '\"Approvals\" | \"AskPerson\" | \"ParkingSubagent\" | \"Payment\" | \"TestsAfterEdits\" | \"research\" | \"runCi\" | \"sequences\"'."
[[ $(<control.errors) == "$expected_control" ]] || { cat control.errors >&2; exit 1; }
flock -u 9

jq -n --arg pin "$pin" --arg version "$version" --arg compilerSha "$(sha256 "$compiler")" --arg nodeTypesSha "$(sha256 "$node_types")" \
  --arg domSha "$(sha256 "$dom")" --arg spec "$spec" --arg specSha "$(sha256 "$spec")" \
  --arg writer "$writer" --arg writerSha "$(sha256 "$writer")" --arg member "$member" --arg memberSha "$(sha256 "$member")" \
  --arg rootConfigSha "$(sha256 tsconfig.json)" --arg baseConfigSha "$(sha256 tsconfig.base.json)" --arg lockSha "$(sha256 package-lock.json)" \
  --arg failure "$(<focus.errors)" --arg control "$(<control.errors)" --slurpfile packages locked-packages.jsonl '
  {format:1,pin:$pin,platform:"linux/arm64",typescript:{version:$version,compilerSha256:$compilerSha,domSha256:$domSha},
   nodeTypes:{workerThreadsSha256:$nodeTypesSha},lockedArchives:$packages,
   config:{rootSha256:$rootConfigSha,baseSha256:$baseConfigSha,lockSha256:$lockSha,lib:["es2024"],types:["node"],module:"nodenext",noEmit:true},
   source:{spec:{file:$spec,sha256:$specSha},writer:{file:$writer,sha256:$writerSha},indexedDB:{file:$member,sha256:$memberSha}},
   unchangedRoot:{phase:"list-files-only",containsSpec:true,containsIndexedDB:true,includesDOM:true,typeChecked:false},
   narrowedClosure:{exit:1,includesDOM:false,diagnostic:$failure},
   narrowedWithPinnedIndexedDB:{exit:0,includesDOM:true,diagnostics:[]},
   control:{change:"replace-sequences-in-expect-type-assertion",exit:1,diagnostic:$control}}'
