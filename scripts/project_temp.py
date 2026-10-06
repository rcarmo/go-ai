"""Use the vendored shell resolver for direct Python helpers and their children."""
from __future__ import annotations

import os
from pathlib import Path
import subprocess
import sys
import tempfile

# Direct imports must not create a source-tree __pycache__ before routing is set.
sys.dont_write_bytecode = True


def configure(purpose: str) -> None:
    names = (
        "PROJECT_TMP_ROOT", "GOCACHE", "GOMODCACHE", "GOPATH", "XDG_CACHE_HOME",
        "npm_config_cache", "BUN_INSTALL_CACHE_DIR", "PYTHONPYCACHEPREFIX",
        "GO_AI_MODEL_REGEN_CACHE", "TMPDIR", "TMP", "TEMP", "GOTMPDIR", "PROFILE_ROOT",
    )
    script = Path(__file__).with_name("project-env.sh")
    # Select before altering TMPDIR; propagate PROJECT_TMP_ROOT to prevent nesting.
    # Query only the named routing fields, never emit the full process environment.
    if not purpose or purpose.startswith((".", "-")) or any(c not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-" for c in purpose):
        raise ValueError("invalid temporary run purpose")
    command = ('source "$1" >/dev/null || exit 2; scratch="$PROJECT_TMP_ROOT/runs/$2"; '
               'project_path_usable "$scratch" || exit 2; mkdir -p "$scratch" || exit 2; printf "%s\\0" '
               + " ".join(f'"${{{name}}}"' for name in names))
    result = subprocess.run(["bash", "-c", command, "project-temp", str(script), purpose],
                            capture_output=True, check=False)
    if result.returncode:
        raise RuntimeError(result.stderr.decode(errors="replace").strip())
    values = result.stdout.decode().split("\0")[:-1]
    if len(values) != len(names):
        raise RuntimeError("project environment returned incomplete routing fields")
    os.environ.update(zip(names, values))
    # TemporaryDirectory adds a unique owned run below this purpose directory.
    tempfile.tempdir = str(Path(os.environ["PROJECT_TMP_ROOT"]) / "runs" / purpose)
