#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

go_cmd="${GO:-go}"
tmp_root="${TMPDIR:-${GO_TMPDIR:-/tmp}}"
cache_base="${GO_AI_MODEL_REGEN_CACHE:-${XDG_CACHE_HOME:-${HOME:-$tmp_root}/.cache}/go-ai/model-regeneration}"

upstream_repo_url="${PI_AI_UPSTREAM_REPO_URL:-https://github.com/earendil-works/pi.git}"
upstream_tag="v0.84.1"
upstream_sha="53fa77ccd8a279eb87e92294ef3687b03ff80112"
npm_url="${PI_AI_NPM_TARBALL_URL:-https://registry.npmjs.org/@earendil-works/pi-ai/-/pi-ai-0.84.1.tgz}"
npm_sha256="6ab689189e7cb3de5cdb126312a3e60e8ac35fe5ee5f1b63d00f711c8a430c73"

workdir="$(mktemp -d "${tmp_root%/}/go-ai-model-regen.XXXXXX")"
cleanup() {
  rm -rf "$workdir"
}
trap cleanup EXIT

fetch_file() {
  local url="$1"
  local out="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$out"
    return
  fi
  if command -v python3 >/dev/null 2>&1; then
    python3 - "$url" "$out" <<'PY'
import sys
import urllib.request
url, out = sys.argv[1], sys.argv[2]
with urllib.request.urlopen(url) as response, open(out, "wb") as fh:
    fh.write(response.read())
PY
    return
  fi
  echo "cannot fetch $url: install curl or python3" >&2
  return 1
}

sha256_file() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
    return
  fi
  if command -v python3 >/dev/null 2>&1; then
    python3 - "$file" <<'PY'
import hashlib
import sys
with open(sys.argv[1], "rb") as fh:
    print(hashlib.sha256(fh.read()).hexdigest())
PY
    return
  fi
  echo "cannot compute sha256: install sha256sum, shasum, or python3" >&2
  return 1
}

ensure_source_checkout() {
  local dir="$cache_base/pi-$upstream_sha"
  if [[ ! -d "$dir/.git" ]] || [[ "$(git -C "$dir" rev-parse HEAD 2>/dev/null || true)" != "$upstream_sha" ]]; then
    rm -rf "$dir"
    mkdir -p "$dir"
    git -C "$dir" init -q
    git -C "$dir" remote add origin "$upstream_repo_url"
    git -C "$dir" fetch -q --depth 1 origin "refs/tags/$upstream_tag"
    git -C "$dir" checkout -q --detach FETCH_HEAD
  fi
  local got
  got="$(git -C "$dir" rev-parse HEAD)"
  if [[ "$got" != "$upstream_sha" ]]; then
    echo "upstream source checkout resolved to $got, want $upstream_sha" >&2
    return 1
  fi
  printf '%s\n' "$dir"
}

ensure_npm_package() {
  local dir="$cache_base/pi-ai-0.84.1-package"
  local marker="$dir/.sha256"
  if [[ ! -d "$dir/package/dist/providers/data" ]] || [[ "$(cat "$marker" 2>/dev/null || true)" != "$npm_sha256" ]]; then
    rm -rf "$dir"
    mkdir -p "$dir"
    local tgz="$workdir/pi-ai-0.84.1.tgz"
    fetch_file "$npm_url" "$tgz"
    local got
    got="$(sha256_file "$tgz")"
    if [[ "$got" != "$npm_sha256" ]]; then
      echo "npm tarball sha256 mismatch: got $got, want $npm_sha256" >&2
      return 1
    fi
    tar -xzf "$tgz" -C "$dir"
    printf '%s\n' "$npm_sha256" > "$marker"
  fi
  printf '%s\n' "$dir/package"
}

if [[ -n "${PI_AI_MODELS_GENERATED_TS:-}" ]]; then
  source_models_ts="$PI_AI_MODELS_GENERATED_TS"
else
  source_checkout="$(ensure_source_checkout)"
  source_models_ts="$source_checkout/packages/ai/src/models.generated.ts"
fi

if [[ -n "${PI_AI_MODEL_DATA_DIR:-}" ]]; then
  provider_data_dir="$PI_AI_MODEL_DATA_DIR"
else
  npm_package="$(ensure_npm_package)"
  provider_data_dir="$npm_package/dist/providers/data"
fi

generated="$workdir/models_generated.go"
want_norm="$workdir/want.go"
got_norm="$workdir/got.go"

if [[ ! -f "$source_models_ts" ]]; then
  echo "model regeneration source not found: $source_models_ts" >&2
  echo "Set PI_AI_MODELS_GENERATED_TS to the exact upstream src/models.generated.ts" >&2
  exit 1
fi
if [[ ! -d "$provider_data_dir" ]]; then
  echo "model provider data dir not found: $provider_data_dir" >&2
  echo "Set PI_AI_MODEL_DATA_DIR to the exact published provider data directory" >&2
  exit 1
fi

(
  cd "$repo_root"
  PI_AI_MODEL_DATA_DIR="$provider_data_dir" "$go_cmd" run ./scripts/generate-models.go -input "$source_models_ts" -output "$generated" >/dev/null
)
"$go_cmd" fmt "$generated" >/dev/null

normalize() {
  sed -E 's#^// Generated: .*#// Generated: <normalized>#' "$1"
}

normalize "$repo_root/models_generated.go" > "$want_norm"
normalize "$generated" > "$got_norm"

diff -u "$want_norm" "$got_norm" >/dev/null || {
  echo "models_generated.go does not match normalized regeneration from exact upstream artifacts" >&2
  echo "source: $source_models_ts" >&2
  echo "provider data: $provider_data_dir" >&2
  diff -u "$want_norm" "$got_norm" >&2 || true
  exit 1
}

echo "model regeneration metadata comparator passed"
