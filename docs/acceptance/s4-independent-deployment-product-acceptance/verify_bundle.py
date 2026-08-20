#!/usr/bin/env python3
"""Verify every immutable payload entry in MANIFEST.json.

Requires Pillow because acceptance requires a full PNG decode, not merely a
signature/header check. The script fails closed on missing/extra payload files,
digest/size/type/dimension mismatches, invalid JSON, non-UTF-8 text, or a PNG
that Pillow cannot verify and fully load.
"""

from __future__ import annotations

import hashlib
import json
import sys
from pathlib import Path

try:
    from PIL import Image
except ImportError as exc:
    raise SystemExit("FAIL: Pillow is required for full PNG decode validation") from exc


ROOT = Path(__file__).resolve().parent
MANIFEST = ROOT / "MANIFEST.json"
MANIFEST_EXCLUSIONS = {"MANIFEST.json"}


def fail(message: str) -> None:
    print(f"FAIL: {message}", file=sys.stderr)
    raise SystemExit(1)


def bundle_paths() -> set[str]:
    return {
        path.relative_to(ROOT).as_posix()
        for path in ROOT.rglob("*")
        if path.is_file() and path.relative_to(ROOT).as_posix() not in MANIFEST_EXCLUSIONS
    }


def main() -> None:
    try:
        manifest = json.loads(MANIFEST.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        fail(f"cannot read MANIFEST.json: {exc}")

    entries = manifest.get("files")
    if not isinstance(entries, list):
        fail("manifest files is not a list")

    recorded = {entry.get("path") for entry in entries}
    if None in recorded or len(recorded) != len(entries):
        fail("manifest contains missing or duplicate paths")
    actual = bundle_paths()
    if recorded != actual:
        fail(
            "payload inventory mismatch; "
            f"missing={sorted(recorded - actual)} extra={sorted(actual - recorded)}"
        )

    decoded_pngs = 0
    parsed_json = 0
    decoded_text = 0
    declared_type_mismatches = 0
    for entry in entries:
        rel = entry["path"]
        path = ROOT / rel
        data = path.read_bytes()
        digest = hashlib.sha256(data).hexdigest()
        if digest != entry.get("sha256"):
            fail(f"sha256 mismatch: {rel}")
        if len(data) != entry.get("byte_size"):
            fail(f"byte size mismatch: {rel}")

        kind = entry.get("file_kind")
        if kind == "png":
            try:
                with Image.open(path) as image:
                    image.verify()
                with Image.open(path) as image:
                    image.load()
                    actual_image = {
                        "format": image.format,
                        "mode": image.mode,
                        "width": image.width,
                        "height": image.height,
                    }
            except Exception as exc:  # Pillow exposes several decode exception classes.
                fail(f"PNG decode failed: {rel}: {exc}")
            expected_image = {
                "format": entry.get("format"),
                "mode": entry.get("mode"),
                "width": entry.get("width"),
                "height": entry.get("height"),
            }
            if actual_image != expected_image:
                fail(f"PNG metadata mismatch: {rel}: {actual_image} != {expected_image}")
            decoded_pngs += 1
        elif kind == "json":
            try:
                json.loads(data.decode("utf-8"))
            except (UnicodeDecodeError, json.JSONDecodeError) as exc:
                fail(f"JSON validation failed: {rel}: {exc}")
            parsed_json += 1
        elif kind == "text":
            try:
                data.decode("utf-8")
            except UnicodeDecodeError as exc:
                fail(f"UTF-8 validation failed: {rel}: {exc}")
            decoded_text += 1
            if entry.get("declared_extension") == ".json":
                declared_type_mismatches += 1
        else:
            fail(f"unsupported file_kind {kind!r}: {rel}")

    print(
        "PASS: "
        f"{len(entries)} bundle files (manifest excluded); {decoded_pngs} PNGs fully decoded; "
        f"{parsed_json} JSON files parsed; {decoded_text} text files decoded; "
        "inventory, SHA256, byte sizes, types, and dimensions match"
    )
    if declared_type_mismatches:
        print(
            "WARNING: "
            f"{declared_type_mismatches} original .json-named file is not JSON; "
            "it is preserved byte-for-byte and validated as UTF-8 text (see MANIFEST.json)"
        )


if __name__ == "__main__":
    main()
