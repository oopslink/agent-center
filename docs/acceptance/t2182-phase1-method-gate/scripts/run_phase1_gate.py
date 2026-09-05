#!/usr/bin/env python3
"""Run the T2182 phase-one gate until the first blocking condition."""

from __future__ import annotations

import json
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
RAW = ROOT / "raw"
LOGS = ROOT / "logs"


COMMANDS = [
    {
        "name": "phase1-fixture-gate",
        "cmd": [sys.executable, "scripts/phase1_fixture_gate.py"],
        "log": "logs/phase1-fixture-gate.log",
        "blocking": True,
    },
    {
        "name": "perf-timeline-selftest",
        "cmd": [sys.executable, "scripts/perf_timeline_selftest.py"],
        "log": "logs/perf-timeline-selftest.log",
        "blocking": True,
    },
    {
        "name": "overlap-predicate-selftest",
        "cmd": [sys.executable, "scripts/overlap_predicate_selftest.py"],
        "log": "logs/overlap-predicate-selftest.log",
        "blocking": True,
    },
]


NOT_RUN_OUTPUTS = {
    "perf-timeline-selftest": "raw/perf-timeline-selftest.json",
    "overlap-predicate-selftest": "raw/overlap-predicate-selftest.json",
    "preflight-verdict": "raw/preflight-verdict.json",
}


def write_not_run(name: str, reason: str) -> None:
    raw_path = ROOT / NOT_RUN_OUTPUTS[name]
    payload = {
        "task": "T2182",
        "phase": "phase1_method_gate",
        "check": name,
        "status": "NOT_RUN",
        "checked_at": datetime.now(timezone.utc).isoformat(),
        "reason": reason,
    }
    raw_path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n")
    log_name = name.replace("_", "-")
    log_path = LOGS / f"{log_name}.log"
    log_path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n")


def main() -> int:
    RAW.mkdir(parents=True, exist_ok=True)
    LOGS.mkdir(parents=True, exist_ok=True)
    exit_codes = []
    blocked_reason = None
    for entry in COMMANDS:
        log_path = ROOT / entry["log"]
        with log_path.open("w") as log:
            proc = subprocess.run(
                entry["cmd"],
                cwd=ROOT,
                text=True,
                stdout=log,
                stderr=subprocess.STDOUT,
                check=False,
            )
        exit_codes.append(
            {
                "name": entry["name"],
                "command": " ".join(entry["cmd"]),
                "exit_code": proc.returncode,
                "log": entry["log"],
            }
        )
        if proc.returncode != 0 and entry["blocking"]:
            blocked_reason = (
                f"{entry['name']} exited {proc.returncode}; phase-one contract "
                "requires stopping at the first failed or blocked self-check."
            )
            break
    if blocked_reason:
        for name in NOT_RUN_OUTPUTS:
            if not (ROOT / NOT_RUN_OUTPUTS[name]).exists():
                write_not_run(name, blocked_reason)
        for entry in COMMANDS[len(exit_codes):]:
            exit_codes.append(
                {
                    "name": entry["name"],
                    "command": " ".join(entry["cmd"]),
                    "exit_code": None,
                    "log": entry["log"],
                    "status": "NOT_RUN",
                    "reason": blocked_reason,
                }
            )
        exit_codes.append(
            {
                "name": "preflight-verdict",
                "command": f"{sys.executable} scripts/preflight_verdict.py .",
                "exit_code": None,
                "log": "logs/preflight-verdict.log",
                "status": "NOT_RUN",
                "reason": blocked_reason,
            }
        )
    result = {
        "task": "T2182",
        "phase": "phase1_method_gate",
        "result": "BLOCKED/NOT_RUN" if blocked_reason else "METHOD_GATE_PASS",
        "checked_at": datetime.now(timezone.utc).isoformat(),
        "commands": exit_codes,
    }
    (RAW / "command-exit-codes.json").write_text(
        json.dumps(result, indent=2, sort_keys=True) + "\n"
    )
    print(json.dumps(result, indent=2, sort_keys=True))
    return 2 if blocked_reason else 0


if __name__ == "__main__":
    raise SystemExit(main())
