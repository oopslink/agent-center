# Access canonical S6 final acceptance

- Integrated candidate SHA: `1b5804fdee477c4901276bff2ff8c62a95c72e19`
- Expected and observed base: `7232c04df2bc902353107404d18b4bc3f5bd5712`
- Integration order: S3 RAM Roles, S4 Team Role RAM Roles, S5 Subject access.
- Built binary identity: `agent-center review/s6-integration-1b5804fd (commit 1b5804fd)`.

## Automated gates

- Frontend full suite: 192 files, 1803 tests passed.
- Canonical focused suite: 52 tests passed.
- TypeScript `--noEmit`: passed.
- Production Vite build and repository `make build`: passed.
- Backend `go test ./internal/webconsole/api`: passed.
- Fresh production-binary Chromium: RAM Roles full state matrix passed.
- Fresh production-binary Chromium: Team Role full state matrix passed.
- Fresh browser Subject access capture: 15 states passed.

## Browser evidence

The regenerated evidence is stored beside the S3, S4, and S5 acceptance reports. It covers 1672/1280, light/dark, loading, empty, service error, 403, 409, CRUD/CAS, deny precedence, direct grant/revoke, and success/error notifications. Browser runs used the exact integrated SHA above; no parent-task screenshot was accepted as this S6 verdict.

The existing Playwright output includes request/console/network diagnostics and videos for the fresh production-binary runs.
