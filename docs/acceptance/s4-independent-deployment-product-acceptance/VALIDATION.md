# Evidence Validation

## Result

- Payload inventory: 254 files, 20,381,841 bytes, 217 unique SHA256 values.
- Images: all 112 original PNG files passed Pillow `Image.verify()`, were reopened, and passed full `Image.load()` decode. Width, height, mode, byte size, and SHA256 are recorded per file in [`MANIFEST.json`](MANIFEST.json).
- Structured logs: 17 JSON files passed UTF-8 decode and JSON parse.
- Text: 125 files passed UTF-8 decode.
- Video: none of the three source evidence trees contains `.webm`, `.mp4`, `.mov`, `.gif`, `.jpg`, or `.jpeg`; therefore no video was omitted from the bundle.
- Duplicate payloads: 37 paths have bytes identical to an earlier path. Every separately named original capture is retained; `duplicate_of` records the first identical path so reviewers can de-duplicate consumption without losing provenance.

## Explicit source defect — not hidden as PASS

`sources/eaebddbf/logs/03-install-test-instance.json` is named `.json` in the source commit but begins with command transcript text and fails JSON parsing at line 1, column 3. It is preserved byte-for-byte, classified as `text/plain`, and successfully decoded as UTF-8. The manifest records `declared_type_mismatch_count: 1` and the per-file parse failure. It is **not** represented as valid JSON.

No PNG decode failed. Had any image failed signature verification or full pixel decode, the manifest generation and verifier would have stopped rather than recording a PASS.

## Re-run

From the repository root:

```bash
python3 docs/acceptance/s4-independent-deployment-product-acceptance/verify_bundle.py
```

The verifier fails closed on missing or extra payload files, SHA256 or byte-size mismatch, invalid JSON where the manifest declares JSON, non-UTF-8 text, incomplete PNG decode, or dimension/type mismatch. Pillow is intentionally required for full image decode.

Useful inventory queries:

```bash
jq '.summary' docs/acceptance/s4-independent-deployment-product-acceptance/MANIFEST.json
jq -r '.files[] | select(.file_kind == "png") | [.path,.sha256,.byte_size,.width,.height,.source_content_commit] | @tsv' docs/acceptance/s4-independent-deployment-product-acceptance/MANIFEST.json
```

