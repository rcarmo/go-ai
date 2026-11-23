#!/usr/bin/env bash
set -euo pipefail
if [[ $# != 3 && ( $# != 4 || $4 != remaining ) ]]; then
  printf 'Usage: bash %s <pinned-git-mirror> <retained-oracle-dir> <release-archive-dir> [remaining]\n' "$0" >&2
  exit 1
fi
here=$(cd -- "$(dirname -- "$0")" && pwd -P)
deps="$here/../dependencies"
mirror=$(realpath -- "$1")
oracle=$(realpath -- "$2")
archives=$(realpath -- "$3")
pin=faac585884ae2c013a6b1400f3e9e77f10b34bac
base=2184e6164e220f1c874cd0230a8f4d2399e026bd
mode=${4:-catalogue}
[[ "$mode" != remaining ]] || base=08b02b9d17d493f1863263b290b0d1a7915505c4
file=test/qwen-token-plan-models.test.ts
selector='^Qwen Token Plan models (exposes exactly the documented Individual text models( on the China endpoint)?|exposes all text models on qwen-token-plan(-cn)?|omits image models from qwen-token-plan(-cn)?|omits retired models on qwen-token-plan(-cn|-individual|-individual-cn)?)$'
retired_case='Qwen Token Plan models omits retired models on qwen-token-plan'
asset_receipt=docs/upstream-v1.6.0/dependencies/provider-catalog-assets.jsonl
scratch=$(mktemp -d /tmp/v160-qwen-token-plan-models.XXXXXX)
trap 'rm -r -- "$scratch"' EXIT
mkdir -p "$scratch/home" "$scratch/tmp" "$scratch/node_modules/@earendil-works"
git -C "$mirror" archive "$pin" package.json packages/ai/{package.json,vitest.config.ts,src} "packages/ai/$file" \
  packages/schema/{package.json,src} packages/telemetry/{package.json,src} | tar -x -C "$scratch"
git -C "$here" show "$base:docs/upstream-v1.6.0/dependencies/npm-closure.jsonl" > "$scratch/closure.jsonl"
git -C "$here" show "$base:$asset_receipt" > "$scratch/assets.jsonl"
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
manifest_sha=$(sha256sum "$data/.manifest.json" | cut -d' ' -f1)
jq -es '[.[] | select(.role=="provider-data" and (.archiveMember | test("/qwen-token-plan(-cn|-individual|-individual-cn)?[.]json$"))) | {member:.archiveMember,sha256}] | if length==4 then . else error("expected four Qwen provider files") end' "$scratch/assets.jsonl" > "$scratch/qwen-digests.json"
jq -e '."openai-completions" | has("chat:deepseek-v4-flash")|not' "$data/qwen-token-plan.json" > "$scratch/retired.check"
if [[ "$mode" == remaining ]]; then
  git -C "$here" show "$base:docs/upstream-v1.6.0/test-inventory/qwen-token-plan-models.json" > "$scratch/prior.json"
  jq -e --arg pin "$pin" --arg file "packages/ai/$file" --slurpfile digests "$scratch/qwen-digests.json" '
    .pin==$pin and .selection.file==$file and .assets.qwenProviderDigests==$digests[0] and
    .result=={passed:10,failed:0,selectedSkipped:0} and
    ((.selection.caseNames+.selection.filteredOut)|unique|length)==120
  ' "$scratch/prior.json" > "$scratch/prior.check"
  jq -e '.selection.filteredOut | select(length==110)' "$scratch/prior.json" > "$scratch/cases.json"
  selector=$(jq -er '.selection.caseNames | select(all(.[]; test("^[a-zA-Z0-9 -]+$"))) | "^(?!(?:"+join("|")+")$)Qwen Token Plan models "' "$scratch/prior.json")
  jq -e --arg table "Qwen Token Plan models exposes Qwen reasoning_effort levels for 'qwen-token-plan-individual'/'glm-5.3'" '
    [.[]|select(.==$table or .=="Qwen Token Plan models exposes glm-5.3 reasoning_effort levels for qwen-token-plan-individual")] | select(length==2)
  ' \
    "$scratch/cases.json" > "$scratch/failed-cases.json"
  change='change Individual GLM 5.3 low alias to medium in scratch catalogue'
  scope=offline-auth-thinking-and-payloads
  # shellcheck source=offline-loopback.sh
  source "$here/offline-loopback.sh"
  prepare_offline_loopback "$scratch"
else
  jq -n '[
    "exposes exactly the documented Individual text models",
    "exposes exactly the documented Individual text models on the China endpoint",
    "exposes all text models on qwen-token-plan",
    "exposes all text models on qwen-token-plan-cn",
    "omits image models from qwen-token-plan",
    "omits image models from qwen-token-plan-cn",
    ("omits retired models on " + (["qwen-token-plan","qwen-token-plan-cn","qwen-token-plan-individual","qwen-token-plan-individual-cn"][]))
  ] | map("Qwen Token Plan models "+.)' > "$scratch/cases.json"
  jq -n --arg retired "$retired_case" '[$retired]' > "$scratch/failed-cases.json"
  change='reintroduce retired deepseek-v4-flash in scratch qwen-token-plan.json'
  scope=offline-model-only
fi
selected=$(jq -er 'length' "$scratch/cases.json")
libc=$(node -p "process.report.getReport().header.glibcVersionRuntime ? 'glibc' : 'musl'")
bun run "$deps/copy-openai-probe-deps.ts" "$scratch/closure.jsonl" "$lock_sha" "$oracle" "$scratch" "$libc" \
  node_modules/{vitest,partial-json,openai} > "$scratch/copied.json"
ln -s ../../packages/schema "$scratch/node_modules/@earendil-works/pi-schema"
ln -s ../../packages/telemetry "$scratch/node_modules/@earendil-works/pi-telemetry"
node_version=$(node --version)
vitest_version=$(jq -er '.version' "$scratch/node_modules/vitest/package.json")
[[ "$vitest_version" == 4.1.12 ]]

exec 8>/tmp/go-ai/v160-focused-verify.lock
flock 8
if [[ "$mode" == remaining ]]; then
  run_offline_loopback "$scratch" node "$scratch/check-netns.mjs"
fi
isolated_node() {
  if [[ "$mode" == remaining ]]; then
    run_offline_loopback "$scratch" env "$@"
  else
    env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" "$@"
  fi
}
run() {
  local tag=$1
  (cd "$scratch/packages/ai" && isolated_node \
    NODE_OPTIONS="--max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons" \
    node ../../node_modules/vitest/vitest.mjs run "$file" -t "$selector" --pool=forks --maxWorkers=1 --reporter=json \
    --outputFile "$scratch/$tag.json") > "$scratch/$tag.log" 2>&1
}
if ! run original; then cat "$scratch/original.log" >&2; jq -r '.testResults[].message' "$scratch/original.json" >&2 || true; exit 1; fi
jq -e --argjson selected "$selected" --slurpfile cases "$scratch/cases.json" '
  .numPassedTests==$selected and .numFailedTests==0 and .numTodoTests==0 and
  ([.testResults[].assertionResults[] | select(.status!="skipped") | [.fullName,.status]]==($cases[0]|map([.,"passed"]))) and
  (.numPendingTests==([.testResults[].assertionResults[] | select(.status=="skipped")]|length))
' "$scratch/original.json" > "$scratch/original.check"

if [[ "$mode" == remaining ]]; then
  jq -e --slurpfile prior "$scratch/prior.json" '
    ([.testResults[].assertionResults[]|select(.status=="skipped")|.fullName]|sort)==($prior[0].selection.caseNames|sort)
  ' "$scratch/original.json" > "$scratch/prior-partition.check"
  asset="$data/qwen-token-plan-individual.json"
  jq -e '."openai-completions"["chat:glm-5.3"].thinkingLevelMap.low=="low"' "$asset" > "$scratch/low.check"
  cp "$asset" "$scratch/asset-original.json"
  jq '."openai-completions"["chat:glm-5.3"].thinkingLevelMap.low="medium"' "$scratch/asset-original.json" > "$asset"
else
  # Add the retired ID using a copy of the current dated successor only in scratch.
  asset="$data/qwen-token-plan.json"
  cp "$asset" "$scratch/asset-original.json"
  jq '."openai-completions" |= (. + {"chat:deepseek-v4-flash": (.["chat:deepseek-v4-flash-0731"] | .id="deepseek-v4-flash")})' \
    "$scratch/asset-original.json" > "$asset"
fi
if run mutated; then printf 'Unexpected pass: %s\n' "$change" >&2; exit 1; fi
if ! jq -e --slurpfile cases "$scratch/cases.json" --slurpfile failed "$scratch/failed-cases.json" --argjson selected "$selected" --arg mode "$mode" '
  .numPassedTests==($selected-($failed[0]|length)) and .numFailedTests==($failed[0]|length) and .numTodoTests==0 and
  ([.testResults[].assertionResults[] | select(.status!="skipped") | [.fullName,.status]]==($cases[0]|map([.,if . as $case | $failed[0]|index($case) then "failed" else "passed" end]))) and
  all(.testResults[].assertionResults[]|select(.status=="failed"); .failureMessages | join("\n") |
    if $mode=="remaining" then contains("to match object") and (contains("qwen-token-plan-models.test.ts:243:") or contains("qwen-token-plan-models.test.ts:259:")) else contains("deepseek-v4-flash") end)
' "$scratch/mutated.json" > "$scratch/mutated.check"; then
  jq -c '{passed:.numPassedTests,failed:.numFailedTests,failures:[.testResults[].assertionResults[]|select(.status=="failed")|{fullName,failureMessages}]}' "$scratch/mutated.json" >&2
  exit 1
fi
cp "$scratch/asset-original.json" "$asset"
while IFS=$'\t' read -r member sha; do
  [[ "$(sha256sum "$data/${member##*/}" | cut -d' ' -f1)" == "$sha" ]]
done < "$scratch/member-digests"
flock -u 8

sources=("packages/ai/$file" packages/ai/{vitest.config.ts,src/compat.ts,src/providers/{qwen-token-plan,qwen-token-plan-cn,qwen-token-plan-individual,qwen-token-plan-individual-cn}.models.ts})
if [[ "$mode" == remaining ]]; then
  sources+=(packages/ai/src/{env-api-keys.ts,models.ts,auth/helpers.ts,api/openai-completions.ts,providers/{qwen-token-plan,qwen-token-plan-cn,qwen-token-plan-individual,qwen-token-plan-individual-cn}.ts})
fi
for path in "${sources[@]}"; do
  git -C "$mirror" show "$pin:$path" | cmp "$scratch/$path" -
  jq -cn --arg file "$path" --arg sha "$(sha256sum "$scratch/$path" | cut -d' ' -f1)" '{file:$file,sha256:$sha}'
done > "$scratch/source-digests.jsonl"
jq -n --arg pin "$pin" --arg base "$base" --arg node "$node_version" --arg vitest "$vitest_version" \
  --arg file "packages/ai/$file" --arg selector "$selector" --arg change "$change" --arg scope "$scope" --arg mode "$mode" --argjson selected "$selected" \
  --slurpfile failed "$scratch/failed-cases.json" --slurpfile cases "$scratch/cases.json" --slurpfile report "$scratch/original.json" \
  --arg archive "$archive_name" --arg archiveSha "$archive_sha" --slurpfile qwen "$scratch/qwen-digests.json" --arg manifestSha "$manifest_sha" \
  --slurpfile copied "$scratch/copied.json" --slurpfile source "$scratch/source-digests.jsonl" '
  {format:1,pin:$pin,evidenceBase:$base,node:$node,vitest:$vitest,
    selection:{file:$file,selector:$selector,caseNames:$cases[0],filteredOut:[$report[0].testResults[].assertionResults[]|select(.status=="skipped")|.fullName]},
    result:{passed:$selected,failed:0,selectedSkipped:0},sourceDigests:$source,
    assets:{archive:$archive,archiveSha256:$archiveSha,fileCount:44,manifestSha256:$manifestSha,qwenProviderDigests:$qwen[0]},
    control:{change:$change,failed:$failed[0],passed:($cases[0]-$failed[0])},
    copiedDependencies:$copied[0],exclusions:[],scope:$scope} +
  (if $mode=="remaining" then {isolation:{interfaces:["lo"],egressProbe:{ipv4:"ENETUNREACH",ipv6:"ENETUNREACH"},nodeNetworkPermission:false}} else {} end)'
