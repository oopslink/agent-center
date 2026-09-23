# Computer Use bundle drift regression plan

Scope: runtime Codex MCP configuration; no domain, database, concurrency, or permission-policy changes.

1. Reproduce empty shared bundle with healthy guest-local service: config must keep required node_repl and point at the local signed app.
2. Enabled Computer Use with missing dependencies: return an explicit error, preserve previous config; disabled Computer Use still works without dependencies.
3. Existing source-backed/native host configurations remain valid; runtime package and full Go suite pass.
4. In analyst VM, run actual product CodexSession to obtain Safari UI text and screenshot through node_repl/Sky. Repeat after a fresh runtime restart to verify regeneration.

Root cause evidence: deployed service9e09f2a2; guest sees source computer-use app as an empty directory while host bundle and guest-local app both exist. Old resolver checks source before reaching its local-app override and silently omits MCP. Selecting the service that runs in the guest preserves existing trust and signed caller authentication; missing prerequisites become a startup error.
