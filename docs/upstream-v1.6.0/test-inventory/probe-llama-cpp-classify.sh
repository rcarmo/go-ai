#!/usr/bin/env bash
set -euo pipefail
if [[ $# != 2 ]]; then
  printf 'Usage: bash %s <pinned-git-mirror> <retained-oracle-dir>\n' "$0" >&2; exit 1
fi
here=$(cd -- "$(dirname -- "$0")" && pwd -P)
mirror=$(realpath -- "$1")
oracle=$(realpath -- "$2")
pin=faac585884ae2c013a6b1400f3e9e77f10b34bac
base=dbd2495eb9e46addcd5fd7453c83b447620b42d9
file=test/llama-cpp-classify.test.ts
provider=packages/ai/src/api/llama-cpp-classify.ts
queue=docs/upstream-v1.6.0/test-inventory/primary-queue-r21.jsonl
scratch=$(mktemp -d /tmp/v160-llama-cpp.XXXXXX)
trap 'rm -r -- "$scratch"' EXIT
mkdir -p "$scratch/home" "$scratch/tmp" "$scratch/node_modules"
sources=(package.json packages/ai/{package.json,vitest.config.ts,src/types.ts} "packages/ai/$file" "$provider"
  packages/ai/src/utils/{error-body,headers,identity,provider-retry,text,version}.ts)
git -C "$mirror" archive "$pin" "${sources[@]}" | tar -x -C "$scratch"
git -C "$here" show "$base:docs/upstream-v1.6.0/dependencies/npm-closure.jsonl" > "$scratch/closure.jsonl"
lock_sha=$(git -C "$mirror" show "$pin:package-lock.json" | sha256sum | cut -d' ' -f1)
libc=$(node -p "process.report.getReport().header.glibcVersionRuntime ? 'glibc' : 'musl'")
bun run "$here/../dependencies/copy-openai-probe-deps.ts" "$scratch/closure.jsonl" "$lock_sha" "$oracle" "$scratch" "$libc" \
  node_modules/vitest > "$scratch/copied.json"
node_version=$(node --version)
vitest_version=$(jq -er '.version' "$scratch/node_modules/vitest/package.json")
[[ "$vitest_version" == 4.1.12 ]]
check_sources() {
  local path
  for path in "${sources[@]}"; do
    git -C "$mirror" show "$pin:$path" | cmp "$scratch/$path" -
  done
}
check_sources
file_sha=$(sha256sum "$scratch/packages/ai/$file" | cut -d' ' -f1)
git -C "$here" show "$base:$queue" | jq -e --arg file "packages/ai/$file" --arg sha "$file_sha" '
  select(.kind=="suite" and .path==$file) | .upstream.sha256==$sha and .original.status=="none-found"
' > "$scratch/queue.check"
sed -n 's/^[[:blank:]]*it("\([^"]*\)".*/llama.cpp classifier \1/p' "$scratch/packages/ai/$file" | jq -Rse 'split("\n")|map(select(length>0))|select(length==15 and (unique|length)==15)' > "$scratch/cases.json"
failed='llama.cpp classifier retries deeper readouts when a label is missing and fails without inventing zeros'
exec 8>/tmp/go-ai/v160-focused-verify.lock
flock 8
run() {
  local tag=$1
  (cd "$scratch/packages/ai" && env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" \
    NODE_OPTIONS="--max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons" \
    node ../../node_modules/vitest/vitest.mjs run "$file" --pool=forks --maxWorkers=1 --reporter=json \
    --outputFile "$scratch/$tag.json") > "$scratch/$tag.log" 2>&1
}
if ! run original; then cat "$scratch/original.log" >&2; jq -r '.testResults[].message' "$scratch/original.json" >&2 || true; exit 1; fi
jq -e --slurpfile cases "$scratch/cases.json" '
  .numPassedTests==15 and .numFailedTests==0 and .numTodoTests==0 and .numPendingTests==0 and
  ([.testResults[].assertionResults[]|[.fullName,.status]]==($cases[0]|map([.,"passed"])))
' "$scratch/original.json" > "$scratch/original.check"
[[ $(grep -Fc 'const READOUT_ESCALATION = [4096, 32768]' "$scratch/$provider") -eq 1 ]]
sed -i 's/const READOUT_ESCALATION = \[4096, 32768\]/const READOUT_ESCALATION = [4095, 32768]/' "$scratch/$provider"
if run mutated; then printf 'Incorrect classification escalation unexpectedly passed\n' >&2; exit 1; fi
if ! jq -e --slurpfile cases "$scratch/cases.json" --arg failed "$failed" '
  .numPassedTests==14 and .numFailedTests==1 and .numTodoTests==0 and .numPendingTests==0 and
  ([.testResults[].assertionResults[]|[.fullName,.status]]==($cases[0]|map([.,if .==$failed then "failed" else "passed" end]))) and
  any(.testResults[].assertionResults[]; .fullName==$failed and (.failureMessages|join("\n")|contains("llama-cpp-classify.test.ts:247:")))
' "$scratch/mutated.json" > "$scratch/mutated.check"; then
  jq -c '[.testResults[].assertionResults[]|select(.status=="failed")|{fullName,failureMessages}]' "$scratch/mutated.json" >&2
  exit 1
fi
flock -u 8
git -C "$mirror" show "$pin:$provider" > "$scratch/$provider"
check_sources
for path in "${sources[@]}"; do
  jq -cn --arg file "$path" --arg sha "$(sha256sum "$scratch/$path" | cut -d' ' -f1)" '{file:$file,sha256:$sha}'
done > "$scratch/source-digests.jsonl"
jq -n --arg pin "$pin" --arg base "$base" --arg node "$node_version" --arg vitest "$vitest_version" \
  --arg file "packages/ai/$file" --arg queue "$queue" --arg failed "$failed" --slurpfile cases "$scratch/cases.json" \
  --slurpfile source "$scratch/source-digests.jsonl" --slurpfile copied "$scratch/copied.json" '
  {format:1,pin:$pin,evidenceBase:$base,node:$node,vitest:$vitest,
    selection:{file:$file,selector:"whole-file",reportedCaseNames:$cases[0]},
    result:{passed:15,failed:0,skipped:0,todo:0},sourceDigests:$source,copiedDependencies:$copied[0],
    control:{change:"readout escalation 4096 to 4095 in scratch adapter",failed:[$failed],passed:($cases[0]-[$failed])},
    gates:{network:"no-Node-network-permission",credentials:"dummy-key-and-stub-fetch",exclusions:[]},
    priorQueue:{file:$queue,commit:$base},scope:"offline-classifier-only"}'
