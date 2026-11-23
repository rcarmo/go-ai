#!/usr/bin/env bash
set -euo pipefail

if [[ $# != 2 ]]; then
  printf 'Usage: bash %s <pinned-git-mirror> <prepared-oracle-dir>\n' "$0" >&2
  exit 1
fi
mirror=$(realpath -- "$1")
oracle=$(realpath -- "$2")
here=$(cd -- "$(dirname -- "$0")" && pwd -P)
cd "$(git -C "$here" rev-parse --show-toplevel)"
# shellcheck source=/dev/null
source scripts/project-env.sh
export CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1
export GOTOOLCHAIN=go1.27.3 GOPROXY=off GOSUMDB=off GOWORK=off GOVCS='*:off'

pin=faac585884ae2c013a6b1400f3e9e77f10b34bac
original=packages/durable/test/sqlite-migrations.test.ts
selector='durable SQLite migrations migrates a version 1 database to store sleeping tasks, keeping its tasks and indexes'
native=TestNativeSQLiteMigrationEngine
scratch=$(mktemp -d "$TMPDIR/sqlite-migration-engines.XXXXXX")
trap 'rm -r -- "$scratch"' EXIT
mkdir -p "$scratch/home" "$scratch/tmp"
sha256() { sha256sum -- "$1" | cut -d' ' -f1; }

# Use pinned workspace source and the existing host-compatible runner closure.
# Both the test and its Vitest config come from Git, without edits.
git -C "$mirror" archive "$pin" package.json packages/{durable,chord,schema,telemetry,codemode,ai}/{package.json,src} \
  packages/durable/vitest.config.ts packages/durable/test/setup-document-retention.ts "$original" | tar -x -C "$scratch"
git -C "$mirror" show "$pin:package-lock.json" > "$scratch/lock.json"
libc=$(node -p "process.report.getReport().header.glibcVersionRuntime ? 'glibc' : 'musl'")
bun run "$here/copy-openai-probe-deps.ts" "$here/npm-closure.jsonl" "$(sha256 "$scratch/lock.json")" \
  "$oracle" "$scratch" "$libc" node_modules/vitest node_modules/diff > "$scratch/copied.json"
mkdir -p "$scratch/node_modules/@earendil-works"
for p in chord pi-schema pi-telemetry pi-codemode pi-ai; do
  ln -s "../../packages/${p#pi-}" "$scratch/node_modules/@earendil-works/$p"
done
cp -- "$here/sqlite-migration-engine-witness.mjs" "$scratch/witness.mjs"
cp -- "$here/sqlite-migration-engine-test.go.txt" "$scratch/native_test.go"
jq -n --arg path "$PWD/durable/storage_sqlite_engine_audit_test.go" --arg backing "$scratch/native_test.go" \
  '{Replace:{($path):$backing}}' > "$scratch/native.overlay.json"

exec 9>/tmp/go-ai/v160-focused-verify.lock
flock 9
(
  cd "$scratch/packages/durable"
  env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" SQLITE_ENGINE_TRACE="$scratch/node.trace" \
    NODE_OPTIONS="--max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons --import=$scratch/witness.mjs" \
    node ../../node_modules/vitest/vitest.mjs run test/sqlite-migrations.test.ts -t "$selector" --pool=forks --maxWorkers=1 \
    --reporter=json --outputFile "$scratch/original.result.json"
) > "$scratch/original.log" 2>&1 || { cat "$scratch/original.log" >&2; exit 1; }
jq -e --arg full "$selector" '.numPassedTests==1 and .numFailedTests==0 and
  ([.testResults[].assertionResults[]|select(.status=="passed")|.fullName]==[$full])' \
  "$scratch/original.result.json" > "$scratch/original.check"
jq -es 'length==1 and .[0].schemaVersionAtDDL==1 and .[0].sqlite.version==.[0].nodeSQLiteVersion' \
  "$scratch/node.trace" > "$scratch/node.check"

[[ $(go env GOVERSION) == go1.27.3 ]]
go test -mod=readonly -overlay "$scratch/native.overlay.json" -c -o "$scratch/native.test" ./durable
(
  cd durable
  SQLITE_ENGINE_TRACE="$scratch/native.trace" TMPDIR="$scratch/tmp" "$scratch/native.test" \
    -test.run "^$native$" -test.timeout 110s -test.v
) > "$scratch/native.log" 2>&1 || { cat "$scratch/native.log" >&2; exit 1; }
[[ $(grep -Ec -- "^--- PASS: $native \\([0-9.]+s\\)$" "$scratch/native.log") == 1 ]]
go version -m -json "$scratch/native.test" > "$scratch/build-info.json"
platform="$(go env GOOS)/$(go env GOARCH)"
flock -u 9

jq -e '.driver=="modernc.org/sqlite" and .schemaVersion==2' "$scratch/native.trace" > "$scratch/native-engine.check"

# Both migration runs pass. The exact comparison passes for the observed
# identities and rejects a scratch receipt with the two identities exchanged.
jq -n --slurpfile node "$scratch/node.trace" --slurpfile go "$scratch/native.trace" \
  '[$node[0].sqlite,$go[0].sqlite]' > "$scratch/identities.json"
jq 'reverse' "$scratch/identities.json" > "$scratch/swapped.json"
compare_identities() {
  jq -e --slurpfile expected "$scratch/identities.json" '.==$expected[0]' "$1"
}
compare_identities "$scratch/identities.json" > "$scratch/engine.check"
if compare_identities "$scratch/swapped.json" > "$scratch/engine.control"; then
  printf 'Control unexpectedly matched the observed SQLite identities\n' >&2
  exit 1
else
  [[ $? == 1 ]] # jq evaluated false; an invocation or parsing error is not a control pass.
fi

jq -n --arg pin "$pin" --arg selector "$selector" --arg original "$original" --arg native "$native" \
  --arg originalSha "$(sha256 "$scratch/$original")" --arg nodeExecutableSha "$(sha256 "$(realpath "$(command -v node)")")" \
  --arg vitest "$(jq -r '.version' "$scratch/node_modules/vitest/package.json")" --arg platform "$platform" \
  --slurpfile node "$scratch/node.trace" --slurpfile go "$scratch/native.trace" --slurpfile build "$scratch/build-info.json" '
  $build[0] as $info |
  ($info.Settings | map({key:.Key,value:.Value}) | from_entries) as $settings |
  ($info.Deps | map(select(.Path=="modernc.org/sqlite" or .Path=="modernc.org/libc") |
    if .Replace!=null then error("unexpected module replacement") else {path:.Path,version:.Version,sum:.Sum} end)) as $modules |
  if ($modules|length)!=2 or $info.GoVersion!="go1.27.3" or $settings.CGO_ENABLED!="0" or
    ($settings.GOOS+"/"+$settings.GOARCH)!=$platform then error("unexpected Go build information") else
  {format:1,pin:$pin,platform:$platform,
    oracle:{test:$original,sha256:$originalSha,selector:$selector,passed:1,vitest:$vitest,nodeExecutableSha256:$nodeExecutableSha,observed:$node[0]},
    native:{test:$native,passed:1,go:$info.GoVersion,cgoEnabled:false,modules:$modules,observed:$go[0]},
    control:{type:"swap-runtime-sqlite-version-and-source-id",identityCheck:"rejected",migrationTestsPassed:2}}
  end'
