#!/usr/bin/env bash
set -euo pipefail
if [[ $# != 3 ]]; then
  printf 'Usage: bash %s <pinned-git-mirror> <retained-oracle-dir> <release-archive-dir>\n' "$0" >&2
  exit 1
fi
here=$(cd -- "$(dirname -- "$0")" && pwd -P)
deps="$here/../dependencies"
mirror=$(realpath -- "$1")
oracle=$(realpath -- "$2")
archives=$(realpath -- "$3")
pin=faac585884ae2c013a6b1400f3e9e77f10b34bac
base=c32240b3d48af127e43cf9b4bc45ee9911a95b6e
file=test/together-models.test.ts
first='Together models registers the default Kimi K3 model via OpenAI-compatible Chat Completions API'
second='Together models models Together reasoning controls from the Together API surface'
filtered='Together models resolves TOGETHER_API_KEY from the environment'
selector='registers the default Kimi K3 model via OpenAI-compatible Chat Completions API|models Together reasoning controls from the Together API surface'
asset_receipt=docs/upstream-v1.6.0/dependencies/provider-catalog-assets.jsonl
native_file=tests/models_catalog_upstream_test.go
scratch=$(mktemp -d /tmp/v160-together-models.XXXXXX)
trap 'rm -r -- "$scratch"' EXIT
mkdir -p "$scratch/home" "$scratch/tmp" "$scratch/node_modules/@earendil-works"
git -C "$mirror" archive "$pin" package.json packages/ai/{package.json,vitest.config.ts,src} "packages/ai/$file" \
  packages/schema/{package.json,src} packages/telemetry/{package.json,src} | tar -x -C "$scratch"
git -C "$here" show "$base:docs/upstream-v1.6.0/dependencies/npm-closure.jsonl" > "$scratch/closure.jsonl"
git -C "$here" show "$base:$asset_receipt" > "$scratch/assets.jsonl"
git -C "$here" show "$base:$native_file" > "$scratch/native-baseline.go"
grep -F 'model.MaxTokens != 131072' "$scratch/native-baseline.go" > "$scratch/native-baseline.check"
lock_sha=$(git -C "$mirror" show "$pin:package-lock.json" | sha256sum | cut -d' ' -f1)
jq -e --arg pin "$pin" --arg sha "$lock_sha" 'select(.kind=="manifest") | .pin==$pin and .lockSha256==$sha and .package=="@earendil-works/pi-ai" and .version=="1.6.0" and .providerFiles==43' "$scratch/assets.jsonl" > "$scratch/assets.check"
read -r archive_name archive_sha < <(jq -er 'select(.kind=="manifest") | [.archive.file,.archive.sha256] | @tsv' "$scratch/assets.jsonl")
[[ "$archive_name" == earendil-works-pi-ai-1.6.0.tgz && "$(sha256sum "$archives/$archive_name" | cut -d' ' -f1)" == "$archive_sha" ]]
data="$scratch/packages/ai/src/providers/data"
mkdir -p "$data"
jq -er 'select(.kind=="catalog-asset" and (.role=="provider-data" or .role=="schema-v6-manifest")) | [.archiveMember,.sha256] | @tsv' "$scratch/assets.jsonl" > "$scratch/member-digests"
[[ $(wc -l < "$scratch/member-digests") == 44 ]]
while IFS=$'\t' read -r member sha; do
  [[ "$member" =~ ^package/dist/providers/data/(\.manifest|[a-z0-9][a-z0-9-]*)\.json$ && "$sha" =~ ^[0-9a-f]{64}$ ]]
  printf '%s\n' "$member"
done < "$scratch/member-digests" > "$scratch/members"
[[ $(sort -u "$scratch/members" | wc -l) == 44 ]]
tar -xzf "$archives/$archive_name" --strip-components=4 -C "$data" --files-from="$scratch/members"
while IFS=$'\t' read -r member sha; do
  [[ "$(sha256sum "$data/${member##*/}" | cut -d' ' -f1)" == "$sha" ]]
done < "$scratch/member-digests"
together_sha=$(sha256sum "$data/together.json" | cut -d' ' -f1)
manifest_sha=$(sha256sum "$data/.manifest.json" | cut -d' ' -f1)
jq -e '."openai-completions"["chat:moonshotai/Kimi-K3"].maxTokens==1048576' "$data/together.json" > "$scratch/max.check"
libc=$(node -p "process.report.getReport().header.glibcVersionRuntime ? 'glibc' : 'musl'")
bun run "$deps/copy-openai-probe-deps.ts" "$scratch/closure.jsonl" "$lock_sha" "$oracle" "$scratch" "$libc" \
  node_modules/{vitest,partial-json} > "$scratch/copied.json"
ln -s ../../packages/schema "$scratch/node_modules/@earendil-works/pi-schema"
ln -s ../../packages/telemetry "$scratch/node_modules/@earendil-works/pi-telemetry"
node_version=$(node --version)
vitest_version=$(jq -er '.version' "$scratch/node_modules/vitest/package.json")
[[ "$vitest_version" == 4.1.12 ]]

exec 8>/tmp/go-ai/v160-focused-verify.lock
flock 8
run() {
  local tag=$1
  (cd "$scratch/packages/ai" && env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" \
    NODE_OPTIONS="--max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons" \
    node ../../node_modules/vitest/vitest.mjs run "$file" -t "$selector" --pool=forks --maxWorkers=1 --reporter=json \
    --outputFile "$scratch/$tag.json") > "$scratch/$tag.log" 2>&1
}
if ! run original; then cat "$scratch/original.log" >&2; jq -r '.testResults[].message' "$scratch/original.json" >&2 || true; exit 1; fi
jq -e --arg first "$first" --arg second "$second" --arg other "$filtered" '
  .numPassedTests==2 and .numFailedTests==0 and .numPendingTests==1 and .numTodoTests==0 and
  ([.testResults[].assertionResults[] | [.fullName,.status]]==[[$first,"passed"],[$second,"passed"],[$other,"skipped"]])
' "$scratch/original.json" > "$scratch/original.check"

# Change only the selected field semantically. The unmodified source must detect the old token limit.
jq '."openai-completions"["chat:moonshotai/Kimi-K3"].maxTokens=131072' "$data/together.json" > "$scratch/mutated-together.json"
cp "$scratch/mutated-together.json" "$data/together.json"
if run mutated; then printf 'Unexpected pass on historical Together token limit\n' >&2; exit 1; fi
jq -e --arg first "$first" --arg second "$second" --arg other "$filtered" '
  .numPassedTests==1 and .numFailedTests==1 and .numPendingTests==1 and .numTodoTests==0 and
  ([.testResults[].assertionResults[] | [.fullName,.status]]==[[$first,"failed"],[$second,"passed"],[$other,"skipped"]]) and
  any(.testResults[].assertionResults[]; .fullName==$first and (.failureMessages | join("\n") | contains("1048576") and contains("131072")))
' "$scratch/mutated.json" > "$scratch/mutated.check"
flock -u 8

for path in "packages/ai/$file" packages/ai/{vitest.config.ts,src/compat.ts,src/providers/together.models.ts}; do
  git -C "$mirror" show "$pin:$path" | cmp "$scratch/$path" -
  jq -cn --arg file "$path" --arg sha "$(sha256sum "$scratch/$path" | cut -d' ' -f1)" '{file:$file,sha256:$sha}'
done > "$scratch/source-digests.jsonl"
jq -n --arg pin "$pin" --arg base "$base" --arg node "$node_version" --arg vitest "$vitest_version" \
  --arg file "packages/ai/$file" --arg selector "$selector" --arg first "$first" --arg second "$second" --arg other "$filtered" \
  --arg archive "$archive_name" --arg archiveSha "$archive_sha" --arg togetherSha "$together_sha" --arg manifestSha "$manifest_sha" \
  --slurpfile copied "$scratch/copied.json" --slurpfile source "$scratch/source-digests.jsonl" '
  {format:1,pin:$pin,evidenceBase:$base,node:$node,vitest:$vitest,
    selection:{file:$file,selector:$selector,caseNames:[$first,$second],filteredOut:[$other]},
    result:{passed:2,failed:0,selectedSkipped:0},sourceDigests:$source,
    assets:{archive:$archive,archiveSha256:$archiveSha,fileCount:44,manifestSha256:$manifestSha,togetherSha256:$togetherSha},
    control:{change:"Kimi-K3 maxTokens 1048576 -> 131072 in scratch together.json",failed:[$first],passed:[$second]},
    copiedDependencies:$copied[0],exclusions:[],scope:"offline-model-only"}'
