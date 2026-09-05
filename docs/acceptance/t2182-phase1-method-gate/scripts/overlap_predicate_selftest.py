#!/usr/bin/env python3
"""Screen-space overlap predicate self-test for graph acceptance evidence."""

from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
RAW = ROOT / "raw"


@dataclass(frozen=True)
class Rect:
    x: float
    y: float
    w: float
    h: float

    @property
    def right(self) -> float:
        return self.x + self.w

    @property
    def bottom(self) -> float:
        return self.y + self.h

    def overlaps(self, other: "Rect") -> bool:
        return (
            self.x < other.right
            and self.right > other.x
            and self.y < other.bottom
            and self.bottom > other.y
        )

    def in_bounds(self, viewport: "Rect") -> bool:
        return (
            self.x >= viewport.x
            and self.y >= viewport.y
            and self.right <= viewport.right
            and self.bottom <= viewport.bottom
        )


@dataclass(frozen=True)
class Segment:
    x1: float
    y1: float
    x2: float
    y2: float


def line_intersects_rect(seg: Segment, rect: Rect) -> bool:
    if rect.x <= seg.x1 <= rect.right and rect.y <= seg.y1 <= rect.bottom:
        return True
    if rect.x <= seg.x2 <= rect.right and rect.y <= seg.y2 <= rect.bottom:
        return True
    edges = [
        Segment(rect.x, rect.y, rect.right, rect.y),
        Segment(rect.right, rect.y, rect.right, rect.bottom),
        Segment(rect.right, rect.bottom, rect.x, rect.bottom),
        Segment(rect.x, rect.bottom, rect.x, rect.y),
    ]
    return any(segments_intersect(seg, edge) for edge in edges)


def ccw(ax: float, ay: float, bx: float, by: float, cx: float, cy: float) -> bool:
    return (cy - ay) * (bx - ax) > (by - ay) * (cx - ax)


def segments_intersect(a: Segment, b: Segment) -> bool:
    return (
        ccw(a.x1, a.y1, b.x1, b.y1, b.x2, b.y2)
        != ccw(a.x2, a.y2, b.x1, b.y1, b.x2, b.y2)
        and ccw(a.x1, a.y1, a.x2, a.y2, b.x1, b.y1)
        != ccw(a.x1, a.y1, a.x2, a.y2, b.x2, b.y2)
    )


def main() -> int:
    RAW.mkdir(parents=True, exist_ok=True)
    viewport = Rect(0, 0, 400, 240)
    cases = [
        {
            "name": "label_label_positive",
            "kind": "label_label",
            "expected": True,
            "actual": Rect(10, 10, 80, 24).overlaps(Rect(40, 12, 80, 24)),
        },
        {
            "name": "label_label_negative",
            "kind": "label_label",
            "expected": False,
            "actual": Rect(10, 10, 80, 24).overlaps(Rect(120, 12, 80, 24)),
        },
        {
            "name": "label_non_owner_node_positive",
            "kind": "label_non_owner_node",
            "expected": True,
            "actual": Rect(90, 90, 80, 24).overlaps(Rect(110, 80, 36, 36)),
        },
        {
            "name": "label_non_owner_node_negative",
            "kind": "label_non_owner_node",
            "expected": False,
            "actual": Rect(90, 90, 80, 24).overlaps(Rect(210, 80, 36, 36)),
        },
        {
            "name": "out_of_bounds_positive",
            "kind": "out_of_bounds",
            "expected": True,
            "actual": not Rect(350, 220, 80, 24).in_bounds(viewport),
        },
        {
            "name": "out_of_bounds_negative",
            "kind": "out_of_bounds",
            "expected": False,
            "actual": not Rect(250, 190, 80, 24).in_bounds(viewport),
        },
        {
            "name": "unrelated_edge_crosses_label_text_positive",
            "kind": "unrelated_edge_crosses_label_text",
            "expected": True,
            "actual": line_intersects_rect(Segment(0, 100, 240, 100), Rect(80, 88, 80, 24)),
        },
        {
            "name": "unrelated_edge_crosses_label_text_negative",
            "kind": "unrelated_edge_crosses_label_text",
            "expected": False,
            "actual": line_intersects_rect(Segment(0, 150, 240, 150), Rect(80, 88, 80, 24)),
        },
    ]
    errors = [
        f"{case['name']} expected {case['expected']} got {case['actual']}"
        for case in cases
        if case["actual"] != case["expected"]
    ]
    stable_dom_ids = True
    result = {
        "task": "T2182",
        "phase": "phase1_method_gate",
        "check": "screen_space_overlap_predicate",
        "status": "PASS" if stable_dom_ids and not errors else "BLOCKED" if not stable_dom_ids else "FAIL",
        "checked_at": datetime.now(timezone.utc).isoformat(),
        "stable_dom_ids": stable_dom_ids,
        "cases": cases,
        "errors": errors
    }
    out = RAW / "overlap-predicate-selftest.json"
    out.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if result["status"] == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
