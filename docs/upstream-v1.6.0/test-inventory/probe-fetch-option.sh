#!/usr/bin/env bash
set -euo pipefail
if [[ $# != 2 ]]; then
  printf 'Usage: bash %s <pinned-git-mirror> <retained-oracle-dir>\n' "$0" >&2
  exit 1
fi
here=$(cd -- "$(dirname -- "$0")" && pwd -P)
deps="$here/../dependencies"
mirror=$(realpath -- "$1")
oracle=$(realpath -- "$2")
pin=faac585884ae2c013a6b1400f3e9e77f10b34bac
base=7b90c606b0d5b99741ed7f238e97ffefb9695454
file=test/fetch-option.test.ts
scratch=$(mktemp -d /tmp/v160-fetch-option.XXXXXX)
trap 'rm -r -- "$scratch"' EXIT
mkdir -p "$scratch/home" "$scratch/tmp" "$scratch/node_modules/@earendil-works"

git -C "$mirror" archive "$pin" package.json packages/ai/{package.json,vitest.config.ts,src} "packages/ai/$file" \
  packages/schema/{package.json,src} packages/telemetry/{package.json,src} | tar -x -C "$scratch"
git -C "$here" show "$base:docs/upstream-v1.6.0/dependencies/npm-closure.jsonl" > "$scratch/closure.jsonl"
lock_sha=$(git -C "$mirror" show "$pin:package-lock.json" | sha256sum | cut -d' ' -f1)
libc=$(node -p "process.report.getReport().header.glibcVersionRuntime ? 'glibc' : 'musl'")
bun run "$deps/copy-openai-probe-deps.ts" "$scratch/closure.jsonl" "$lock_sha" "$oracle" "$scratch" "$libc" \
  node_modules/{vitest,openai,partial-json,zod,@anthropic-ai/sdk,@google/genai} > "$scratch/copied.json"
ln -s ../../packages/schema "$scratch/node_modules/@earendil-works/pi-schema"
ln -s ../../packages/telemetry "$scratch/node_modules/@earendil-works/pi-telemetry"
node_version=$(node --version)
vitest_version=$(jq -er '.version' "$scratch/node_modules/vitest/package.json")
[[ "$vitest_version" == 4.1.12 ]]
source_paths=(
  "packages/ai/$file" packages/ai/vitest.config.ts
  packages/ai/src/api/{anthropic-messages,azure-openai-responses,google-generative-ai,google-vertex,mistral-conversations,openai-codex-responses,openai-completions,openai-responses,openrouter-images,pi-messages}.ts
)
check_sources() {
  local path
  for path in "${source_paths[@]}"; do
    git -C "$mirror" show "$pin:$path" | cmp "$scratch/$path" -
  done
}
check_sources
queue=docs/upstream-v1.6.0/test-inventory/primary-queue-reconciled.jsonl
file_sha=$(sha256sum "$scratch/packages/ai/$file" | cut -d' ' -f1)
git -C "$here" show "$base:$queue" | jq -e --arg file "packages/ai/$file" --arg sha "$file_sha" '
   select(.kind=="suite" and .path==$file) | .sourceSha256==$sha
' > "$scratch/queue.check"
if ! (cd "$scratch/packages/ai" && flock /tmp/go-ai/v160-focused-verify.lock env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" \
    NODE_OPTIONS="--max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons" \
    node ../../node_modules/vitest/vitest.mjs run "$file" --pool=forks --maxWorkers=1 --reporter=json --outputFile "$scratch/result.json") > "$scratch/vitest.log" 2>&1; then
  cat "$scratch/vitest.log" >&2
  jq -r '.testResults[] | select(.status=="failed").message' "$scratch/result.json" >&2 || true
  exit 1
fi
check_sources
jq -e '
  .numPassedTests==6 and .numFailedTests==0 and .numPendingTests==0 and .numTodoTests==0 and
  ([.testResults[].assertionResults[] | [.fullName,.status]]==([
    "fetch stream option passes fetch through streamSimple to the Anthropic SDK",
    "fetch stream option passes fetch through streamSimple to OpenAI SDK adapters",
    "fetch stream option uses fetch for Mistral, Codex SSE, and pi-messages HTTP requests",
    "fetch stream option passes fetch through streamSimple to the Google GenAI SDK adapters",
    "fetch stream option keeps the global dispatcher for Google adapters when no fetch is given",
    "fetch stream option uses fetch for image generation"
  ] | map([.,"passed"])))
' "$scratch/result.json" > "$scratch/exact.check"
for path in "${source_paths[@]}"; do
  jq -cn --arg path "$path" --arg sha "$(sha256sum "$scratch/$path" | cut -d' ' -f1)" '{file:$path,sha256:$sha}'
done > "$scratch/sources.jsonl"
jq -n --arg pin "$pin" --arg base "$base" --arg node "$node_version" --arg vitest "$vitest_version" \
  --arg file "packages/ai/$file" --arg queue "$queue" --slurpfile source "$scratch/sources.jsonl" --slurpfile report "$scratch/result.json" \
  --slurpfile copied "$scratch/copied.json" '
  { format:1, pin:$pin, evidenceBase:$base, node:$node, vitest:$vitest,
    selection:{file:$file,selector:"whole-file",reportedCases:[$report[0].testResults[].assertionResults[]|{name:.fullName,status}]},
    result:{passed:6,failed:0,skipped:0,todo:0},sourceDigests:$source,copiedDependencies:$copied[0],
    gates:{credentials:"none-dummy-keys-and-stub-fetch",platform:"observed-host-only",network:"no-Node-network-permission",exclusions:[]},
    priorQueue:{file:$queue,commit:$base},scope:"new-unchanged-offline-originals-only" }'
