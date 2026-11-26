#!/usr/bin/env bash
# Usage: bash probe-bedrock-sdk-input.sh <upstream git mirror> <staged oracle> <npm _cacache>
# Reads the existing npm content object by lock SRI. Writes the receipt to stdout.
set -euo pipefail

mirror=$(realpath "$1")
oracle=$(realpath "$2")
cache=$(realpath "$3")
pin=ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe
sdk=@aws-sdk/client-bedrock-runtime
lock_key=node_modules/$sdk
provider=packages/ai/src/api/bedrock-converse-stream.ts
manifest=packages/ai/package.json
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT

for path in package-lock.json "$manifest" "$provider"; do
  git -C "$mirror" show "$pin:$path" | cmp - "$oracle/$path"
done
version=$(jq -er --arg sdk "$sdk" '.dependencies[$sdk]' "$oracle/$manifest")
lock=$(jq -ce --arg k "$lock_key" --arg v "$version" '.packages[$k] | select(.version == $v) | {version, resolved, integrity, engines}' "$oracle/package-lock.json")
integrity=$(jq -er '.integrity | select(startswith("sha512-"))' <<< "$lock")
hex=$(printf '%s' "${integrity#sha512-}" | base64 --decode | od -An -v -tx1 | tr -d ' \n')
archive=$cache/content-v2/sha512/${hex:0:2}/${hex:2:2}/${hex:4}
printf '%s  %s\n' "$hex" "$archive" | sha512sum --check --status
sha256() { sha256sum < "$1" | cut -d ' ' -f 1; }
bytes() { wc -c < "$1" | tr -d ' '; }

tar -xOzf "$archive" package/package.json > "$work/package.json"
cmp "$work/package.json" "$oracle/$lock_key/package.json"
entry=$(jq -er --arg name "$sdk" --arg version "$version" 'select(.name == $name and .version == $version) | .main | select(. == "./dist-cjs/index.js")' "$work/package.json")
member=package/${entry#./}
installed=$lock_key/${entry#./}
tar -xOzf "$archive" "$member" > "$work/entry"
cmp "$work/entry" "$oracle/$installed"
entry_sha=$(sha256 "$work/entry")

# No credentials, requests, clients or test runner; importing the provider is
# enough to witness which SDK entry Node loads. The oracle stays read-only.
runtime=$(env -i PATH="$PATH" ORACLE="$oracle" PROVIDER="$provider" SDK="$sdk" \
  ENTRY="$installed" ENTRY_SHA="$entry_sha" \
  node --permission --allow-fs-read="$oracle" --conditions=source --input-type=module - <<'JS'
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { registerHooks } from 'node:module';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

const { ORACLE, PROVIDER, SDK, ENTRY, ENTRY_SHA } = process.env;
const url = path => pathToFileURL(resolve(ORACLE, path)).href;
const resolved = [];
const loaded = [];
const hooks = registerHooks({
  resolve(specifier, context, nextResolve) {
    const result = nextResolve(specifier, context);
    if (specifier === SDK) resolved.push([context.parentURL, result.url]);
    return result;
  },
  load(address, context, nextLoad) {
    const result = nextLoad(address, context);
    if (address === url(ENTRY)) {
      const source = Buffer.from(result.source);
      loaded.push({ format: result.format, bytes: source.length, sha256: createHash('sha256').update(source).digest('hex') });
    }
    return result;
  },
});
const provider = await import(url(PROVIDER));
hooks.deregister();
assert.deepEqual(resolved, [[url(PROVIDER), url(ENTRY)]]);
assert.equal(loaded.length, 1);
assert.equal(loaded[0].format, 'commonjs');
assert.equal(loaded[0].sha256, ENTRY_SHA);
assert.equal(typeof provider.stream, 'function');
assert.equal(typeof provider.streamSimple, 'function');
console.log(JSON.stringify({ action: 'import only', node: process.version, provider: PROVIDER, resolved: ENTRY, load: loaded[0] }));
JS
)

jq -n --arg pin "$pin" --arg lockSHA "$(sha256 "$oracle/package-lock.json")" \
  --arg key "$lock_key" --argjson lock "$lock" \
  --arg archiveSHA "$(sha256 "$archive")" --argjson archiveBytes "$(bytes "$archive")" \
  --arg manifestSHA "$(sha256 "$work/package.json")" \
  --arg member "$member" --arg entrySHA "$entry_sha" --argjson entryBytes "$(bytes "$work/entry")" \
  --arg providerSHA "$(sha256 "$oracle/$provider")" --argjson runtime "$runtime" \
  '{pin: $pin, lockfileSHA256: $lockSHA, lock: ($lock + {key: $key}), archive: {sha256: $archiveSHA, bytes: $archiveBytes, manifestSHA256: $manifestSHA, entry: {member: $member, sha256: $entrySHA, bytes: $entryBytes}}, staged: {manifestAndEntryMatchArchive: true, providerSHA256: $providerSHA}, runtime: $runtime}'
