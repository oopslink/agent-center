# T2184 final fixed-candidate closeout

Verdict: **BLOCKED / PRODUCT NOT RUN**

## Provenance

- Fixed candidate: `ac-exec/task-340201c6/exec-7a7d087b@6074ea621f178889bcfca5adca8cf27c3ad174da`
- Remote `origin/main` before: `ddfb87d47e8253fb2e4f22d014f497301fa4b7ec`
- Candidate parent and merge-base: `ddfb87d47e8253fb2e4f22d014f497301fa4b7ec`
- Isolated build: `evidence/t2184-final-closeout-6074ea62`
- Web origin: `http://127.0.0.1:61301`
- Organization: `org-808e96ea`
- Data policy: official Agent Center MCP and authenticated public UI only. No database inspection/write, admin socket/HTTP, worker token, mock, or MSW.

## Result

The fixed candidate was fetched from the remote at the exact required SHA, built successfully, started in a fresh isolated instance, and exercised through the authenticated public UI. The Insight secondary navigation visibly contains **Collaboration effects**, and the direct URL `/organizations/org-808e96ea/insights/collaboration` loads without an import failure or unexpected 401.

The final product gates could not legally run. The isolated instance has zero workers. Its public **Add Agent** dialog shows `No compatible Runtime models` and keeps **Create agent** disabled. The supervisor successfully generated an auditable A→B reassignment fixture using official Agent Center MCP (`task-bdcab70a`, agent_A `agent:agent-20d5e05c`, agent_B `agent:agent-35ac0e16`), but there is no public fixture/event import path to move those center events into the isolated fixed-SHA instance. Using a bootstrap/worker token is explicitly forbidden by the task.

This is an environment BLOCKED result, not a product REJECT. No candidate was changed, no merge to `origin/main` was attempted, and fresh-main smoke was not run.

## Single external condition

Provide the isolated fixed-SHA instance with one officially enrolled compatible worker/runtime through an owner-managed path that does not expose or use a worker token in this acceptance session, **or** provide a documented public fixture/event import endpoint accepting the MCP-generated manifest. That is the only missing prerequisite for the 117+/250+ graph and the remaining interaction gates.

## Evidence

- `screenshots/01-signup.png` — fresh isolated public signup page.
- `screenshots/02-insight-nav.png` — authenticated Insight navigation with Collaboration effects.
- `screenshots/03-add-agent.png` — Add Agent blocked by no compatible runtime.
- `screenshots/04-direct-url.png` — authenticated direct URL reload on the fixed candidate.
- `raw/fixture-manifest.json` — official MCP-created and re-assigned fixture provenance.
- Fixture manifest SHA-256: `412040f86f2b1d06d911ffe54322dded2602ac0a071aeae93c94257419884ff9`.
- `verdict.json` — machine-readable authoritative outcome.

## Commands and exit codes

- `git ls-remote origin refs/heads/main refs/heads/ac-exec/task-340201c6/exec-7a7d087b` → `0`
- `git merge-base origin/main 6074ea621f178889bcfca5adca8cf27c3ad174da` → `0`, output `ddfb87d47e8253fb2e4f22d014f497301fa4b7ec`
- `make build` → `0`
- Candidate server startup → running and serving `http://127.0.0.1:61301`
- Browser signup/navigation/direct URL checks → `0`
- Full product gate → `NOT_RUN` due to the single external condition above.
