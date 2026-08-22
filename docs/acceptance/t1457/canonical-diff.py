#!/usr/bin/env python3
import argparse
import json
from pathlib import Path

from PIL import Image, ImageChops, ImageStat


def main() -> None:
    parser = argparse.ArgumentParser(description="Generate same-size canonical overlay/diff stats.")
    parser.add_argument("--canonical", required=True)
    parser.add_argument("--candidate", required=True)
    parser.add_argument("--overlay", required=True)
    parser.add_argument("--diff", required=True)
    parser.add_argument("--stats", required=True)
    args = parser.parse_args()

    canonical_path = Path(args.canonical)
    candidate_path = Path(args.candidate)
    overlay_path = Path(args.overlay)
    diff_path = Path(args.diff)
    stats_path = Path(args.stats)

    canonical = Image.open(canonical_path).convert("RGB")
    candidate = Image.open(candidate_path).convert("RGB")
    if canonical.size != candidate.size:
        raise SystemExit(f"size mismatch canonical={canonical.size} candidate={candidate.size}")

    diff = ImageChops.difference(canonical, candidate)
    overlay = Image.blend(canonical, candidate, 0.5)
    diff.save(diff_path)
    overlay.save(overlay_path)

    width, height = canonical.size
    pixels = width * height
    changed_pixels = sum(1 for pixel in diff.getdata() if pixel != (0, 0, 0))
    stat = ImageStat.Stat(diff)
    means = stat.mean
    rms = stat.rms
    extrema = stat.extrema
    stats = {
        "canonical": str(canonical_path),
        "candidate": str(candidate_path),
        "width": width,
        "height": height,
        "pixels": pixels,
        "changed_pixels": changed_pixels,
        "changed_ratio": changed_pixels / pixels,
        "mae_per_channel": sum(means) / 3,
        "rmse_per_channel": sum(rms) / 3,
        "max_abs_channel_delta": max(high for _, high in extrema),
        "overlay": str(overlay_path),
        "pixel_diff": str(diff_path),
    }
    stats_path.write_text(json.dumps(stats, indent=2) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
