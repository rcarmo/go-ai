#!/usr/bin/env python3
"""Validate committed v0.85.1 release inventory files and hashes."""
from __future__ import annotations

import argparse
import hashlib
import pathlib
import shutil
import subprocess
import sys
import tempfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
FILES = {
    "changed-paths.txt": (9, "ee26f669d92dc77b265731165a2ff69ccb67defba92517cbbd5f97a186e187d2"),
    "changed-tests.txt": (3, "f7e274bf229c90fc22ba22384c5b89f71a5c6801f77067d099525a9cdc537610"),
    "test-corpus-142.txt": (142, "56f8742065a4ad01d73e5aee53035324f2e7333a735222ab15db870819e29065"),
}


def sha256(path: pathlib.Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def fail(message: str) -> int:
    print(f"v0.85.1 inventory validation failed: {message}", file=sys.stderr)
    return 1


def validate(root: pathlib.Path = ROOT) -> int:
    docs = root / "docs" / "v0851"
    for name, (want_count, want_hash) in FILES.items():
        path = docs / name
        if not path.is_file():
            return fail(f"missing {path}")
        rows = [line for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]
        got_hash = sha256(path)
        if len(rows) != want_count:
            return fail(f"{name} row count {len(rows)}, want {want_count}")
        if got_hash != want_hash:
            return fail(f"{name} sha256 {got_hash}, want {want_hash}")
    corpus = [line.replace("packages/ai/", "") for line in (docs / "test-corpus-142.txt").read_text(encoding="utf-8").splitlines() if line.strip()]
    if len(set(corpus)) != len(corpus):
        return fail("test-corpus-142.txt contains duplicates")
    if not all(path.startswith("test/") and path.endswith(".test.ts") for path in corpus):
        return fail("test-corpus-142.txt contains non-test paths")
    print("v0.85.1 inventory validation passed")
    for name in FILES:
        path = docs / name
        print(f"{name}: {len([l for l in path.read_text(encoding='utf-8').splitlines() if l.strip()])} rows, sha256 {sha256(path)}")
    return 0


def self_test() -> int:
    with tempfile.TemporaryDirectory(prefix="go-ai-v0851-inventory-test-") as td:
        tmp = pathlib.Path(td) / "repo"
        shutil.copytree(ROOT, tmp, ignore=shutil.ignore_patterns(".git", "artifacts"))
        if validate(tmp) != 0:
            return fail("clean copied inventory did not validate before corruption")
        target = tmp / "docs" / "v0851" / "changed-paths.txt"
        target.write_text(target.read_text(encoding="utf-8") + "\npackages/ai/extra.ts\n", encoding="utf-8")
        proc = subprocess.run([sys.executable, str(tmp / "scripts" / "validate-v0851-inventory.py")], cwd=tmp, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if proc.returncode == 0:
            return fail("corrupted changed-paths inventory unexpectedly validated")
        target = tmp / "docs" / "v0851" / "test-corpus-142.txt"
        data = target.read_text(encoding="utf-8")
        target.write_text(data.replace("packages/ai/test/abort.test.ts", "packages/ai/test/abort-corrupt.test.ts", 1), encoding="utf-8")
        proc = subprocess.run([sys.executable, str(tmp / "scripts" / "validate-v0851-inventory.py")], cwd=tmp, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if proc.returncode == 0:
            return fail("corrupted test corpus unexpectedly validated")
    print("v0.85.1 inventory negative self-test passed")
    return 0


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args(argv)
    return self_test() if args.self_test else validate(ROOT)


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
