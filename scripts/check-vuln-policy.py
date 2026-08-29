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


def load_policy(path: pathlib.Path) -> list[dict[str, Any]]:
    data = json.loads(path.read_text(encoding="utf-8"))
    today = dt.date.today()
    entries: list[dict[str, Any]] = []
    for entry in data.get("allowedFindings", []):
        vuln_id = entry.get("id")
        owner = entry.get("owner")
        rationale = entry.get("rationale")
        expires = entry.get("expires")
        go_version = entry.get("goVersion")
        scope = entry.get("scope")
        mitigation = entry.get("mitigation")
        if not all(isinstance(v, str) and v.strip() for v in (vuln_id, owner, rationale, mitigation, expires, go_version, scope)):
            raise ValueError(f"invalid policy entry: {entry!r}")
        expiry = dt.date.fromisoformat(expires)
        if expiry < today:
            raise ValueError(f"policy entry {vuln_id} expired on {expires}")
        entries.append(entry)
    return entries


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

    go_version = ""
    vulns: dict[str, dict[str, Any]] = {}
    findings: dict[str, int] = {}
    for event in events:
        if "config" in event and isinstance(event["config"], dict):
            raw_go_version = event["config"].get("go_version")
            if isinstance(raw_go_version, str):
                go_version = raw_go_version
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

    if not go_version:
        return fail("govulncheck JSON did not report go_version")

    active_allowed = {entry["id"]: entry for entry in allowed if entry.get("goVersion") == go_version}
    inactive_matches = sorted(vuln_id for vuln_id in findings if vuln_id in {entry["id"] for entry in allowed} and vuln_id not in active_allowed)
    if inactive_matches:
        return fail("findings documented only for a different Go version: " + ", ".join(inactive_matches))

    if not findings:
        if active_allowed:
            return fail("unused active vulnerability policy entries for " + go_version + ": " + ", ".join(sorted(active_allowed)))
        print(f"govulncheck: no reachable vulnerabilities for {go_version}")
        return 0

    unknown = sorted(set(findings) - set(active_allowed))
    if unknown:
        return fail("undocumented reachable findings for " + go_version + ": " + ", ".join(unknown))
    unused = sorted(set(active_allowed) - set(findings))
    if unused:
        return fail("unused active vulnerability policy entries for " + go_version + ": " + ", ".join(unused))

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
        print(f"govulncheck: {vuln_id} allowed for {go_version} by policy until {active_allowed[vuln_id]['expires']} (findings={findings[vuln_id]}, fixed={','.join(sorted(set(fixed))) or 'unknown'}, mitigation={active_allowed[vuln_id]['mitigation']})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
