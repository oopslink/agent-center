#!/usr/bin/env python3
import json
import math
import sys
from pathlib import Path

from PIL import Image, ImageChops


def main() -> int:
    if len(sys.argv) != 5:
        print("usage: image_diff.py <canonical> <candidate> <overlay> <pixel_diff>", file=sys.stderr)
        return 2

    canonical_path = Path(sys.argv[1])
    candidate_path = Path(sys.argv[2])
    overlay_path = Path(sys.argv[3])
    diff_path = Path(sys.argv[4])

    canonical = Image.open(canonical_path).convert("RGBA")
    candidate = Image.open(candidate_path).convert("RGBA")
    if canonical.size != candidate.size:
        raise SystemExit(f"size mismatch: canonical={canonical.size} candidate={candidate.size}")

    overlay = Image.blend(canonical, candidate, 0.5)
    overlay.save(overlay_path)

    diff = ImageChops.difference(canonical, candidate)
    boosted = diff.point(lambda value: min(255, value * 4))
    boosted.save(diff_path)

    width, height = canonical.size
    pixels = width * height
    changed = 0
    abs_sum = 0
    sq_sum = 0
    max_delta = 0
    for c_px, d_px in zip(canonical.getdata(), candidate.getdata()):
        channel_deltas = [abs(c_px[i] - d_px[i]) for i in range(3)]
        if any(channel_deltas):
            changed += 1
        abs_sum += sum(channel_deltas)
        sq_sum += sum(delta * delta for delta in channel_deltas)
        max_delta = max(max_delta, *channel_deltas)

    channels = pixels * 3
    print(json.dumps({
        "canonical": str(canonical_path),
        "candidate": str(candidate_path),
        "width": width,
        "height": height,
        "pixels": pixels,
        "changed_pixels": changed,
        "changed_ratio": changed / pixels if pixels else 0,
        "mae_per_channel": abs_sum / channels if channels else 0,
        "rmse_per_channel": math.sqrt(sq_sum / channels) if channels else 0,
        "max_abs_channel_delta": max_delta,
        "overlay": str(overlay_path),
        "pixel_diff": str(diff_path),
    }, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
