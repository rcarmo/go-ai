#!/usr/bin/env python3
"""Validate committed v0.87.1 release inventory files and hashes."""
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
    "changed-paths.txt": (16, "2756fce613d0163b6eb5c47b599584589a229e5c7ed30a86b65380031e272eb6"),
    "changed-tests.txt": (9, "5b66a8cf9050b36a8dbae7a1b802c12037a2ec2332cf2e3f370953ea9ef9ac43"),
    "test-corpus-150.txt": (150, "042cdfbc8cc089da71409e615fb54cfe7273a960f8e0d10e63e07584ae9f2e75"),
}


def sha256(path: pathlib.Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def fail(message: str) -> int:
    print(f"v0.87.1 inventory validation failed: {message}", file=sys.stderr)
    return 1


def validate(root: pathlib.Path = ROOT) -> int:
    docs = root / "docs" / "v0871"
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
    changed_paths = [line for line in (docs / "changed-paths.txt").read_text(encoding="utf-8").splitlines() if line.strip()]
    if changed_paths != sorted(changed_paths) or not all(path.startswith("packages/ai/") for path in changed_paths):
        return fail("changed-paths.txt must be sorted packages/ai/... paths")
    changed_tests = [line for line in (docs / "changed-tests.txt").read_text(encoding="utf-8").splitlines() if line.strip()]
    if changed_tests != sorted(changed_tests) or not all(path.startswith("packages/ai/test/") and path.endswith(".test.ts") for path in changed_tests):
        return fail("changed-tests.txt must be sorted packages/ai/test/*.test.ts paths")
    corpus_rows = [line for line in (docs / "test-corpus-150.txt").read_text(encoding="utf-8").splitlines() if line.strip()]
    if len(set(corpus_rows)) != len(corpus_rows):
        return fail("test-corpus-150.txt contains duplicates")
    if corpus_rows != sorted(corpus_rows):
        return fail("test-corpus-150.txt must be sorted")
    if not all("/" not in path and path.endswith(".test.ts") for path in corpus_rows):
        return fail("test-corpus-150.txt must contain basenames only")
    print("v0.87.1 inventory validation passed")
    for name in FILES:
        path = docs / name
        print(f"{name}: {len([l for l in path.read_text(encoding='utf-8').splitlines() if l.strip()])} rows, sha256 {sha256(path)}")
    return 0


def self_test() -> int:
    with tempfile.TemporaryDirectory(prefix="go-ai-v0871-inventory-test-") as td:
        tmp = pathlib.Path(td) / "repo"
        shutil.copytree(ROOT, tmp, ignore=shutil.ignore_patterns(".git", "artifacts"))
        if validate(tmp) != 0:
            return fail("clean copied inventory did not validate before corruption")
        for relative, mutation in [
            ("changed-paths.txt", "packages/ai/extra.ts\n"),
            ("changed-tests.txt", "packages/ai/test/extra.test.ts\n"),
            ("test-corpus-150.txt", "extra.test.ts\n"),
        ]:
            copy = pathlib.Path(td) / f"repo-{relative}"
            shutil.copytree(tmp, copy)
            target = copy / "docs" / "v0871" / relative
            target.write_text(target.read_text(encoding="utf-8") + mutation, encoding="utf-8")
            proc = subprocess.run([sys.executable, str(copy / "scripts" / "validate-v0871-inventory.py")], cwd=copy, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            if proc.returncode == 0:
                return fail(f"corrupted {relative} inventory unexpectedly validated")
    print("v0.87.1 inventory negative self-test passed")
    return 0


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args(argv)
    return self_test() if args.self_test else validate(ROOT)


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
