#!/usr/bin/env python3
import json
import math
import sys
from pathlib import Path

from PIL import Image, ImageChops


def stats_for(canonical_path: Path, candidate_path: Path, overlay_path: Path, diff_path: Path) -> dict:
    canonical = Image.open(canonical_path).convert("RGB")
    candidate = Image.open(candidate_path).convert("RGB")
    if canonical.size != candidate.size:
        raise SystemExit(f"{candidate_path} size {candidate.size}, canonical size {canonical.size}")

    Image.blend(canonical, candidate, 0.5).save(overlay_path)
    diff = ImageChops.difference(canonical, candidate)
    diff.save(diff_path)

    pixels = canonical.size[0] * canonical.size[1]
    changed = 0
    abs_sum = 0
    sq_sum = 0
    max_delta = 0
    for r, g, b in diff.getdata():
        if r or g or b:
            changed += 1
        abs_sum += r + g + b
        sq_sum += (r * r) + (g * g) + (b * b)
        max_delta = max(max_delta, r, g, b)

    channels = pixels * 3
    return {
        "canonical": str(canonical_path),
        "candidate": str(candidate_path),
        "overlay": str(overlay_path),
        "pixel_diff": str(diff_path),
        "width": canonical.size[0],
        "height": canonical.size[1],
        "pixels": pixels,
        "changed_pixels": changed,
        "changed_ratio": changed / pixels,
        "mae_per_rgb_channel": abs_sum / channels,
        "rmse_per_rgb_channel": math.sqrt(sq_sum / channels),
        "max_abs_channel_delta": max_delta,
    }


def main() -> None:
    if len(sys.argv) < 4:
        raise SystemExit("usage: diff-canonical.py CANONICAL OUT_DIR STATE_NAME...")
    canonical_path = Path(sys.argv[1])
    out_dir = Path(sys.argv[2])
    all_stats = {}
    for name in sys.argv[3:]:
        viewport = "1280x941" if name.endswith("1280") else "1672x941"
        candidate = out_dir / f"{name}-{viewport}.png"
        overlay = out_dir / f"{name}-canonical-overlay.png"
        pixel_diff = out_dir / f"{name}-canonical-pixel-diff.png"
        state_stats = stats_for(canonical_path, candidate, overlay, pixel_diff)
        stats_path = out_dir / f"{name}-canonical-diff-stats.json"
        stats_path.write_text(json.dumps(state_stats, indent=2) + "\n", encoding="utf-8")
        all_stats[name] = state_stats
    (out_dir / "canonical-diff-stats-all.json").write_text(json.dumps(all_stats, indent=2) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
