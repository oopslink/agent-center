# T1802 Runtime Control Integration Verification

Date: 2026-08-31

Integrated delivery under review: `61b217ff7d88e88fdfeaca04c754de67807ae155`

Outcome:

- The delivered T1802 commit was already present at `origin/main` when this executor fetched `origin main`.
- No runtime-control implementation was rewritten in this integration commit.
- The agent-facing MCP registry exposes `runtime_deploy_restart` in the strict supervisor startup catalog.
- `runtime_deploy_restart` remains authenticated through the MCP-to-agent-tools path: non-worker tokens and worker/agent mismatches fail closed, while server-side remote ref verification is required before worker control-stream dispatch.
- Worker-side runtime deploy execution rejects caller-asserted verification booleans and requires server-populated `verified_target_sha`, `verified_base_sha`, and `verified_at`.

Verification commands run:

- `git fetch origin main && git rev-parse HEAD origin/main origin/HEAD`
- `go test ./internal/mcphost -run 'TestRequireTools_TieredCatalogIncludesSupervisorCore|TestAgentFacingToolParity'`
- `go test ./internal/admin/api -run 'TestRuntimeDeployRestartHandler_(VerifiesRemoteBeforeEnqueue|RejectsMismatchedRemoteSHA|RejectsNonWorkerToken|RejectsWrongWorkerAgent)'`
- `go test ./internal/workerdaemon -run 'TestControllerHandler_RuntimeDeploy|TestSourceRuntimeDeployer|TestParseAgentCenterVersionReadback'`
- `go test ./internal/agentruntime -run 'TestStartCodex_PreflightsFullCatalogForNativeToolSearch|TestStartCodex_WritesMCPConfigAndCodexHome|TestStartCodex_BlocksWhenSupervisorMCPPreflightFails|TestStartClaude_BlocksWhenSupervisorMCPPreflightFails'`
- `make gen-mcp-docs`
- `go test ./internal/runtimedeploy ./internal/mcphost ./internal/agentruntime ./internal/admin/api ./internal/workerdaemon`
- `go test ./...`

All test commands above passed. `make gen-mcp-docs` regenerated the MCP docs without leaving a git diff.

Notes:

- The declared `task-input/v1/README.md` and `task-input/v1/manifest.json` were not present in this workspace, and a workspace search found no `task-input` directory.
- No agent-center admin socket/HTTP endpoint, database file, worker token, process argument, or runtime config fallback was used for recovery or verification.
