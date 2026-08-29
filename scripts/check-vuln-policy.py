#!/usr/bin/env python3
"""Run govulncheck JSON output through an explicit, expiring finding policy."""
from __future__ import annotations
import datetime as dt
import json
import pathlib
import subprocess
import sys
from typing import Any


def fail(msg: str) -> int:
    print(f"vulnerability policy check failed: {msg}", file=sys.stderr)
    return 1


def load_policy(path: pathlib.Path) -> dict[str, dict[str, Any]]:
    data = json.loads(path.read_text(encoding="utf-8"))
    today = dt.date.today()
    allowed: dict[str, dict[str, Any]] = {}
    for entry in data.get("allowedFindings", []):
        vuln_id = entry.get("id")
        owner = entry.get("owner")
        rationale = entry.get("rationale")
        expires = entry.get("expires")
        if not all(isinstance(v, str) and v.strip() for v in (vuln_id, owner, rationale, expires)):
            raise ValueError(f"invalid policy entry: {entry!r}")
        expiry = dt.date.fromisoformat(expires)
        if expiry < today:
            raise ValueError(f"policy entry {vuln_id} expired on {expires}")
        allowed[vuln_id] = entry
    return allowed


def main(argv: list[str]) -> int:
    if len(argv) < 3:
        print("usage: scripts/check-vuln-policy.py security-vuln-policy.json GOVULNCHECK...", file=sys.stderr)
        return 2
    policy_path = pathlib.Path(argv[1])
    try:
        allowed = load_policy(policy_path)
    except Exception as exc:  # noqa: BLE001 - command-line validator reports concise failure.
        return fail(str(exc))

    cmd = argv[2:]
    proc = subprocess.run(cmd, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)
    if proc.stderr:
        print(proc.stderr, file=sys.stderr, end="")

    output = "\n".join(line for line in proc.stdout.splitlines() if not line.strip().startswith("go: "))
    decoder = json.JSONDecoder()
    idx = 0
    events: list[dict[str, Any]] = []
    while idx < len(output):
        while idx < len(output) and output[idx].isspace():
            idx += 1
        if idx >= len(output):
            break
        if output[idx] != "{":
            return fail(f"unexpected non-JSON govulncheck output: {output[idx:idx+120]}")
        try:
            event, idx = decoder.raw_decode(output, idx)
        except json.JSONDecodeError as exc:
            return fail(f"invalid govulncheck JSON output: {exc}")
        if isinstance(event, dict):
            events.append(event)

    vulns: dict[str, dict[str, Any]] = {}
    findings: dict[str, int] = {}
    for event in events:
        if "osv" in event:
            osv = event["osv"]
            if isinstance(osv, dict) and isinstance(osv.get("id"), str):
                vulns[osv["id"]] = osv
        if "finding" in event:
            finding = event["finding"]
            if isinstance(finding, dict) and isinstance(finding.get("osv"), str):
                findings[finding["osv"]] = findings.get(finding["osv"], 0) + 1

    if proc.returncode not in (0, 3):
        return fail(f"govulncheck exited {proc.returncode}")

    if not findings:
        print("govulncheck: no reachable vulnerabilities")
        return 0

    unknown = sorted(set(findings) - set(allowed))
    if unknown:
        return fail("undocumented reachable findings: " + ", ".join(unknown))

    for vuln_id in sorted(findings):
        osv = vulns.get(vuln_id, {})
        affected = osv.get("affected") if isinstance(osv, dict) else None
        fixed = []
        if isinstance(affected, list):
            for item in affected:
                if not isinstance(item, dict):
                    continue
                ranges = item.get("ranges")
                if not isinstance(ranges, list):
                    continue
                for r in ranges:
                    if not isinstance(r, dict):
                        continue
                    for event in r.get("events", []):
                        if isinstance(event, dict) and isinstance(event.get("fixed"), str):
                            fixed.append(event["fixed"])
        print(f"govulncheck: {vuln_id} allowed by policy until {allowed[vuln_id]['expires']} (findings={findings[vuln_id]}, fixed={','.join(sorted(set(fixed))) or 'unknown'})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
