package e2e

// Phase 3 e2e tests previously drove the issue lifecycle WRITE commands
// (`issue open` / `issue comment` / `issue conclude` / `issue withdraw` and the
// `open-issue` agent verb). Those CLI commands were removed in #132 (issue
// management moved to webconsole/admin/MCP on the new pm model), so the tests —
// which asserted those deleted commands' own behavior (lazy-create, conclude
// task-spawn, withdraw, conclude rollback, agent open-issue origin) — were
// deleted rather than reworked: the behavior under test is intentionally gone.
