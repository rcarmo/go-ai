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
base=6c5059998c3e3093158032605bb358086d214418
mode=${4:-wire}
[[ "$mode" != remaining ]] || base=2670fa7ed002d2f41f90872ac996634fba212420
file=test/fireworks-models.test.ts
suite='Anthropic-compatible session affinity and tool compat'
selector="^$suite (sends x-session-affinity header for Fireworks models|omits x-session-affinity header for native Anthropic models|omits x-session-affinity header when cacheRetention is none|sends only x-session-id for OpenRouter models|omits OpenRouter session headers when cacheRetention is none|allows OpenRouter session headers to be disabled|omits cache_control on tools for Fireworks models|omits eager_input_streaming on tools for Fireworks models|sends cache_control on tools for native Anthropic models|sends eager_input_streaming on tools for native Anthropic models)$"
asset_receipt=docs/upstream-v1.6.0/dependencies/provider-catalog-assets.jsonl
scratch=$(mktemp -d /tmp/v160-fireworks-loopback.XXXXXX)
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
fireworks_sha=$(sha256sum "$data/fireworks.json" | cut -d' ' -f1)
manifest_sha=$(sha256sum "$data/.manifest.json" | cut -d' ' -f1)
libc=$(node -p "process.report.getReport().header.glibcVersionRuntime ? 'glibc' : 'musl'")
bun run "$deps/copy-openai-probe-deps.ts" "$scratch/closure.jsonl" "$lock_sha" "$oracle" "$scratch" "$libc" \
  node_modules/{vitest,partial-json,typebox,openai,@anthropic-ai/sdk} > "$scratch/copied.json"
ln -s ../../packages/schema "$scratch/node_modules/@earendil-works/pi-schema"
ln -s ../../packages/telemetry "$scratch/node_modules/@earendil-works/pi-telemetry"
node_version=$(node --version)
vitest_version=$(jq -er '.version' "$scratch/node_modules/vitest/package.json")
[[ "$vitest_version" == 4.1.12 ]]

if [[ "$mode" == remaining ]]; then
  inventory=docs/upstream-v1.6.0/test-inventory
  git -C "$here" show "$base:$inventory/fireworks-loopback.json" > "$scratch/wire.json"
  git -C "$here" show "$base:$inventory/fireworks-models.json" > "$scratch/models.json"
  jq -es --arg pin "$pin" --arg file "packages/ai/$file" --arg sha "$fireworks_sha" '
    all(.[]; .pin==$pin and .selection.file==$file and .assets.fireworksSha256==$sha and .result.failed==0 and .result.selectedSkipped==0) and
    (.[0].selection.caseNames|length)==10 and (.[1].selection.caseNames|length)==3 and
    ((.[1].selection.caseNames-.[0].selection.filteredOut)|length)==0
  ' "$scratch/wire.json" "$scratch/models.json" > "$scratch/prior.check"
  jq -es '.[0].selection.filteredOut-.[1].selection.caseNames | select(length==12 and (unique|length)==12)' \
    "$scratch/wire.json" "$scratch/models.json" > "$scratch/cases.json"
  jq -s '[.[].selection.caseNames[]] | sort' "$scratch/wire.json" "$scratch/models.json" > "$scratch/prior-cases.json"
  # These pinned full names contain no regex metacharacters.
  selector=$(jq -er 'select(all(.[]; test("^[a-zA-Z0-9 /_-]+$"))) | "^("+join("|")+")$"' "$scratch/cases.json")
  failed_case='Fireworks models sends native Messages effort levels for accounts/fireworks/models/deepseek-v4p1-flash'
  expected_error='expected false to be true'
  expected_line=126
  change='set DeepSeek Flash forceAdaptiveThinking to false in scratch fireworks.json'
  scope=offline-fireworks-models-and-payloads
else
  jq -n --arg suite "$suite" '[
    "sends x-session-affinity header for Fireworks models",
    "omits x-session-affinity header for native Anthropic models",
    "omits x-session-affinity header when cacheRetention is none",
    "sends only x-session-id for OpenRouter models",
    "omits OpenRouter session headers when cacheRetention is none",
    "allows OpenRouter session headers to be disabled",
    "omits cache_control on tools for Fireworks models",
    "omits eager_input_streaming on tools for Fireworks models",
    "sends cache_control on tools for native Anthropic models",
    "sends eager_input_streaming on tools for native Anthropic models"
  ] | map($suite+" "+.)' > "$scratch/cases.json"
  failed_case=$(jq -er '.[0]' "$scratch/cases.json")
  expected_error=fireworks-session-1
  expected_line=343
  change='rename x-session-affinity to x-bad-affinity in scratch Anthropic adapter'
  scope=offline-loopback-session-and-tools
fi
selected=$(jq -er 'length' "$scratch/cases.json")

# The kernel, not a fetch shim, restricts the Vitest worker and its children.
# Configure and check the fresh namespace before dropping all capabilities.
cat > "$scratch/in-netns.sh" <<'BASH'
set -euo pipefail
[[ $(readlink /proc/self/ns/net) != "$1" ]]
shift
ip -j link show | jq -e '[.[].ifname]==["lo"]' > /dev/null
ip link set lo up
for family in -4 -6; do
  ip "$family" -j route show table all | jq -e 'all(.[]; .dev=="lo")' > /dev/null
done
exec setpriv --bounding-set=-all --inh-caps=-all --ambient-caps=-all --no-new-privs "$@"
BASH
cat > "$scratch/check-netns.mjs" <<'JS'
import assert from "node:assert/strict";
import { createConnection, createServer } from "node:net";
import { once } from "node:events";

const server = createServer(socket => socket.end("loopback ready"));
server.listen(0, "127.0.0.1");
await once(server, "listening");
try {
  const socket = createConnection({ host: "127.0.0.1", port: server.address().port });
  let body = "";
  for await (const chunk of socket) body += chunk;
  assert.equal(body, "loopback ready");
} finally {
  server.close();
}
for (const host of ["192.0.2.1", "2001:db8::1"]) {
  const guard = AbortSignal.timeout(2000);
  const socket = createConnection({ host, port: 443, signal: guard });
  try {
    const [error] = await once(socket, "error", { signal: guard });
    assert.equal(error.code, "ENETUNREACH");
  } finally {
    socket.destroy();
  }
}
JS

host_netns=$(readlink /proc/self/ns/net)
isolate() {
  unshare --user --map-root-user --net env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" \
    bash "$scratch/in-netns.sh" "$host_netns" "$@"
}
exec 8>/tmp/go-ai/v160-focused-verify.lock
flock 8
isolate node "$scratch/check-netns.mjs"
run() {
  local tag=$1
  (cd "$scratch/packages/ai" && isolate env \
    NODE_OPTIONS="--max-old-space-size=768 --permission --allow-net --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons" \
    node ../../node_modules/vitest/vitest.mjs run "$file" -t "$selector" --pool=forks --maxWorkers=1 --reporter=json \
    --outputFile "$scratch/$tag.json") > "$scratch/$tag.log" 2>&1
}
if ! run original; then cat "$scratch/original.log" >&2; jq -r '.testResults[].message' "$scratch/original.json" >&2 || true; exit 1; fi
jq -e --argjson selected "$selected" --slurpfile cases "$scratch/cases.json" '
  .numPassedTests==$selected and .numFailedTests==0 and .numTodoTests==0 and
  ([.testResults[].assertionResults[] | select(.status!="skipped") | [.fullName,.status]]==($cases[0]|map([.,"passed"]))) and
  (.numPendingTests==([.testResults[].assertionResults[] | select(.status=="skipped")]|length))
' "$scratch/original.json" > "$scratch/original.check"

provider=packages/ai/src/api/anthropic-messages.ts
if [[ "$mode" == remaining ]]; then
  jq -e --slurpfile prior "$scratch/prior-cases.json" '
    ([.testResults[].assertionResults[]|select(.status=="skipped")|.fullName]|sort)==$prior[0]
  ' "$scratch/original.json" > "$scratch/filtered.check"
  jq -e '."anthropic-messages"["chat:accounts/fireworks/models/deepseek-v4p1-flash"].compat.forceAdaptiveThinking==true' \
    "$data/fireworks.json" > "$scratch/adaptive.check"
  cp "$data/fireworks.json" "$scratch/original-fireworks.json"
  jq '."anthropic-messages"["chat:accounts/fireworks/models/deepseek-v4p1-flash"].compat.forceAdaptiveThinking=false' \
    "$scratch/original-fireworks.json" > "$data/fireworks.json"
else
  # Only the scratch provider is changed. Both originals and assets stay intact.
  git -C "$mirror" show "$pin:$provider" | cmp "$scratch/$provider" -
  grep -q 'sessionAffinityFormat === "openrouter" ? "x-session-id" : "x-session-affinity"' "$scratch/$provider"
  sed -i 's/sessionAffinityFormat === "openrouter" ? "x-session-id" : "x-session-affinity"/sessionAffinityFormat === "openrouter" ? "x-session-id" : "x-bad-affinity"/' "$scratch/$provider"
fi
if run mutated; then printf 'Unexpected pass: %s\n' "$change" >&2; exit 1; fi
if ! jq -e --slurpfile cases "$scratch/cases.json" --arg failed "$failed_case" --argjson selected "$selected" \
  --arg error "$expected_error" --arg location "fireworks-models.test.ts:$expected_line:" '
  .numPassedTests==($selected-1) and .numFailedTests==1 and .numTodoTests==0 and
  ([.testResults[].assertionResults[] | select(.status!="skipped") | [.fullName,.status]]==($cases[0]|map([.,if .==$failed then "failed" else "passed" end]))) and
  any(.testResults[].assertionResults[]; .fullName==$failed and (.failureMessages | join("\n") | contains($error) and contains($location)))
' "$scratch/mutated.json" > "$scratch/mutated.check"; then
  jq -c '{passed:.numPassedTests,failed:.numFailedTests,failures:[.testResults[].assertionResults[]|select(.status=="failed")|{fullName,failureMessages}]}' "$scratch/mutated.json" >&2
  exit 1
fi
flock -u 8
sources=("packages/ai/$file" packages/ai/{vitest.config.ts,src/providers/fireworks.models.ts} "$provider")
if [[ "$mode" == remaining ]]; then
  cp "$scratch/original-fireworks.json" "$data/fireworks.json"
  [[ $(sha256sum "$data/fireworks.json" | cut -d' ' -f1) == "$fireworks_sha" ]]
  sources+=(packages/ai/src/api/openai-completions.ts)
else
  git -C "$mirror" show "$pin:$provider" > "$scratch/$provider"
fi
for path in "${sources[@]}"; do
  git -C "$mirror" show "$pin:$path" | cmp "$scratch/$path" -
  jq -cn --arg file "$path" --arg sha "$(sha256sum "$scratch/$path" | cut -d' ' -f1)" '{file:$file,sha256:$sha}'
done > "$scratch/source-digests.jsonl"
jq -n --arg pin "$pin" --arg base "$base" --arg node "$node_version" --arg vitest "$vitest_version" \
  --arg file "packages/ai/$file" --arg selector "$selector" --slurpfile cases "$scratch/cases.json" --slurpfile report "$scratch/original.json" \
  --arg archive "$archive_name" --arg archiveSha "$archive_sha" --arg fireworksSha "$fireworks_sha" --arg manifestSha "$manifest_sha" \
  --arg change "$change" --arg failed "$failed_case" --arg scope "$scope" --argjson selected "$selected" \
  --slurpfile copied "$scratch/copied.json" --slurpfile source "$scratch/source-digests.jsonl" '
  {format:1,pin:$pin,evidenceBase:$base,node:$node,vitest:$vitest,
    selection:{file:$file,selector:$selector,caseNames:$cases[0],filteredOut:[$report[0].testResults[].assertionResults[]|select(.status=="skipped")|.fullName]},
    result:{passed:$selected,failed:0,selectedSkipped:0},sourceDigests:$source,
    assets:{archive:$archive,archiveSha256:$archiveSha,fileCount:44,manifestSha256:$manifestSha,fireworksSha256:$fireworksSha},
    isolation:{mechanism:"unprivileged Linux network namespace; capabilities dropped before execution",interfaces:["lo"],loopback:"IPv4 TCP transfer",egressProbe:{ipv4:"ENETUNREACH",ipv6:"ENETUNREACH"}},
    control:{change:$change,failed:[$failed],passed:($cases[0]|map(select(.!=$failed)))},
    copiedDependencies:$copied[0],exclusions:[],scope:$scope}'
