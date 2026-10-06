#!/usr/bin/env bash
# Source for direct commands and helper entrypoints. Retained evidence is never
# mixed into disposable build/tmp or cache trees. CI maps the same hierarchy to
# its runner-owned temporary directory; installed toolchains are not relocated.
PROJECT=go-ai
source "$(dirname "${BASH_SOURCE[0]}")/project-tmp.sh"
PROJECT_TMP_ROOT=$(project_tmp_resolve "$PROJECT") || { return 2 2>/dev/null || exit 2; }
export PROJECT_TMP_ROOT
project_tmp_init "$PROJECT_TMP_ROOT" || { return 2 2>/dev/null || exit 2; }
project_path_usable "$PROJECT_TMP_ROOT/evidence" || { return 2 2>/dev/null || exit 2; }
mkdir -p "$PROJECT_TMP_ROOT/evidence"
export GOCACHE=${GOCACHE:-$PROJECT_TMP_ROOT/cache/go-build}
export GOMODCACHE=${GOMODCACHE:-$PROJECT_TMP_ROOT/cache/go-mod}
export GOPATH=${GOPATH:-$PROJECT_TMP_ROOT/cache/go-path}
export XDG_CACHE_HOME=${XDG_CACHE_HOME:-$PROJECT_TMP_ROOT/cache/xdg}
export npm_config_cache=${npm_config_cache:-$PROJECT_TMP_ROOT/cache/npm}
export BUN_INSTALL_CACHE_DIR=${BUN_INSTALL_CACHE_DIR:-$PROJECT_TMP_ROOT/cache/bun}
export PYTHONPYCACHEPREFIX=${PYTHONPYCACHEPREFIX:-$PROJECT_TMP_ROOT/cache/python}
export GO_AI_MODEL_REGEN_CACHE=${GO_AI_MODEL_REGEN_CACHE:-$PROJECT_TMP_ROOT/cache/model-regeneration}
# Incoming TMPDIR is a fallback BASE (snapshotted by the resolver), not child
# scratch. Preserve already-routed owned run directories on nested invocation.
case ${TMPDIR:-} in "$PROJECT_TMP_ROOT"/*) ;; *) TMPDIR="$PROJECT_TMP_ROOT/build/tmp";; esac
export TMPDIR
for name in GOTMPDIR TMP TEMP; do
  case ${!name:-} in "$PROJECT_TMP_ROOT"/*) ;; *) printf -v "$name" '%s' "$TMPDIR";; esac
  export "$name"
done
export PROFILE_ROOT=${PROFILE_ROOT:-$PROJECT_TMP_ROOT/evidence/profiles}
for name in GOCACHE GOMODCACHE GOPATH XDG_CACHE_HOME npm_config_cache BUN_INSTALL_CACHE_DIR PYTHONPYCACHEPREFIX GO_AI_MODEL_REGEN_CACHE TMPDIR GOTMPDIR TMP TEMP PROFILE_ROOT; do
  value=${!name}
  case "$value" in "$PROJECT_TMP_ROOT"/*) ;; *) echo "go-ai: $name must be below $PROJECT_TMP_ROOT" >&2; return 2 2>/dev/null || exit 2;; esac
  project_path_usable "$value" || { echo "go-ai: unsafe $name path" >&2; return 2 2>/dev/null || exit 2; }
  mkdir -p "$value" || { return 2 2>/dev/null || exit 2; }
done
