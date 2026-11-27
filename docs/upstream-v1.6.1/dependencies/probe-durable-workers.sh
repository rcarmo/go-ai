#!/usr/bin/env bash
# Usage: bash probe-durable-workers.sh <pinned git mirror> <npm _cacache> <v1.6.1 npm releases> <OpenAI tarball> <Google tarball> [full-storage]
set -euo pipefail
if [[ $# != 5 && ( $# != 6 || $6 != full-storage ) ]]; then
  printf 'Usage: bash %s <pinned git mirror> <npm _cacache> <v1.6.1 npm releases> <OpenAI tarball> <Google tarball> [full-storage]\n' "$0" >&2
  exit 2
fi
full_storage=${6:-}
here=$(cd -- "$(dirname -- "$0")" && pwd -P)
# shellcheck source=/dev/null
source "$here/../../../scripts/project-env.sh"
work=$(mktemp -d "$TMPDIR/v161-durable-workers.XXXXXXXX")
trap 'rm -r -- "$work"' EXIT
mkdir "$work/stage" "$work/home" "$work/tmp"
bun run "$here/stage-durable-workers.ts" "${@:1:5}" "$work/stage" > "$work/closure.json"
engine=node_modules/@cloudflare/workerd-linux-arm64/bin/workerd
engine_sha=$(sha256sum < "$work/stage/$engine" | cut -d ' ' -f 1)
# Match the same archive member separately witnessed by the release's direct workerd example.
[[ $engine_sha == "$(jq -er .engineAvailability.workerd.binary.sha256 "$here/cloudflare-codemode-example.json")" ]]
selector='migrates a version 1 schema to admit sleeping tasks, keeping tasks and indexes'
full="Durable Object SQLite schema $selector"
if [[ $full_storage ]]; then
  selector=
  cmp <(jq -S .closure "$here/durable-workers-migration.json") <(jq -S . "$work/closure.json")
fi

# Miniflare needs loopback. The network namespace has no other interface or default route.
# The mount namespace exposes no checkout, credentials or shared Oracle to the test process.
# The fallback resolver can stat virtual absolute module paths and receive ENOENT normally.
# Workerd initialises its default public trust store even without an outbound route.
# shellcheck disable=SC2016
if ! flock -w 900 /tmp/go-ai/v160-focused-verify.lock timeout 80s env -i \
  PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin HOME="$work/home" TMPDIR="$work/tmp" XDG_CACHE_HOME="$work/home/.cache" \
  WRANGLER_SEND_METRICS=false CLOUDFLARE_LOAD_DEV_VARS_FROM_DOT_ENV=false NODE_DEBUG=vitest-plugin:module-fallback \
  NODE_OPTIONS=--max-old-space-size=1024 unshare -Urn sh -ec '
    ip link set lo up
    [ "$(ip -o link show | cut -d : -f 2 | tr -d " ")" = lo ]
    [ -z "$(ip -4 route show default)" ] && [ -z "$(ip -6 route show default)" ]
    exec bwrap --die-with-parent --unshare-pid --ro-bind /usr /usr --symlink usr/bin /bin --symlink usr/lib /lib --symlink usr/sbin /sbin \
      --ro-bind /etc/hosts /etc/hosts --ro-bind /etc/nsswitch.conf /etc/nsswitch.conf --ro-bind /etc/passwd /etc/passwd --ro-bind /etc/group /etc/group \
      --ro-bind /etc/ssl/certs /etc/ssl/certs --dev /dev --proc /proc --dir /tmp --bind "$1" "$1" --chdir "$1/stage/packages/durable" sh -ec '\''
        work=$1 selector=$2 engine=$3
        "./../../$engine" --version > "$work/workerd.version"
        node --version > "$work/node.version"
        set -- storage.test.ts
        if [ -n "$selector" ]; then set -- "$@" -t "$selector"; fi
        exec strace -f -qq -o "$work/spawn.log" -e trace=execve,execveat \
          node ../../node_modules/vitest/vitest.mjs run --config test-workers/vitest.config.ts "$@" \
          --maxWorkers=1 --reporter=json --outputFile "$work/result.json"
      '\'' sh "$1" "$2" "$3"
  ' sh "$work" "$selector" "$engine" > "$work/vitest.log" 2>&1; then
  tail -80 "$work/vitest.log" >&2
  exit 1
fi
if [[ $full_storage ]]; then
  jq -e --arg migration "$full" '.success and .numTotalTests==35 and .numPassedTests==35 and .numFailedTests==0 and
    .numPendingTests==0 and .numTodoTests==0 and (.testResults|length)==1 and
    ([.testResults[].assertionResults[] | select(.status=="passed") | .fullName] |
      length==35 and (unique|length)==35 and index($migration)!=null)' "$work/result.json" > "$work/test.check"
else
  jq -e --arg full "$full" '.success and .numPassedTests==1 and .numFailedTests==0 and .numTodoTests==0 and
    ([.testResults[].assertionResults[] | select(.status=="passed") | .fullName] == [$full])' "$work/result.json" > "$work/test.check"
fi
# A direct version probe proves availability; execve under the Vitest command proves use.
spawns=$(grep -F "execve(\"$work/stage/$engine\", [" "$work/spawn.log" | grep -cE ' = 0$')
[[ $spawns -ge 1 ]]
[[ $(sha256sum < "$work/stage/$engine" | cut -d ' ' -f 1) == "$engine_sha" ]]
while IFS=$'\t' read -r path sha; do
  [[ $(sha256sum < "$work/stage/$path" | cut -d ' ' -f 1) == "$sha" ]]
done < <(jq -r '.inputs[] | [.path,.sha256] | @tsv' "$work/closure.json")

if [[ $full_storage ]]; then
  jq -n --arg node "$(cat "$work/node.version")" --arg workerd "$(cat "$work/workerd.version")" \
    --arg engine "$engine" --arg sha "$engine_sha" --argjson spawns "$spawns" \
    --arg migration "$full" --slurpfile closure "$work/closure.json" --slurpfile result "$work/result.json" '
    {pin:$closure[0].pin,closure:{receipt:"durable-workers-migration.json",lockSHA256:$closure[0].lockSHA256,
       archiveRecordsSHA256:$closure[0].archiveRecordsSHA256,stageMatchesEarlierReceipt:true},
     execution:{file:"packages/durable/test-workers/storage.test.ts",selector:null,node:$node,
       vitest:($closure[0].selectedNpmArchives[]|select(.name=="vitest").version),
       result:{passed:$result[0].numPassedTests,failed:$result[0].numFailedTests,skipped:$result[0].numPendingTests,
         todo:$result[0].numTodoTests},
       passedOriginals:[$result[0].testResults[].assertionResults[].fullName],
       overlapWithEarlierReceipt:[$migration],additionalOriginals:($result[0].numPassedTests-1),
       workerd:{version:$workerd,path:$engine,sha256:$sha,successfulExecveByVitest:$spawns},
       isolation:"private mount namespace; private network with loopback only; no hosted services"},
     scope:"Full unchanged storage.test.ts file against Cloudflare Durable Object SQLite. Other test-workers files and native Go backend parity were not evaluated."}'
  exit
fi
jq -n --arg full "$full" --arg node "$(cat "$work/node.version")" --arg workerd "$(cat "$work/workerd.version")" \
  --arg engine "$engine" --arg sha "$engine_sha" --argjson spawns "$spawns" \
  --slurpfile closure "$work/closure.json" --slurpfile result "$work/result.json" '
  {closure:$closure[0],execution:{file:"packages/durable/test-workers/storage.test.ts",selector:$full,node:$node,
     vitest:($closure[0].selectedNpmArchives[]|select(.name=="vitest").version),
     result:{passed:$result[0].numPassedTests,failed:$result[0].numFailedTests,selectedSkipped:0,filteredOut:$result[0].numPendingTests},
     workerd:{version:$workerd,path:$engine,sha256:$sha,successfulExecveByVitest:$spawns},
     isolation:"private mount namespace; private network with loopback only; no hosted services"},
   scope:"One unchanged Durable Cloudflare Vitest migration original. No other test-workers cases, native Go parity, full workspace install, or deployment."}'
