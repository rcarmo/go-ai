#!/usr/bin/env python3
"""Verify a native release tag is a pre-existing Rui annotated tag for a runtime commit."""
from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

FULL_SHA_RE = re.compile(r"^[0-9a-f]{40}$")
EXPECTED_TAGGER_NAME = "Rui Carmo"
EXPECTED_TAGGER_EMAIL = "rui.carmo@gmail.com"


def fail(message: str) -> int:
    print(f"native tag verification failed: {message}", file=sys.stderr)
    return 1


def run(cmd: list[str], *, cwd: Path | None = None) -> str:
    return subprocess.check_output(cmd, cwd=cwd, text=True, stderr=subprocess.STDOUT).strip()


def validate_inputs(tag: str, runtime_ref: str) -> str | None:
    runtime_ref = runtime_ref.lower()
    if not FULL_SHA_RE.fullmatch(runtime_ref):
        return f"runtime_ref must be a lowercase 40-character Git SHA, got {runtime_ref!r}"
    if not tag or tag.startswith("-") or "/" in tag or ".." in tag:
        return f"tag must be a simple non-empty tag name, got {tag!r}"
    return None


def verify_local(repo: Path, tag: str, runtime_ref: str, tagger_name: str, tagger_email: str) -> int:
    ref = f"refs/tags/{tag}"
    try:
        obj_type = run(["git", "cat-file", "-t", ref], cwd=repo)
    except subprocess.CalledProcessError:
        return fail(f"native release tag {tag} is absent")
    if obj_type != "tag":
        return fail(f"native release tag {tag} exists but is not annotated (type {obj_type})")
    raw = run(["git", "cat-file", "-p", ref], cwd=repo)
    object_sha = ""
    object_type = ""
    tagger = ""
    for line in raw.splitlines():
        if line.startswith("object "):
            object_sha = line.split(" ", 1)[1].strip().lower()
        elif line.startswith("type "):
            object_type = line.split(" ", 1)[1].strip()
        elif line.startswith("tagger "):
            tagger = line.split(" ", 1)[1].strip()
        elif line == "":
            break
    expected_tagger = f"{tagger_name} <{tagger_email}>"
    if object_type != "commit":
        return fail(f"native release tag {tag} targets object type {object_type!r}, expected commit")
    if object_sha != runtime_ref:
        return fail(f"native release tag {tag} targets {object_sha}, expected {runtime_ref}")
    if not tagger.startswith(expected_tagger + " "):
        return fail(f"native release tag {tag} tagger {tagger!r}, expected {expected_tagger!r}")
    print(f"native tag {tag} verified: Rui annotated tag -> {runtime_ref}")
    return 0


def gh_api(path: str) -> dict:
    raw = run(["gh", "api", path])
    return json.loads(raw)


def verify_github(repo: str, tag: str, runtime_ref: str, tagger_name: str, tagger_email: str) -> int:
    try:
        ref_data = gh_api(f"repos/{repo}/git/ref/tags/{tag}")
    except subprocess.CalledProcessError as exc:
        return fail(f"native release tag {tag} is absent or unreadable via Git Data API: {exc.output.strip()}")
    obj = ref_data.get("object") if isinstance(ref_data, dict) else None
    if not isinstance(obj, dict):
        return fail(f"Git Data API ref for {tag} is malformed: {ref_data!r}")
    if obj.get("type") != "tag":
        return fail(f"native release tag {tag} exists but is not annotated (Git Data type {obj.get('type')!r})")
    tag_sha = obj.get("sha")
    if not isinstance(tag_sha, str) or not FULL_SHA_RE.fullmatch(tag_sha.lower()):
        return fail(f"Git Data API tag object SHA for {tag} is malformed: {tag_sha!r}")
    tag_data = gh_api(f"repos/{repo}/git/tags/{tag_sha}")
    tagger = tag_data.get("tagger") if isinstance(tag_data, dict) else None
    target = tag_data.get("object") if isinstance(tag_data, dict) else None
    if not isinstance(tagger, dict) or tagger.get("name") != tagger_name or tagger.get("email") != tagger_email:
        return fail(f"native release tag {tag} tagger {tagger!r}, expected {tagger_name} <{tagger_email}>")
    if not isinstance(target, dict) or target.get("type") != "commit":
        return fail(f"native release tag {tag} target {target!r}, expected commit object")
    target_sha = str(target.get("sha", "")).lower()
    if target_sha != runtime_ref:
        return fail(f"native release tag {tag} targets {target_sha}, expected {runtime_ref}")
    print(f"native tag {tag} verified by Git Data API: Rui annotated tag -> {runtime_ref}")
    return 0


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mode", choices=("local", "github"), required=True)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--runtime-ref", required=True)
    parser.add_argument("--repo", help="GitHub owner/repo for --mode github")
    parser.add_argument("--git-repo", type=Path, default=Path.cwd(), help="local git repo path for --mode local")
    parser.add_argument("--tagger-name", default=EXPECTED_TAGGER_NAME)
    parser.add_argument("--tagger-email", default=EXPECTED_TAGGER_EMAIL)
    args = parser.parse_args(argv[1:])
    runtime_ref = args.runtime_ref.lower()
    if error := validate_inputs(args.tag, runtime_ref):
        return fail(error)
    if args.mode == "github":
        if not args.repo:
            return fail("--repo is required in github mode")
        return verify_github(args.repo, args.tag, runtime_ref, args.tagger_name, args.tagger_email)
    return verify_local(args.git_repo, args.tag, runtime_ref, args.tagger_name, args.tagger_email)


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
