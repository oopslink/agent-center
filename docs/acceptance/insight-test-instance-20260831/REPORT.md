# Insight candidate isolated test-instance deployment report

Date: 2026-08-31 UTC

## Verdict

- Deployed-smoke on a real isolated launchd test-instance: PASS.
- Overall acceptance gate: FAIL/BLOCKED because `make lint` fails at `go vet`.

This report does not weaken the gate. The candidate was deployable and smoke-tested, but cannot be signed off while the lint gate is red.

## Candidate

- Branch: `ac-exec/task-bc021533/exec-8d107cb7`
- Candidate SHA: `0a02c230b0df9a885eccecf17482920f929b230a`
- `origin/main`: `0a02c230b0df9a885eccecf17482920f929b230a`
- Binary version: `agent-center ac-exec/task-bc021533/exec-8d107cb7-0a02c230 (commit 0a02c230)`
- Task input package: `task-input/v1` was not present in this workspace; local search found no `task-input` directory.

Evidence: `raw/00-provenance.log`.

## Build Gate

- `make lint`: FAIL. `go vet ./...` reports `internal/admin/api/agent_tools_write.go:794:2: unreachable code`.
- `make build`: PASS. Built `bin/agent-center` and `bin/fakeagent` from the candidate.

Evidence: `raw/01-make-lint.log`, `raw/02-make-build.log`.

## Installed Test Instance

Command:

```bash
./bin/agent-center install test-instance --id insight-smoke-20260831 --with-seed --workers 1 --output json
```

Result:

- Instance: `insight-smoke-20260831`
- Prefix: `/Users/oopslink/.agent-center-test/insight-smoke-20260831`
- Web URL: `http://127.0.0.1:49515`
- Server port: `49516`
- Admin port: `49517`
- Seeded org slug: `org-75edacca`
- Seeded org/project/channel: `organization-340fdd93`, `project-0f187407`, `channel-9af573ea`
- Worker: `test-insight-smoke-20260831-w1`

The install output and generated configs were recorded with tokens/passcodes redacted.

Evidence: `raw/03-install-test-instance.log`, `raw/05-generated-config.log`.

## Health And Version

- `GET /api/health`: HTTP 200
- Health body: `{"status":"ok","version":"ac-exec/task-bc021533/exec-8d107cb7-0a02c230"}`
- `/api/version` is not a route in this candidate; version evidence comes from `/api/health` and `agent-center version`.

Evidence: `raw/04-health-version-auth.log`, `raw/09-deployed-smoke-asserted.log`.

## Service State

Launchd reports both services running from the isolated prefix:

- `com.agent-center.center.test-insight-smoke-20260831`: `state = running`, pid `11037`
- `com.agent-center.worker.test-insight-smoke-20260831-w1`: `state = running`, pid `11076`

`agent-center list-test-instances` reports `insight-smoke-20260831` with `WORKERS 1` and `ONLINE yes`.

The `--with-seed` mode install output says workers are workforce-enrolled but not org-enrolled; the worker log therefore contains expected `worker_not_org_enrolled` control-connect retries. The process is still launchd-running and listed online by the test-instance tool.

Evidence: `raw/06-launchctl-status.log`, `raw/07-service-logs-tail.log`, `raw/10-list-test-instances.log`.

## Deployed-Smoke

Script:

```bash
AC_PASSCODE=<seeded-passcode-from-local-install-output> bash docs/acceptance/insight-test-instance-20260831/run_deployed_smoke.sh
```

Asserted real-instance path:

- Health: HTTP 200
- Signin with seeded owner/org: HTTP 200
- `/api/auth/me`: HTTP 200
- `/api/orgs`: HTTP 200
- `/api/orgs/org-75edacca/projects`: HTTP 200
- Seeded project detail: HTTP 200
- `/api/orgs/org-75edacca/conversations`: HTTP 200
- Seeded channel detail: HTTP 200
- `/api/orgs/org-75edacca/fleet`: HTTP 200
- Files upload create: HTTP 201
- Files upload PUT bytes: HTTP 200
- Files upload complete: HTTP 200

Final marker: `deployed_smoke_asserted=PASS`.

Evidence: `raw/09-deployed-smoke-asserted.log`.

Note: `raw/08-deployed-smoke.log` is an invalid first attempt retained for audit. It used the wrong signin field and did not fail on HTTP 401s; it was superseded by the fail-fast asserted script above.

## Isolation Boundary

All install/runtime paths recorded in evidence point under `/Users/oopslink/.agent-center-test/insight-smoke-20260831`. The run did not use Runtime Control MCP, agent-center center tools, production admin sockets, worker tokens, production `~/.agent-center`, or raw HTTP to agent-center control-plane endpoints.

## Cleanup Plan

Keep the instance running for reviewer access until the Supervisor/owner no longer needs it.

Cleanup command:

```bash
./bin/agent-center uninstall test-instance --id insight-smoke-20260831
```

Expected cleanup effect: boot out the test-instance launchd labels and remove `/Users/oopslink/.agent-center-test/insight-smoke-20260831`. If manual verification is needed before cleanup, use:

```bash
launchctl print gui/$(id -u)/com.agent-center.center.test-insight-smoke-20260831
launchctl print gui/$(id -u)/com.agent-center.worker.test-insight-smoke-20260831-w1
./bin/agent-center list-test-instances
```
