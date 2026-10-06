#!/usr/bin/env python3
"""Portable routing, fail-closed overrides, child propagation and owned scratch."""
import os
from pathlib import Path
import subprocess
import tempfile

from project_temp import configure
configure("project-temp-tests")
SCRIPTS = Path(__file__).resolve().parent
KEYS = ("PROJECT_TMP_BASE", "PROJECT_TMP_ROOT", "PROJECT_ORIGINAL_TMPDIR", "CI", "GITHUB_ACTIONS", "GITLAB_CI", "TF_BUILD", "CIRCLECI", "RUNNER_TEMP", "TMPDIR", "TMP", "TEMP", "GOTMPDIR", "GOCACHE", "GOMODCACHE", "GOPATH", "XDG_CACHE_HOME", "npm_config_cache", "BUN_INSTALL_CACHE_DIR", "PYTHONPYCACHEPREFIX", "GO_AI_MODEL_REGEN_CACHE", "PROFILE_ROOT")


def shell(command, env):
    return subprocess.run(["bash", "-c", command], env=env, capture_output=True, text=True)


with tempfile.TemporaryDirectory(prefix="run-") as td:
    base = Path(td)
    env = {k: v for k, v in os.environ.items() if k not in KEYS}
    # Ensure the resolver never enables errexit in a profiling caller: failed
    # test executions must continue to CPU/heap analysis.
    policy = shell(f'set +e; source "{SCRIPTS / "project-env.sh"}"; false; echo analysed', env)
    assert policy.returncode == 0 and policy.stdout.strip() == "analysed", policy
    resolver = SCRIPTS / "project-tmp.sh"
    entry = SCRIPTS / "project-env.sh"
    absent = base / "absent" / "workspace" / "tmp"
    for choice in ("RUNNER_TEMP", "TMPDIR"):
        trial = dict(env, CI="true", **{choice: str(base / choice)})
        got = shell(f'source "{resolver}"; project_tmp_resolve go-ai "{absent}"', trial)
        assert got.returncode == 0, got.stderr
        assert got.stdout.strip() == str(base / choice / "go-ai"), got.stdout
    platform = shell(f'source "{resolver}"; project_tmp_resolve go-ai "{absent}"', env)
    assert platform.returncode == 0 and platform.stdout.strip() == "/tmp/go-ai", platform
    # CI never prefers a writable workspace over runner scratch.
    ci = shell(f'source "{resolver}"; project_tmp_resolve go-ai', dict(env, CI="true", RUNNER_TEMP=str(base / "runner")))
    assert ci.returncode == 0 and ci.stdout.strip() == str(base / "runner/go-ai"), ci
    local = shell(f'source "{resolver}"; project_tmp_resolve go-ai "{absent}"', dict(env, RUNNER_TEMP=str(base / "runner"), TMPDIR=str(base / "inherited")))
    assert local.returncode == 0 and local.stdout.strip() == "/tmp/go-ai", local
    # Exercise full direct-helper setup, including rebasing inherited base temp
    # values AFTER resolution, not merely the resolver's path string.
    for trial, expected in (
        (dict(env, CI="true", RUNNER_TEMP=str(base / "runner"), TMPDIR=str(base / "old-temp")), base / "runner/go-ai"),
        (dict(env, CI="true", TMPDIR=str(base / "inherited")), base / "inherited/go-ai"),
    ):
        got = shell(f'source "{entry}"; printf "%s\\n" "$PROJECT_TMP_ROOT" "$TMPDIR" "$TMP" "$TEMP" "$GOTMPDIR"', trial)
        assert got.returncode == 0, got.stderr
        assert got.stdout.splitlines() == [str(expected)] + [str(expected / "build/tmp")] * 4, got.stdout
        probe = base / "fallback.py"
        probe.write_text(f'import sys;sys.path.insert(0,{str(SCRIPTS)!r});from project_temp import configure;configure("fallback");import os;print(os.environ["PROJECT_TMP_ROOT"]);print(os.environ["TMPDIR"])')
        got = subprocess.run(["python3", str(probe)], env=trial, capture_output=True, text=True)
        assert got.returncode == 0 and got.stdout.splitlines() == [str(expected), str(expected / "build/tmp")], got.stderr
    root = base / "go-ai"
    chosen = shell(f'source "{entry}"; echo "$PROJECT_TMP_ROOT"', dict(env, PROJECT_TMP_BASE=str(base)))
    assert chosen.returncode == 0 and chosen.stdout.strip() == str(root), chosen
    conflict = shell(f'source "{entry}"', dict(env, PROJECT_TMP_BASE=str(base), PROJECT_TMP_ROOT=str(base / "different/go-ai")))
    assert conflict.returncode != 0, "conflicting base/root accepted"
    empty_base = shell(f'source "{entry}"', dict(env, PROJECT_TMP_BASE=""))
    assert empty_base.returncode != 0, "empty base accepted"
    agree = shell(f'source "{entry}"; echo "$PROJECT_TMP_ROOT"', dict(env, PROJECT_TMP_BASE=str(base), PROJECT_TMP_ROOT=str(root)))
    assert agree.returncode == 0 and agree.stdout.strip() == str(root), agree
    explicit = dict(env, PROJECT_TMP_ROOT=str(root))
    got = shell(f'source "{entry}"; printf "%s\\n" "$PROJECT_TMP_ROOT" "$TMPDIR" "$GOCACHE"; source "{entry}"; printf "%s\\n" "$PROJECT_TMP_ROOT"', explicit)
    assert got.returncode == 0, got.stderr
    assert got.stdout.splitlines() == [str(root), str(root / "build/tmp"), str(root / "cache/go-build"), str(root)]
    owned = root / "runs/child/run-owned"
    owned.mkdir(parents=True)
    nested = shell(f'source "{entry}"; printf "%s\\n" "$TMPDIR" "$GOTMPDIR"', dict(explicit, TMPDIR=str(owned), GOTMPDIR=str(owned)))
    assert nested.returncode == 0 and nested.stdout.splitlines() == [str(owned), str(owned)], nested
    make = subprocess.run(["make", "project-paths", f"PROJECT_TMP_BASE={base}"], cwd=SCRIPTS.parent, env=env, capture_output=True, text=True)
    assert make.returncode == 0 and f"PROJECT_TMP_ROOT={root}" in make.stdout, make.stderr
    make_bad = subprocess.run(["make", "project-paths", "PROJECT_TMP_ROOT=relative/go-ai"], cwd=SCRIPTS.parent, env=env, capture_output=True, text=True)
    assert make_bad.returncode != 0, "Make accepted invalid command-line root"
    for bad in ("relative/go-ai", str(base / "other"), "", str(base / ".." / "go-ai")):
        got = shell(f'source "{entry}"', dict(env, PROJECT_TMP_ROOT=bad))
        assert got.returncode != 0, f"unsafe override passed: {bad}"
    symlink = base / "link"
    symlink.symlink_to(root, target_is_directory=True)
    got = shell(f'source "{entry}"', dict(env, PROJECT_TMP_ROOT=str(symlink / "go-ai")))
    assert got.returncode != 0, "symlink ancestor accepted"
    got = shell(f'source "{entry}"', dict(explicit, GOCACHE=str(base / "outside")))
    assert got.returncode != 0, "outside cache accepted"
    # Direct Python configuration uses the identical resolver, inherited root,
    # unique tempfile directories and project-scoped child Go scratch.
    python = f'import sys;sys.path.insert(0,{str(SCRIPTS)!r});from project_temp import configure;configure("probe");import os,tempfile;print(os.environ["PROJECT_TMP_ROOT"]);print(tempfile.mkdtemp(prefix="run-"))'
    probe = base / "probe.py"
    probe.write_text(python)
    bad_python = subprocess.run(["python3", str(probe)], env=dict(env, PROJECT_TMP_ROOT=str(base / "other")), capture_output=True, text=True)
    assert bad_python.returncode != 0, "Python silently accepted unsafe root"
    got = subprocess.run([os.environ.get("PYTHON", "python3"), str(probe)], env=explicit, capture_output=True, text=True)
    assert got.returncode == 0, got.stderr
    lines = got.stdout.splitlines()
    assert lines[0] == str(root) and Path(lines[1]).is_relative_to(root / "runs/probe"), lines
print("portable project routing, unsafe overrides, propagation and isolated scratch passed")
