#!/usr/bin/env python3
"""Preflight exactly one root verdict.json and validate hard-fail cases."""

from __future__ import annotations

import json
import sys
from datetime import datetime, timezone
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
REQUIRED_KEYS = {
    "task",
    "phase",
    "result",
    "product_verdict",
    "frozen_inputs",
    "checks",
    "raw_evidence",
    "fixture_manifest_sha256",
    "preflight",
}


def validate_run_root(run_root: Path) -> list[str]:
    errors: list[str] = []
    verdicts = list(run_root.rglob("verdict.json"))
    root_verdict = run_root / "verdict.json"
    if verdicts != [root_verdict]:
        errors.append(
            "expected exactly one verdict.json at run root; found "
            + ", ".join(str(path.relative_to(run_root)) for path in verdicts)
        )
        return errors
    try:
        verdict = json.loads(root_verdict.read_text())
    except Exception as exc:
        return [f"invalid verdict json: {exc}"]
    missing = REQUIRED_KEYS - set(verdict)
    extra = set(verdict) - REQUIRED_KEYS
    if missing:
        errors.append("missing verdict fields: " + ", ".join(sorted(missing)))
    if extra:
        errors.append("unexpected verdict fields: " + ", ".join(sorted(extra)))
    for raw_path in verdict.get("raw_evidence", []):
        path = run_root / raw_path
        if not path.exists():
            errors.append(f"dangling raw evidence path: {raw_path}")
    preflight = verdict.get("preflight", {})
    log_path = preflight.get("log")
    if log_path and not (run_root / log_path).exists():
        errors.append(f"dangling preflight log path: {log_path}")
    return errors


def exercise_hard_failures() -> list[dict]:
    scratch = ROOT / "raw" / "preflight-negative-cases"
    if scratch.exists():
        for child in sorted(scratch.rglob("*"), reverse=True):
            if child.is_file():
                child.unlink()
            elif child.is_dir():
                child.rmdir()
    scratch.mkdir(parents=True, exist_ok=True)
    cases = []
    (scratch / "multi").mkdir()
    (scratch / "multi" / "nested").mkdir()
    (scratch / "multi" / "verdict.json").write_text("{}\n")
    (scratch / "multi" / "nested" / "verdict.json").write_text("{}\n")
    cases.append({"name": "multiple_verdicts", "errors": validate_run_root(scratch / "multi")})
    (scratch / "bad_fields").mkdir()
    (scratch / "bad_fields" / "verdict.json").write_text('{"task":"T2182","extra":true}\n')
    cases.append({"name": "field_errors", "errors": validate_run_root(scratch / "bad_fields")})
    (scratch / "dangling").mkdir()
    dangling = {
        "task": "T2182",
        "phase": "phase1_method_gate",
        "result": "BLOCKED/NOT_RUN",
        "product_verdict": "NOT_RUN",
        "frozen_inputs": [
            "01M1QE0GKK0XA1C87J8TZVYP7K",
            "01M1QE1FAW8JKJ3QYPVYR7N207",
            "01M1QE1YRCPP669D73RKDCA2SQ"
        ],
        "checks": {},
        "raw_evidence": ["raw/missing.json"],
        "fixture_manifest_sha256": None,
        "preflight": {"checked_at": "now", "status": "NOT_RUN", "log": "logs/missing.log"}
    }
    (scratch / "dangling" / "verdict.json").write_text(json.dumps(dangling) + "\n")
    cases.append({"name": "dangling_paths", "errors": validate_run_root(scratch / "dangling")})
    return cases


def main() -> int:
    run_root = Path(sys.argv[1]).resolve() if len(sys.argv) > 1 else ROOT
    errors = validate_run_root(run_root)
    negative_cases = exercise_hard_failures()
    negative_ok = all(case["errors"] for case in negative_cases)
    result = {
        "task": "T2182",
        "phase": "phase1_method_gate",
        "check": "unique_root_verdict_preflight",
        "checked_at": datetime.now(timezone.utc).isoformat(),
        "run_root": str(run_root),
        "status": "PASS" if not errors and negative_ok else "FAIL",
        "errors": errors,
        "negative_cases": negative_cases,
        "negative_cases_hard_failed": negative_ok
    }
    out = ROOT / "raw" / "preflight-verdict.json"
    out.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if result["status"] == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
