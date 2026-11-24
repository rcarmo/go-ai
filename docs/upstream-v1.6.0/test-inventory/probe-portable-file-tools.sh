#!/usr/bin/env bash
set -euo pipefail
if [[ $# != 3 && ( $# != 4 || ( $4 != linux-bash && $4 != mock-powershell && $4 != edit-paths && $4 != delayed-abort ) ) ]]; then
  printf 'Usage: bash %s <pinned-git-mirror> <retained-oracle-dir> <release-archive-dir> [linux-bash|mock-powershell|edit-paths|delayed-abort]\n' "$0" >&2; exit 1
fi
here=$(cd -- "$(dirname -- "$0")" && pwd -P)
mirror=$(realpath -- "$1")
oracle=$(realpath -- "$2")
releases=$(realpath -- "$3")
pin=faac585884ae2c013a6b1400f3e9e77f10b34bac
base=3bfd09d6e52cf47b528d33c2e67717fe269cf996
mode=${4:-file}
[[ "$mode" != linux-bash ]] || base=bb6f354b08e8415f09a1c92f492a49f0d5f0bdc5
[[ "$mode" != mock-powershell ]] || base=a0fa5814f398b932edd57ede595a0eecd30a51f5
[[ "$mode" != edit-paths ]] || base=ebe8ec7ab0a1f2da0a1641a9b63c633a48f1a707
[[ "$mode" != delayed-abort ]] || base=c82245710d014e35a2a3c1c6e78f865f5410a726
file=packages/durable/test/tools.test.ts
adapter=packages/durable/src/tools/read.ts
[[ "$mode" == file ]] || adapter=packages/durable/src/tools/bash.ts
queue=docs/upstream-v1.6.0/test-inventory/primary-queue-r21.jsonl
scratch=$(mktemp -d /tmp/v160-portable-file-tools.XXXXXX)
trap 'rm -r -- "$scratch"' EXIT
mkdir -p "$scratch/home" "$scratch/tmp" "$scratch/node_modules/@earendil-works"
sha256() { sha256sum -- "$1" | cut -d' ' -f1; }
for ledger in npm-closure.jsonl provider-catalog-assets.jsonl; do
  git -C "$here" show "$base:docs/upstream-v1.6.0/dependencies/$ledger" > "$scratch/$ledger"
done
lock_sha=$(git -C "$mirror" show "$pin:package-lock.json" | sha256sum | cut -d' ' -f1)
git -C "$mirror" archive "$pin" package.json packages/{ai,chord,codemode,durable,schema,telemetry}/{package.json,src} \
  packages/durable/vitest.config.ts packages/durable/test | tar -x -C "$scratch"
read -r ai_file ai_sha < <(jq -er --arg pin "$pin" --arg sha "$lock_sha" '
  select(.kind=="manifest" and .pin==$pin and .lockSha256==$sha) | .archive | [.file,.sha256] | @tsv
' "$scratch/provider-catalog-assets.jsonl")
[[ $ai_file == earendil-works-pi-ai-1.6.0.tgz && $(sha256 "$releases/$ai_file") == "$ai_sha" ]]
tar -xzf "$releases/$ai_file" -C "$scratch/packages/ai" --strip-components=1 package/dist
libc=$(node -p "process.report.getReport().header.glibcVersionRuntime ? 'glibc' : 'musl'")
bun run "$here/../dependencies/copy-openai-probe-deps.ts" "$scratch/npm-closure.jsonl" "$lock_sha" "$oracle" "$scratch" "$libc" \
  node_modules/{vitest,diff,quickjs-wasi,partial-json} > "$scratch/copied.json"
for p in chord pi-schema pi-telemetry pi-codemode pi-ai pi-durable; do
  ln -s "../../packages/${p#pi-}" "$scratch/node_modules/@earendil-works/$p"
done
vitest=$(jq -er '.version' "$scratch/node_modules/vitest/package.json")
[[ $vitest == 4.1.12 ]]
sources=("$file" "$adapter" packages/durable/{vitest.config.ts,test/setup-document-retention.ts,src/{env/node.ts,tools/{write,edit,file-mutation-queue,env,path-utils}.ts,truncate.ts}})
if [[ "$mode" != file ]]; then
  sources+=(packages/durable/src/tools/read.ts)
  [[ $(uname -s) == Linux && -x /bin/bash ]]
  # shellcheck source=offline-loopback.sh
  source "$here/offline-loopback.sh"
  prepare_offline_loopback "$scratch"
fi
check_sources() {
  local source
  for source in "${sources[@]}"; do git -C "$mirror" show "$pin:$source" | cmp "$scratch/$source" -; done
}
check_sources
if [[ "$mode" == edit-paths || "$mode" == delayed-abort ]]; then
  adapter=packages/durable/src/tools/file-mutation-queue.ts
fi
file_sha=$(sha256 "$scratch/$file")
git -C "$here" show "$base:$queue" | jq -e --arg file "$file" --arg sha "$file_sha" '
  select(.kind=="suite" and .path==$file) | .upstream.sha256==$sha and .original.status=="none-found"
' > "$scratch/queue.check"
if [[ "$mode" == linux-bash ]]; then
  jq -n '[
    "passes the retained window to the environment and forwards what it skipped",
    "streams combined stdout and stderr, returns no output of its own, and repeats it with the exit code",
    "answers a nonzero exit with an error result, and throws on a timeout, after streaming the output",
    "reports the spill of a command that times out",
    "prepares command, cwd, and an explicit environment with the call\u0027s api",
    "supports command prefixes",
    "streams every byte and spills complete output beyond the default limits",
    "does not spill output within the limits"
  ]|map("durable tools bash "+.)' > "$scratch/cases.json"
  failed='durable tools bash answers a nonzero exit with an error result, and throws on a timeout, after streaming the output'
  change='add one to the reported Bash exit code diagnostic in scratch'
  scope=isolated-linux-bash
  expected_error='tools.test.ts:666:'
  pending=28
elif [[ "$mode" == mock-powershell ]]; then
  jq -n '[
    "runs the command with pwsh as one argument, forcing UTF-8 output",
    "falls back to Windows PowerShell and reports the last start failure",
    "answers a nonzero exit with an error result carrying the exit code"
  ]|map("durable tools powershell "+.)' > "$scratch/cases.json"
  failed='durable tools powershell runs the command with pwsh as one argument, forcing UTF-8 output'
  change='replace the PowerShell UTF-8 console suffix in scratch argv construction'
  scope=isolated-mocked-powershell
  expected_error='tools.test.ts:577:'
  pending=33
elif [[ "$mode" == edit-paths ]]; then
  jq -n '[
    "serializes concurrent edits through canonical and symlink paths",
    "serializes edits of one file across environment objects of one file system",
    "serializes a new file created through a symlinked directory with its canonical path",
    "keys a missing file whose name contains a backslash like the created file",
    "does not serialize the same path on different file systems",
    "edits regular files through symlinks"
  ]|map("durable tools edit "+.)' > "$scratch/cases.json"
  failed='durable tools edit keys a missing file whose name contains a backslash like the created file'
  change='split the missing-file basename on a literal backslash in the scratch queue helper'
  scope=isolated-linux-edit-paths
  expected_error='tools.test.ts:513:'
  pending=30
elif [[ "$mode" == delayed-abort ]]; then
  jq -n '[
    "durable tools write keeps the mutation queue locked until an aborted write settles",
    "durable tools edit keeps the mutation queue locked until an aborted edit write settles"
  ]' > "$scratch/cases.json"
  change='release the scratch queue before awaiting the admitted mutation'
  scope=isolated-delayed-abort-write-edit
  pending=34
else
  jq -n '[
    "fail with an ordinary error when no environment is configured",
    "read reads text with offsets and limits and reports continuation as a diagnostic",
    "read truncates large text by line count",
    "read does not count a trailing newline as an extra line at the truncation limit",
    "read shows the start of a line longer than the byte limit",
    "read rejects offsets beyond the file",
    "read reads a log that grows while it is read",
    "write writes files and creates parent directories",
    "edit applies disjoint edits and returns both diff formats",
    "edit repairs edits sent as a JSON string, a single object, or top-level oldText/newText without mutating them",
    "edit matches all edits against the original and rejects overlaps",
    "edit rejects missing and duplicate target text",
    "edit preserves BOM and CRLF line endings"
  ] | map("durable tools "+.)' > "$scratch/cases.json"
  failed='durable tools read reads text with offsets and limits and reports continuation as a diagnostic'
  change='add one to the reported continuation offset in scratch'
  scope=portable-text-read-write-edit
  expected_error='offset=62'
  pending=23
fi
if [[ "$mode" == delayed-abort ]]; then
  failures=$(jq -c '{(.[0]):"tools.test.ts:345:",(.[1]):"tools.test.ts:438:"}' "$scratch/cases.json")
else
  failures=$(jq -nc --arg name "$failed" --arg error "$expected_error" '{($name):$error}')
fi
count=$(jq -er 'length' "$scratch/cases.json")
selector=$(jq -er 'select(all(.[]; test("^[a-zA-Z0-9 ,/\u0027-]+$"))) | "^("+join("|")+")$"' "$scratch/cases.json")
# Node's symlink API requires wildcard FS permission even for a scratch-only
# link. The edit selection instead limits host filesystem access by mounting
# scratch and read-only system runtime files into an otherwise empty root.
run_edit_sandbox() {
  bwrap --unshare-user --unshare-net --unshare-pid --unshare-ipc --die-with-parent --cap-drop ALL \
    --ro-bind /usr /usr --symlink usr/lib /lib --symlink usr/lib64 /lib64 --symlink usr/bin /bin \
    --proc /proc --dev /dev --dir /tmp --bind "$scratch" "$scratch" --chdir "$PWD" \
    --clearenv --setenv PATH /usr/local/bin:/usr/bin:/bin --setenv HOME "$scratch/home" --setenv TMPDIR "$scratch/tmp" \
    -- "$@"
}
run_env() {
  if [[ "$mode" == edit-paths ]]; then
    run_edit_sandbox env "$@"
  elif [[ "$mode" != file ]]; then
    run_offline_loopback "$scratch" env "$@"
  else
    env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" "$@"
  fi
}
run() {
  local tag=$1 fs_scope=$scratch
  [[ "$mode" != edit-paths ]] || fs_scope='*'
  (cd "$scratch/packages/durable" && run_env \
    NODE_OPTIONS="--max-old-space-size=768 --permission --allow-fs-read=$fs_scope --allow-fs-write=$fs_scope --allow-child-process --allow-worker --allow-addons" \
    node ../../node_modules/vitest/vitest.mjs run test/tools.test.ts -t "$selector" --pool=forks --maxWorkers=1 \
    --reporter=json --outputFile "$scratch/$tag.json") > "$scratch/$tag.log" 2>&1
}
show_failure() {
  local tag=$1
  cat "$scratch/$tag.log" >&2
  jq -c '[.testResults[]|{message,failed:[.assertionResults[]|select(.status=="failed")|{fullName,failureMessages}]}]' "$scratch/$tag.json" >&2 || true
}
exec 9>/tmp/go-ai/v160-focused-verify.lock
flock 9
if [[ "$mode" == edit-paths ]]; then
  cat > "$scratch/check-edit-sandbox.sh" <<'SH'
set -euo pipefail
[[ ! -e /work && ! -e /home ]]
/usr/sbin/ip -j link show | jq -e '[.[].ifname]==["lo"]'
for family in -4 -6; do
  /usr/sbin/ip "$family" -j route show table all | jq -e 'all(.[]; .dev=="lo")'
done
node ./check-netns.mjs
SH
  (cd "$scratch" && run_edit_sandbox bash ./check-edit-sandbox.sh) > "$scratch/sandbox.check"
elif [[ "$mode" != file ]]; then
  run_offline_loopback "$scratch" node "$scratch/check-netns.mjs"
fi
if ! run original; then show_failure original; exit 1; fi
jq -e --slurpfile cases "$scratch/cases.json" --argjson count "$count" --argjson pending "$pending" '
  .numPassedTests==$count and .numFailedTests==0 and .numPendingTests==$pending and .numTodoTests==0 and
  ([.testResults[].assertionResults[]|select(.status!="skipped")|[.fullName,.status]]==($cases[0]|map([.,"passed"]))) and
  ([.testResults[].assertionResults[]|.fullName]|unique|length)==36
' "$scratch/original.json" > "$scratch/original.check"
# These are literal TypeScript template expressions, not shell variables.
if [[ "$mode" == linux-bash ]]; then
  # shellcheck disable=SC2016
  [[ $(grep -Fc 'Command exited with code ${exitCode}' "$scratch/$adapter") == 1 ]]
  # shellcheck disable=SC2016
  sed -i '/Command exited with code/s/${exitCode}/${exitCode + 1}/' "$scratch/$adapter"
elif [[ "$mode" == mock-powershell ]]; then
  [[ $(grep -Fc '[System.Text.Encoding]::UTF8' "$scratch/$adapter") == 1 ]]
  sed -i 's/\[System.Text.Encoding\]::UTF8/[System.Text.Encoding]::ASCII/' "$scratch/$adapter"
elif [[ "$mode" == edit-paths ]]; then
  [[ $(grep -Fc 'const name = absolutePath.slice' "$scratch/$adapter") == 1 ]]
  sed -i '/const name = absolutePath.slice/s/;$/\.split("\\\\").at(-1)!;/' "$scratch/$adapter"
elif [[ "$mode" == delayed-abort ]]; then
  [[ $(grep -Fc 'await previous;' "$scratch/$adapter") == 1 ]]
  sed -i '/await previous;/a\	 release();' "$scratch/$adapter"
else
  # shellcheck disable=SC2016
  [[ $(grep -Fc 'more lines in file. Use offset=${nextOffset}' "$scratch/$adapter") == 1 ]]
  # shellcheck disable=SC2016
  sed -i '/more lines in file\. Use offset=/s/${nextOffset}/${nextOffset + 1}/' "$scratch/$adapter"
fi
if run mutated; then printf 'Unexpected pass: %s\n' "$change" >&2; exit 1; fi
if ! jq -e --argjson failures "$failures" --argjson count "$count" --argjson pending "$pending" --slurpfile original "$scratch/original.json" '
  .numPassedTests==($count-($failures|length)) and .numFailedTests==($failures|length) and .numPendingTests==$pending and .numTodoTests==0 and
  ([.testResults[].assertionResults[]|[.fullName,.status]]==[$original[0].testResults[].assertionResults[]|[.fullName,if $failures[.fullName] then "failed" else .status end]]) and
  all(.testResults[].assertionResults[]|select(.status=="failed"); $failures[.fullName] as $loc | (.failureMessages|join("\n")|contains($loc)))
' "$scratch/mutated.json" > "$scratch/control.check"; then show_failure mutated; exit 1; fi
flock -u 9
git -C "$mirror" show "$pin:$adapter" > "$scratch/$adapter"
check_sources
for source in "${sources[@]}"; do
  jq -cn --arg file "$source" --arg sha "$(sha256 "$scratch/$source")" '{file:$file,sha256:$sha}'
done > "$scratch/sources.jsonl"
jq -n --arg pin "$pin" --arg base "$base" --arg node "$(node --version)" --arg vitest "$vitest" \
  --arg file "$file" --arg selector "$selector" --arg aiSha "$ai_sha" --argjson failures "$failures" --arg queue "$queue" \
  --arg change "$change" --arg scope "$scope" --arg mode "$mode" --argjson count "$count" \
  --slurpfile cases "$scratch/cases.json" --slurpfile report "$scratch/original.json" --slurpfile sources "$scratch/sources.jsonl" \
  --slurpfile copied "$scratch/copied.json" '
  {format:1,pin:$pin,evidenceBase:$base,node:$node,vitest:$vitest,
   selection:{file:$file,selector:$selector,caseNames:$cases[0],filteredOut:[$report[0].testResults[].assertionResults[]|select(.status=="skipped")|.fullName]},
   result:{passed:$count,failed:0,selectedSkipped:0},sourceDigests:$sources,aiArchiveSha256:$aiSha,copiedDependencies:$copied[0],
   control:{change:$change,failed:($cases[0]|map(select($failures[.]))),passed:($cases[0]|map(select($failures[.]|not)))},
   priorQueue:{file:$queue,commit:$base},exclusions:[],scope:$scope} +
   (if $mode!="file" then {isolation:{interfaces:["lo"],egressProbe:{ipv4:"ENETUNREACH",ipv6:"ENETUNREACH"},nodeNetworkPermission:false}} else {} end) +
   (if $mode=="edit-paths" then {filesystemIsolation:{mechanism:"Bubblewrap mount namespace",writableHostBind:"scratch only",worktreeMounted:false}} else {} end)'
