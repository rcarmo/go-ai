#!/usr/bin/env python3
"""Negative self-tests for scripts/check-vuln-policy.py."""
from __future__ import annotations
import json
import pathlib
import subprocess
import sys
import tempfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
VALIDATOR = ROOT / "scripts" / "check-vuln-policy.py"


def write_script(path: pathlib.Path, events: list[dict], rc: int = 0) -> None:
    body = "import json, sys\n"
    body += f"events = {events!r}\n"
    body += "for event in events:\n    print(json.dumps(event, indent=2))\n"
    body += f"sys.exit({rc})\n"
    path.write_text("#!/usr/bin/env python3\n" + body, encoding="utf-8")
    path.chmod(0o755)


def write_policy(path: pathlib.Path, entries: list[dict]) -> None:
    path.write_text(json.dumps({"schema": 1, "allowedFindings": entries}, indent=2) + "\n", encoding="utf-8")


def run(policy: pathlib.Path, scanner: pathlib.Path) -> subprocess.CompletedProcess[str]:
    return subprocess.run([sys.executable, str(VALIDATOR), str(policy), str(scanner)], text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)


def base_events(go_version: str = "go1.26.3", vuln: str = "GO-TEST-0001") -> list[dict]:
    return [
        {"config": {"go_version": go_version}},
        {"osv": {"id": vuln, "affected": [{"ranges": [{"events": [{"fixed": "1.26.6"}]}]}]}},
        {"finding": {"osv": vuln}},
    ]


def policy_entry(vuln: str = "GO-TEST-0001", go_version: str = "go1.26.3") -> dict:
    return {
        "id": vuln,
        "owner": "Rui Carmo",
        "rationale": "test exception",
        "mitigation": "upgrade toolchain and remove exception",
        "expires": "2099-01-01",
        "goVersion": go_version,
        "scope": "standard-library toolchain",
    }


def without_mitigation(empty: bool = False) -> dict:
    entry = policy_entry()
    if empty:
        entry["mitigation"] = ""
    else:
        del entry["mitigation"]
    return entry


def expect(name: str, should_pass: bool, events: list[dict], entries: list[dict]) -> None:
    with tempfile.TemporaryDirectory() as td:
        base = pathlib.Path(td)
        scanner = base / "scanner.py"
        policy = base / "policy.json"
        write_script(scanner, events)
        write_policy(policy, entries)
        result = run(policy, scanner)
        if (result.returncode == 0) != should_pass:
            raise AssertionError(f"{name}: rc={result.returncode}\nstdout={result.stdout}\nstderr={result.stderr}")
        print(f"vuln-policy self-test passed: {name}")


def main() -> int:
    expect("matching finding allowed", True, base_events(), [policy_entry()])
    expect("different Go version rejected", False, base_events("go1.24.13"), [policy_entry(go_version="go1.26.3")])
    expect("undocumented finding rejected", False, base_events(vuln="GO-OTHER"), [policy_entry()])
    expect("unused active exception rejected", False, [{"config": {"go_version": "go1.26.3"}}], [policy_entry()])
    expect("missing mitigation rejected", False, base_events(), [without_mitigation()])
    expect("empty mitigation rejected", False, base_events(), [without_mitigation(empty=True)])
    expect("no findings no active exception passes", True, [{"config": {"go_version": "go1.24.13"}}], [policy_entry(go_version="go1.26.3")])
    print("vulnerability policy self-tests passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
