#!/usr/bin/env bash
set -euo pipefail
if [[ $# != 3 && ( $# != 4 || ( $4 != linux-bash && $4 != mock-powershell ) ) ]]; then
  printf 'Usage: bash %s <pinned-git-mirror> <retained-oracle-dir> <release-archive-dir> [linux-bash|mock-powershell]\n' "$0" >&2; exit 1
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
count=$(jq -er 'length' "$scratch/cases.json")
selector=$(jq -er 'select(all(.[]; test("^[a-zA-Z0-9 ,/\u0027-]+$"))) | "^("+join("|")+")$"' "$scratch/cases.json")
run_env() {
  if [[ "$mode" != file ]]; then
    run_offline_loopback "$scratch" env "$@"
  else
    env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" "$@"
  fi
}
run() {
  local tag=$1
  (cd "$scratch/packages/durable" && run_env \
    NODE_OPTIONS="--max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons" \
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
if [[ "$mode" != file ]]; then
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
else
  # shellcheck disable=SC2016
  [[ $(grep -Fc 'more lines in file. Use offset=${nextOffset}' "$scratch/$adapter") == 1 ]]
  # shellcheck disable=SC2016
  sed -i '/more lines in file\. Use offset=/s/${nextOffset}/${nextOffset + 1}/' "$scratch/$adapter"
fi
if run mutated; then printf 'Unexpected pass: %s\n' "$change" >&2; exit 1; fi
if ! jq -e --arg failed "$failed" --arg expectedError "$expected_error" --argjson count "$count" --argjson pending "$pending" --slurpfile original "$scratch/original.json" '
  .numPassedTests==($count-1) and .numFailedTests==1 and .numPendingTests==$pending and .numTodoTests==0 and
  ([.testResults[].assertionResults[]|[.fullName,.status]]==[$original[0].testResults[].assertionResults[]|[.fullName,if .fullName==$failed then "failed" else .status end]]) and
  any(.testResults[].assertionResults[]; .fullName==$failed and (.failureMessages|join("\n")|contains($expectedError)))
' "$scratch/mutated.json" > "$scratch/control.check"; then show_failure mutated; exit 1; fi
flock -u 9
git -C "$mirror" show "$pin:$adapter" > "$scratch/$adapter"
check_sources
for source in "${sources[@]}"; do
  jq -cn --arg file "$source" --arg sha "$(sha256 "$scratch/$source")" '{file:$file,sha256:$sha}'
done > "$scratch/sources.jsonl"
jq -n --arg pin "$pin" --arg base "$base" --arg node "$(node --version)" --arg vitest "$vitest" \
  --arg file "$file" --arg selector "$selector" --arg aiSha "$ai_sha" --arg failed "$failed" --arg queue "$queue" \
  --arg change "$change" --arg scope "$scope" --arg mode "$mode" --argjson count "$count" \
  --slurpfile cases "$scratch/cases.json" --slurpfile report "$scratch/original.json" --slurpfile sources "$scratch/sources.jsonl" \
  --slurpfile copied "$scratch/copied.json" '
  {format:1,pin:$pin,evidenceBase:$base,node:$node,vitest:$vitest,
   selection:{file:$file,selector:$selector,caseNames:$cases[0],filteredOut:[$report[0].testResults[].assertionResults[]|select(.status=="skipped")|.fullName]},
   result:{passed:$count,failed:0,selectedSkipped:0},sourceDigests:$sources,aiArchiveSha256:$aiSha,copiedDependencies:$copied[0],
   control:{change:$change,failed:[$failed],passed:($cases[0]-[$failed])},
   priorQueue:{file:$queue,commit:$base},exclusions:[],scope:$scope} +
   (if $mode!="file" then {isolation:{interfaces:["lo"],egressProbe:{ipv4:"ENETUNREACH",ipv6:"ENETUNREACH"},nodeNetworkPermission:false}} else {} end)'
