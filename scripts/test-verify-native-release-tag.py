#!/usr/bin/env python3
"""Policy simulations for scripts/verify-native-release-tag.py."""
from __future__ import annotations

import pathlib
import subprocess
import sys
import tempfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
VERIFY = ROOT / "scripts" / "verify-native-release-tag.py"
RUI_NAME = "Rui Carmo"
RUI_EMAIL = "rui.carmo@gmail.com"
OTHER_NAME = "Not Rui"
OTHER_EMAIL = "not-rui@example.invalid"


def run(cmd: list[str], cwd: pathlib.Path, *, check: bool = True) -> subprocess.CompletedProcess[str]:
    return subprocess.run(cmd, cwd=cwd, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=check)


def git(repo: pathlib.Path, *args: str) -> str:
    return run(["git", *args], repo).stdout.strip()


def init_repo(repo: pathlib.Path) -> tuple[str, str]:
    git(repo, "init", "-q")
    git(repo, "config", "user.name", RUI_NAME)
    git(repo, "config", "user.email", RUI_EMAIL)
    (repo / "file.txt").write_text("one\n", encoding="utf-8")
    git(repo, "add", "file.txt")
    git(repo, "commit", "-q", "-m", "one")
    first = git(repo, "rev-parse", "HEAD")
    (repo / "file.txt").write_text("two\n", encoding="utf-8")
    git(repo, "commit", "-qam", "two")
    second = git(repo, "rev-parse", "HEAD")
    return first, second


def verify(repo: pathlib.Path, tag: str, runtime: str) -> subprocess.CompletedProcess[str]:
    return run([sys.executable, str(VERIFY), "--mode", "local", "--git-repo", str(repo), "--tag", tag, "--runtime-ref", runtime], repo, check=False)


def expect_pass(name: str, result: subprocess.CompletedProcess[str]) -> None:
    if result.returncode != 0:
        raise AssertionError(f"{name} failed unexpectedly\nstdout={result.stdout}\nstderr={result.stderr}")
    print(f"native tag verifier positive self-test passed: {name}")


def expect_fail(name: str, result: subprocess.CompletedProcess[str], needle: str) -> None:
    if result.returncode == 0:
        raise AssertionError(f"{name} unexpectedly passed\nstdout={result.stdout}")
    combined = result.stdout + result.stderr
    if needle not in combined:
        raise AssertionError(f"{name} failed for wrong reason, missing {needle!r}\n{combined}")
    print(f"native tag verifier negative self-test passed: {name}")


def main() -> int:
    with tempfile.TemporaryDirectory(prefix="go-ai-native-tag-policy-") as td:
        repo = pathlib.Path(td)
        first, second = init_repo(repo)

        expect_fail("absent tag", verify(repo, "v0.99.2", first), "absent")

        git(repo, "tag", "v0.99.2-light", first)
        expect_fail("lightweight tag", verify(repo, "v0.99.2-light", first), "not annotated")

        env = {"GIT_COMMITTER_NAME": OTHER_NAME, "GIT_COMMITTER_EMAIL": OTHER_EMAIL}
        subprocess.run(["git", "-c", f"user.name={OTHER_NAME}", "-c", f"user.email={OTHER_EMAIL}", "tag", "-a", "v0.99.2-wrong-tagger", first, "-m", "wrong tagger"], cwd=repo, env={**env}, text=True, check=True)
        expect_fail("wrong tagger", verify(repo, "v0.99.2-wrong-tagger", first), "tagger")

        git(repo, "tag", "-a", "v0.99.2-wrong-target", second, "-m", "wrong target")
        expect_fail("wrong target", verify(repo, "v0.99.2-wrong-target", first), "targets")

        git(repo, "tag", "-a", "v0.99.2", first, "-m", "go-ai v0.99.2")
        expect_pass("Rui annotated runtime tag", verify(repo, "v0.99.2", first))

    print("native tag verifier self-tests passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
