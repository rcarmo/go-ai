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
base=d1bd6c9fc67967345df7755c2606b2a823e3a31f
file=test/env-node-list-race.test.ts
full_name='NodeExecutionEnv.listDir under concurrent changes leaves out an entry removed between enumeration and lstat'
scratch=$(mktemp -d /tmp/v160-env-list-race.XXXXXX)
trap 'rm -r -- "$scratch"' EXIT
mkdir -p "$scratch/home" "$scratch/tmp" "$scratch/node_modules/@earendil-works"
git -C "$mirror" archive "$pin" package.json packages/durable/{package.json,vitest.config.ts,src,test/setup-document-retention.ts} \
  "packages/durable/$file" packages/chord/{package.json,src} packages/schema/{package.json,src} packages/telemetry/{package.json,src} | tar -x -C "$scratch"
git -C "$here" show "$base:docs/upstream-v1.6.0/dependencies/npm-closure.jsonl" > "$scratch/closure.jsonl"
lock_sha=$(git -C "$mirror" show "$pin:package-lock.json" | sha256sum | cut -d' ' -f1)
libc=$(node -p "process.report.getReport().header.glibcVersionRuntime ? 'glibc' : 'musl'")
bun run "$deps/copy-openai-probe-deps.ts" "$scratch/closure.jsonl" "$lock_sha" "$oracle" "$scratch" "$libc" node_modules/vitest > "$scratch/copied.json"
ln -s ../../packages/chord "$scratch/node_modules/@earendil-works/chord"
ln -s ../../packages/schema "$scratch/node_modules/@earendil-works/pi-schema"
ln -s ../../packages/telemetry "$scratch/node_modules/@earendil-works/pi-telemetry"
node_version=$(node --version)
vitest_version=$(jq -er '.version' "$scratch/node_modules/vitest/package.json")
[[ "$vitest_version" == 4.1.12 ]]
check_source() {
  local path
  for path in "packages/durable/$file" packages/durable/src/env/node.ts packages/durable/vitest.config.ts packages/durable/test/setup-document-retention.ts; do
    git -C "$mirror" show "$pin:$path" | cmp "$scratch/$path" -
  done
}
check_source
if ! (cd "$scratch/packages/durable" && flock /tmp/go-ai/v160-focused-verify.lock env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" \
    NODE_OPTIONS="--max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons" \
    node ../../node_modules/vitest/vitest.mjs run "$file" --pool=forks --maxWorkers=1 --reporter=json --outputFile "$scratch/result.json") > "$scratch/vitest.log" 2>&1; then
  cat "$scratch/vitest.log" >&2
  exit 1
fi
check_source
jq -e --arg full "$full_name" '
  .numPassedTests==1 and .numFailedTests==0 and .numPendingTests==0 and .numTodoTests==0 and
  ([.testResults[].assertionResults[] | [.fullName,.status]]==[[$full,"passed"]])
' "$scratch/result.json" > "$scratch/exact.check"
file_sha=$(sha256sum "$scratch/packages/durable/$file" | cut -d' ' -f1)
jq -n --arg pin "$pin" --arg base "$base" --arg node "$node_version" --arg vitest "$vitest_version" \
  --arg file "packages/durable/$file" --arg fileSha "$file_sha" --arg full "$full_name" \
  --slurpfile copied "$scratch/copied.json" '
  { format:1, pin:$pin, evidenceBase:$base, node:$node, vitest:$vitest,
    selection:{file:$file,sha256:$fileSha,selector:"whole-file",reportedCaseNames:[$full]},
    result:{passed:1,failed:0,skipped:0,todo:0},copiedDependencies:$copied[0],scope:"new-unchanged-offline-original-only" }'
