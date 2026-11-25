#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 3 || $# -gt 4 || ( $# == 4 && $4 != auth-wire ) ]]; then
  printf 'Usage: bash %s <pinned-git-mirror> <oracle-dir> <retained-archive-dir> [auth-wire]\n' "$0" >&2
  exit 1
fi
here=$(cd -- "$(dirname -- "$0")" && pwd -P)
mirror=$(realpath -- "$1")
oracle=$(realpath -- "$2")
archives=$(realpath -- "$3")
pin=faac585884ae2c013a6b1400f3e9e77f10b34bac
base=b0fd6a8affdfb77f902a286867944b6483135d1e
receipt=docs/upstream-v1.6.0/dependencies/provider-archives.jsonl
key='node_modules/@google/genai'
member='package/dist/node/index.mjs'
entry="$key/${member#package/}"
test='test/google-stop-reason-continuation.test.ts'
selector='ends the turn as length instead of letting the SDK send a continuation request'
full_name="Google CONTINUATION finish reason $selector"
scratch=$(mktemp -d /tmp/v160-google-sdk-load.XXXXXX)
trap 'rm -r -- "$scratch"' EXIT
mkdir -p "$scratch/packages" "$scratch/home" "$scratch/tmp"

# Bind the archive to the official lock and the earlier acquisition receipt.
git -C "$mirror" show "$pin:package-lock.json" > "$scratch/lock.json"
git -C "$here" show "$base:$receipt" > "$scratch/receipt.jsonl"
lock_sha=$(sha256sum "$scratch/lock.json" | cut -d' ' -f1)
jq -e --arg pin "$pin" --arg sha "$lock_sha" 'select(.kind=="manifest") | .pin==$pin and .lockSha256==$sha' "$scratch/receipt.jsonl" > "$scratch/receipt-pin.check"
read -r version url integrity < <(jq -er --arg key "$key" '.packages[$key] | [.version,.resolved,.integrity] | @tsv' "$scratch/lock.json")
[[ "$version" == 2.32.0 && "$url" == 'https://registry.npmjs.org/@google/genai/-/genai-2.32.0.tgz' ]]
archive="$archives/${url##*/}"
archive_sha=$(sha256sum "$archive" | cut -d' ' -f1)
jq -e --arg key "$key" --arg sha "$archive_sha" --arg integrity "$integrity" --arg url "$url" 'select(.kind=="archive" and .lockKey==$key) | .state=="verified-archive-bytes" and .sha256==$sha and .integrity==$integrity and .url==$url' "$scratch/receipt.jsonl" > "$scratch/archive-receipt.check"
[[ "sha512-$(openssl dgst -sha512 -binary "$archive" | openssl base64 -A)" == "$integrity" ]]
tar -xOzf "$archive" -- "$member" > "$scratch/archive-module"
member_sha=$(sha256sum "$scratch/archive-module" | cut -d' ' -f1)
member_bytes=$(wc -c < "$scratch/archive-module")

# Use a disposable copy so Vitest's generated caches and the mutation cannot touch retained inputs.
cp -a --reflink=auto "$oracle/node_modules" "$oracle/package.json" "$scratch/"
cp -a --reflink=auto "$oracle/packages/ai" "$oracle/packages/schema" "$oracle/packages/telemetry" "$scratch/packages/"
cp "$here/sdk-load-witness.mjs" "$scratch/witness.mjs"
for file in package.json packages/ai/package.json packages/ai/vitest.config.ts "packages/ai/$test" \
    packages/ai/src/api/google-generative-ai.ts packages/ai/src/api/google-vertex.ts packages/ai/src/api/google-shared.ts; do
  git -C "$mirror" show "$pin:$file" | cmp "$scratch/$file" -
done
[[ "$(realpath -- "$scratch/$entry")" == "$scratch/$entry" ]]
cmp "$scratch/archive-module" "$scratch/$entry"
jq -e --arg version "$version" '.version==$version and .exports["."].node.import=="./dist/node/index.mjs"' "$scratch/$key/package.json" > "$scratch/package.check"
node_version=$(node --version)
vitest_version=$(jq -er '.version' "$scratch/node_modules/vitest/package.json")
[[ "$vitest_version" == 4.1.12 ]]

# The unchanged case supplies a stub fetch and dummy key. No network permission or credentials are passed to Node.
exec 8>/tmp/go-ai/v160-focused-verify.lock
flock 8
run_selected() {
  local tag=$1
  if ! (cd "$scratch/packages/ai" && env -i PATH="$PATH" HOME="$scratch/home" TMPDIR="$scratch/tmp" \
      SDK_LOAD_ROOT="$scratch" SDK_LOAD_TRACE="$scratch/$tag.trace.jsonl" \
      NODE_OPTIONS="--max-old-space-size=768 --permission --allow-fs-read=$scratch --allow-fs-write=$scratch --allow-child-process --allow-worker --allow-addons --import=$scratch/witness.mjs" \
      node ../../node_modules/vitest/vitest.mjs run "$test" -t "$selector" --pool=forks --maxWorkers=1 --reporter=json \
      --outputFile "$scratch/$tag.result.json") > "$scratch/$tag.log" 2>&1; then
    cat "$scratch/$tag.log" >&2
    return 1
  fi
  jq -e --arg full "$full_name" '
    .numPassedTests==1 and .numFailedTests==0 and
    ([.testResults[].assertionResults[] | select(.status=="passed") | .fullName]==[$full])
  ' "$scratch/$tag.result.json" > "$scratch/$tag.pass.check"
}
verify_witness() {
  local tag=$1
  if ! jq -es --arg entry "$entry" 'length==1 and .[0].entry==$entry and .[0].format=="module"' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.load.check"; then
    printf 'Expected one observed SDK ESM load\n' >&2
    return 1
  fi
  if ! jq -e --arg sha "$member_sha" --argjson bytes "$member_bytes" '.sha256==$sha and .bytes==$bytes' "$scratch/$tag.trace.jsonl" > "$scratch/$tag.digest.check"; then
    printf 'Loaded SDK bytes differ from the pinned archive member\n' >&2
    return 13
  fi
}
if [[ ${4:-load} == auth-wire ]]; then
  source_test="packages/ai/$test"
  source_sha=$(sha256sum "$scratch/$source_test" | cut -d' ' -f1)
  test='test/google-standalone-auth-v160-supplemental.test.ts'
  selector='observes the pinned standalone wire credential'
  full_name=$selector
  # Reuse the pinned case's imports, context and model in a separate scratch test.
  head -n 27 "$scratch/$source_test" > "$scratch/packages/ai/$test"
  cat >> "$scratch/packages/ai/$test" <<'TS'
it("observes the pinned standalone wire credential", async () => {
	const requests: unknown[] = [];
	const key = "a+b/c=";
	const fetch: FetchFunction = async (input, init) => {
		const request = new Request(input, init);
		requests.push({ method: request.method, url: request.url, key: request.headers.get("x-goog-api-key") });
		const body = { candidates: [{ content: { role: "model", parts: [{ text: "partial" }] }, finishReason: "CONTINUATION", continuationToken: "token-1" }] };
		return new Response(`data: ${JSON.stringify(body)}\n\n`, { headers: { "content-type": "text/event-stream" } });
	};
	const result = await streamGoogleGenerativeAI(createModel("google-generative-ai"), context, { apiKey: key, fetch }).result();
	expect(result.stopReason).toBe("length");
	expect(requests).toEqual([{ method: "POST", url: "https://upstream.test/v1/models/test-model:streamGenerateContent?alt=sse", key }]);
});
TS
  run_selected wire
  verify_witness wire
  jq -n --arg pin "$pin" --arg node "$node_version" --arg vitest "$vitest_version" \
    --arg source "$source_test" --arg sourceSha "$source_sha" --arg selector "$selector" \
    --arg archiveSha "$archive_sha" --slurpfile witness "$scratch/wire.trace.jsonl" '
    {format:1,pin:$pin,node:$node,vitest:$vitest,fixtureSource:{file:$source,sha256:$sourceSha},
     fixture:{name:$selector,passed:1},archiveSha256:$archiveSha,loaded:$witness[0],
     checkedRequest:{count:1,method:"POST",url:"https://upstream.test/v1/models/test-model:streamGenerateContent?alt=sse",credentialHeader:"x-goog-api-key",preservesKeyBytes:true,stopReason:"length"},
     scope:"supplemental-standalone-auth-wire"}'
  exit
fi

run_selected original
verify_witness original

# The source assertion must still pass with a legal JS byte mutation. Provenance must reject the same load.
printf '\n' >> "$scratch/$entry"
run_selected mutated
mutation_result=0
verify_witness mutated || mutation_result=$?
[[ "$mutation_result" == 13 ]]
mutated_sha=$(sha256sum "$scratch/$entry" | cut -d' ' -f1)
jq -e --arg sha "$mutated_sha" --argjson n "$((member_bytes + 1))" '.sha256==$sha and .bytes==$n' "$scratch/mutated.trace.jsonl" > "$scratch/mutation.check"
flock -u 8

git -C "$mirror" show "$pin:packages/ai/$test" | cmp "$scratch/packages/ai/$test" -
test_sha=$(sha256sum "$scratch/packages/ai/$test" | cut -d' ' -f1)
jq -n --arg pin "$pin" --arg base "$base" --arg node "$node_version" --arg vitest "$vitest_version" \
  --arg test "packages/ai/$test" --arg testSha "$test_sha" --arg selector "$selector" --arg full "$full_name" \
  --arg url "$url" --arg integrity "$integrity" --arg archiveSha "$archive_sha" --arg member "$member" \
  --slurpfile original "$scratch/original.trace.jsonl" --slurpfile mutated "$scratch/mutated.trace.jsonl" '
  {format:1,pin:$pin,evidenceBase:$base,node:$node,vitest:$vitest,
    selectedOriginal:{test:$test,sha256:$testSha,selector:$selector,fullName:$full,passed:1},
    archive:{url:$url,integrity:$integrity,sha256:$archiveSha,member:$member},loaded:$original[0],
    control:{change:"append-one-LF-to-copied-SDK-entry",selectedOriginalPassed:1,loaded:$mutated[0],result:"archive-digest-rejected"},
    scope:"new-focused-run-only"}'
