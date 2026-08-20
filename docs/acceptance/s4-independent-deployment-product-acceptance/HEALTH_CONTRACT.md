# Health Contract Disambiguation

## Decision

For candidate `69d5b662bef16882f1e163546da7b7168f80e1cd`, the Web Console deployment-health contract is:

```text
GET /api/health
200 OK
Content-Type: application/json
{"status":"ok","version":"<build version>"}
```

`GET /health` is **not** the Web Console health endpoint. It is a non-`/api` path and therefore enters the registered SPA catch-all. Its observed `200 OK`, `Content-Type: text/html`, and `index.html` body are the established router behavior for an unknown client route. That response must not be counted as a health probe success.

No production route or health semantic was changed for this remediation.

## Contract basis frozen with the bundle

- [`internal-webconsole-api-server.go.txt`](contract-source/internal-webconsole-api-server.go.txt) registers `GET /api/health` at line 128, registers the SPA catch-all for every non-`/api` path at lines 549–557, and defines the JSON health response at lines 559–569.
- [`internal-webconsole-spa-spa.go.txt`](contract-source/internal-webconsole-spa-spa.go.txt) documents and implements `index.html` fallback for every non-file path.
- [`internal-cli-handlers-test-instance.go.txt`](contract-source/internal-cli-handlers-test-instance.go.txt) treats `<webURL>/api/health`, not `/health`, as the installed center readiness probe at lines 314–317 and 403–407.
- [`docs-design-implementation-06-deployment.md.txt`](contract-source/docs-design-implementation-06-deployment.md.txt) freezes the deployment smoke path as `/api/health` at line 576 and separately describes React-router fallback at line 478.
- [`docs-design-migration-v2.0-to-v2.2.md.txt`](contract-source/docs-design-migration-v2.0-to-v2.2.md.txt) gives the operator probe as `/api/health` with JSON at line 197.

All six contract-source files are byte snapshots from candidate base commit `69d5b662bef16882f1e163546da7b7168f80e1cd`; their hashes are in [`MANIFEST.json`](MANIFEST.json).

## Deployed observations

- [`sources/eaebddbf/logs/06-health-http.txt`](sources/eaebddbf/logs/06-health-http.txt): `/health` returned `200 text/html` and the 594-byte SPA document.
- [`sources/eaebddbf/logs/07-api-health-http.txt`](sources/eaebddbf/logs/07-api-health-http.txt): `/api/health` returned `200 application/json` with `{"status":"ok","version":"ui-hard-gate-69d5b662"}`.
- [`sources/830c710e/evidence-exec-aafae534/logs/01-version-health.txt`](sources/830c710e/evidence-exec-aafae534/logs/01-version-health.txt): a second independent install recorded the same `/health` SPA behavior.
- [`sources/d3ca6c14/logs/32-http-probes-after-signup.txt`](sources/d3ca6c14/logs/32-http-probes-after-signup.txt): a third install recorded both responses side by side.

## Defect boundary

There is no unresolved JSON-health defect against the repository's frozen product/deployment contract, because that contract names `/api/health`. If an external frozen contract instead requires JSON specifically at `/health`, that requirement conflicts with the implementation and deployment documentation above and remains an unresolved product-contract defect; the HTML `200` cannot satisfy it. This bundle makes no such external-contract PASS claim.

