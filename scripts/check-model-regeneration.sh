#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

go_cmd="${GO:-go}"
tmp_root="${TMPDIR:-${GO_TMPDIR:-/tmp}}"
cache_base="${GO_AI_MODEL_REGEN_CACHE:-${XDG_CACHE_HOME:-${HOME:-$tmp_root}/.cache}/go-ai/model-regeneration}"

upstream_repo_url="${PI_AI_UPSTREAM_REPO_URL:-https://github.com/earendil-works/pi.git}"
upstream_tag="v0.99.2"
upstream_sha="${PI_AI_UPSTREAM_SHA:-}"
npm_url="${PI_AI_NPM_TARBALL_URL:-https://registry.npmjs.org/@earendil-works/pi-ai/-/pi-ai-0.99.2.tgz}"
npm_sha256="0b3df8791b488216f309d908789294a744bb61bbaad123d94098e56df9538d25"

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
  local dir="$cache_base/pi-ai-0.99.2-package"
  local marker="$dir/.sha256"
  if [[ ! -f "$dir/package/dist/models.generated.js" ]] || [[ "$(cat "$marker" 2>/dev/null || true)" != "$npm_sha256" ]]; then
    rm -rf "$dir"
    mkdir -p "$dir"
    local tgz="$workdir/pi-ai-0.99.2.tgz"
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

if [[ -n "${PI_AI_MODELS_GENERATED_JS:-}" ]]; then
  source_models_js="$PI_AI_MODELS_GENERATED_JS"
else
  npm_package="$(ensure_npm_package)"
  source_models_js="$npm_package/dist/models.generated.js"
fi

generated_chat="$workdir/models_generated.go"
generated_images="$workdir/image_models_generated.go"
generated_classifiers="$workdir/classifier_models_generated.go"

if [[ ! -f "$source_models_js" ]]; then
  echo "model regeneration source not found: $source_models_js" >&2
  echo "Set PI_AI_MODELS_GENERATED_JS to the exact published dist/models.generated.js" >&2
  exit 1
fi

(
  cd "$repo_root"
  "$go_cmd" run ./scripts/generate-models.go -input "$source_models_js" -kind chat -output "$generated_chat" >/dev/null
  "$go_cmd" run ./scripts/generate-models.go -input "$source_models_js" -kind image -output "$generated_images" >/dev/null
  "$go_cmd" run ./scripts/generate-models.go -input "$source_models_js" -kind classifier -output "$generated_classifiers" >/dev/null
)
"$go_cmd" fmt "$generated_chat" "$generated_images" "$generated_classifiers" >/dev/null

compare_generated() {
  local want="$1"
  local got="$2"
  local label="$3"
  diff -u "$want" "$got" >/dev/null || {
    echo "$label does not match regeneration from exact v0.99.2 schema-v6 catalog" >&2
    echo "source: $source_models_js" >&2
    diff -u "$want" "$got" >&2 || true
    exit 1
  }
}

compare_generated "$repo_root/models_generated.go" "$generated_chat" "models_generated.go"
compare_generated "$repo_root/image_models_generated.go" "$generated_images" "image_models_generated.go"
compare_generated "$repo_root/classifier_models_generated.go" "$generated_classifiers" "classifier_models_generated.go"

echo "chat model regeneration comparator passed"
echo "image model regeneration comparator passed"
echo "classifier model regeneration comparator passed"
