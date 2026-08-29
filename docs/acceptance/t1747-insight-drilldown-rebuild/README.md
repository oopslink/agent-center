# T1747 Insight Drilldown Rebuild Evidence

Fresh-base candidate evidence for the minimal Insight TaskExecution drilldown fix.

- Contract main: `5a18901eaea33c48247e2e8847a29f1d66038d40`
- Old rejected lineage was inspected only for the narrow fix shape; this branch was created from current `origin/main`.
- `SHA256SUMS` intentionally excludes itself. A manifest containing its own normal SHA-256 digest cannot be validated with standard `sha256sum -c` semantics without a custom self-hash convention, so the verifiable manifest covers every other file in this evidence directory, including `api-observations.jsonl`.

Verification:

```sh
cd docs/acceptance/t1747-insight-drilldown-rebuild
shasum -a 256 -c SHA256SUMS
```
