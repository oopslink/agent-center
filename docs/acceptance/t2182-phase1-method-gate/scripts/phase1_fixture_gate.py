#!/usr/bin/env python3
"""Fail-closed T2182 fixture gate for isolated executors.

This script intentionally does not use databases, admin sockets, worker tokens,
runtime MCP config, raw HTTP, or any other agent-center fallback. It records why
the live fixture cannot be created in this executor.
"""

from __future__ import annotations

import hashlib
import json
import os
from datetime import datetime, timezone
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
RAW = ROOT / "raw"
BLOCKED = RAW / "fixture-gate-blocked.json"
MANIFEST = RAW / "fixture-manifest.json"


def main() -> int:
    RAW.mkdir(parents=True, exist_ok=True)
    payload = {
        "task": "T2182",
        "phase": "phase1_method_gate",
        "check": "fresh_fixture_via_official_api",
        "status": "BLOCKED",
        "checked_at": datetime.now(timezone.utc).isoformat(),
        "reason": (
            "Executor instructions explicitly forbid agent-center access and "
            "fallbacks; no official MCP/API credentials or tools are available "
            "inside this isolated workspace."
        ),
        "forbidden_fallbacks_not_used": [
            "sqlite",
            "agent-center database files",
            "admin sockets",
            "admin HTTP endpoints",
            "worker tokens",
            "mcp_config.runtime.json",
            "process arguments",
            "raw HTTP"
        ],
        "required_but_not_created": {
            "agent_A": None,
            "agent_B": None,
            "task_initial_owner": None,
            "task_reassigned_owner": None,
            "event_effect_readback": None
        },
        "fixture_manifest": {
            "path": str(MANIFEST.relative_to(ROOT)),
            "exists": MANIFEST.exists(),
            "sha256": (
                hashlib.sha256(MANIFEST.read_bytes()).hexdigest()
                if MANIFEST.exists()
                else None
            )
        },
        "environment": {
            "cwd": os.getcwd(),
            "agent_center_access": "not_available_by_contract"
        }
    }
    BLOCKED.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n")
    print(json.dumps(payload, indent=2, sort_keys=True))
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
