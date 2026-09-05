#!/usr/bin/env python3
"""Self-test the performance timeline collector schema boundary."""

from __future__ import annotations

import json
from datetime import datetime, timezone
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
RAW = ROOT / "raw"


def validate(timeline: dict) -> list[str]:
    errors: list[str] = []
    names = [event.get("name") for event in timeline.get("events", [])]
    ts = [event.get("timestamp") for event in timeline.get("events", [])]
    required = [
        "t0",
        "request",
        "response",
        "graph_dom_ready",
        "requestAnimationFrame_1",
        "requestAnimationFrame_2",
        "paint_settled",
    ]
    for name in required:
        if name not in names:
            errors.append(f"missing event {name}")
    if names and names[0] != "t0":
        errors.append("t0 must be first and captured before navigation")
    if any(not isinstance(value, (int, float)) for value in ts):
        errors.append("all timestamps must be numeric")
    if ts != sorted(ts):
        errors.append("timestamps must be monotonic")
    artifacts = timeline.get("artifacts", {})
    for name in ("trace", "har", "video"):
        if not artifacts.get(name):
            errors.append(f"missing artifact {name}")
    interaction_types = {item.get("type") for item in timeline.get("interactions", [])}
    if not {"wheel", "drag"}.issubset(interaction_types):
        errors.append("wheel and drag interactions are both required")
    for item in timeline.get("interactions", []):
        if not isinstance(item.get("timestamp"), (int, float)):
            errors.append(f"{item.get('type')} missing numeric timestamp")
        view_box = item.get("viewBox")
        if not isinstance(view_box, list) or len(view_box) != 4:
            errors.append(f"{item.get('type')} missing four-number viewBox")
    return errors


def main() -> int:
    RAW.mkdir(parents=True, exist_ok=True)
    timeline = {
        "t0_before_navigation": 1000.0,
        "events": [
            {"name": "t0", "timestamp": 1000.0},
            {"name": "request", "timestamp": 1001.0},
            {"name": "response", "timestamp": 1010.0},
            {"name": "graph_dom_ready", "timestamp": 1020.0},
            {"name": "requestAnimationFrame_1", "timestamp": 1036.0},
            {"name": "requestAnimationFrame_2", "timestamp": 1052.0},
            {"name": "paint_settled", "timestamp": 1053.0},
        ],
        "artifacts": {
            "trace": "raw/perf-trace.zip",
            "har": "raw/perf.har",
            "video": "raw/perf.webm"
        },
        "interactions": [
            {"type": "wheel", "timestamp": 1060.0, "viewBox": [0, 0, 800, 600]},
            {"type": "drag", "timestamp": 1080.0, "viewBox": [10, 12, 800, 600]}
        ]
    }
    errors = validate(timeline)
    result = {
        "task": "T2182",
        "phase": "phase1_method_gate",
        "check": "playwright_performance_collector_boundary",
        "status": "PASS" if not errors else "FAIL",
        "checked_at": datetime.now(timezone.utc).isoformat(),
        "timeline": timeline,
        "errors": errors
    }
    out = RAW / "perf-timeline-selftest.json"
    out.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if not errors else 1


if __name__ == "__main__":
    raise SystemExit(main())
