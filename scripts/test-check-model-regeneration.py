#!/usr/bin/env python3
"""Negative self-tests for the generated source regeneration comparators."""
from __future__ import annotations

import pathlib
import shutil
import subprocess
import sys
import tempfile

ROOT = pathlib.Path(__file__).resolve().parents[1]


def fail(message: str) -> int:
    print(f"model regeneration self-test failed: {message}", file=sys.stderr)
    return 1


def copy_repo(dst: pathlib.Path) -> None:
    shutil.copytree(ROOT, dst, ignore=shutil.ignore_patterns(".git", "artifacts", "coverage.out"))


def replace_once(path: pathlib.Path, old: str, new: str) -> None:
    text = path.read_text(encoding="utf-8")
    if old not in text:
        raise RuntimeError(f"target text not found in {path}: {old!r}")
    path.write_text(text.replace(old, new, 1), encoding="utf-8")


def run_check(repo: pathlib.Path, name: str) -> None:
    proc = subprocess.run(["bash", "./scripts/check-model-regeneration.sh"], cwd=repo, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if proc.returncode == 0:
        raise RuntimeError(f"{name} corruption unexpectedly passed regeneration comparator")
    combined = proc.stdout + proc.stderr
    if "does not match normalized regeneration" not in combined:
        raise RuntimeError(f"{name} failed for the wrong reason:\n{combined[-4000:]}")


def main() -> int:
    with tempfile.TemporaryDirectory(prefix="go-ai-model-regen-negative-") as td:
        base = pathlib.Path(td)
        text_repo = base / "text"
        image_repo = base / "image"
        copy_repo(text_repo)
        copy_repo(image_repo)
        replace_once(text_repo / "models_generated.go", "Name:             \"GPT-6 Astra\"", "Name:             \"GPT-6 Astra Corrupt\"")
        run_check(text_repo, "text non-ID metadata")
        replace_once(image_repo / "images" / "models_generated.go", "Name:     \"Microsoft AI: MAI-Image-2.6\"", "Name:     \"Microsoft AI: MAI-Image-2.6 Corrupt\"")
        run_check(image_repo, "image non-ID metadata")
    print("model regeneration negative self-test passed")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:  # noqa: BLE001 - CLI diagnostic
        raise SystemExit(fail(str(exc)))
