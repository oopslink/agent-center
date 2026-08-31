# Command Log

Candidate SHA: `e6c4d311652b1f7676900d4a9bc0283a1cacd29c`

The executor used only local filesystem, git, package-manager, build/test, curl, and browser-automation commands. No agent-center control-plane, database, socket, token, runtime config, or raw center HTTP fallback was used.

## Task Input And Repository State

```sh
pwd
rg --files task-input/v1 . | head -200
git status --short --branch
git rev-parse HEAD
git remote -v
sed -n '1,240p' task-input/v1/README.md
sed -n '1,260p' task-input/v1/manifest.json
find task-input/v1/attachments -maxdepth 3 -type f -print
git cat-file -t e6c4d311652b1f7676900d4a9bc0283a1cacd29c
git show --no-patch --format=fuller e6c4d311652b1f7676900d4a9bc0283a1cacd29c
git branch --contains e6c4d311652b1f7676900d4a9bc0283a1cacd29c --all
git ls-remote origin 'refs/heads/candidate/g4-insight-i1-i5-e6c4d311' 'refs/heads/main'
git log --graph --oneline --decorate -n 12 e6c4d311652b1f7676900d4a9bc0283a1cacd29c
```

Key outputs:

```text
Initial branch: ac-exec/task-8c8b63e5/exec-741b68b1
Initial HEAD: ad7959c60ea0d3004c44d65cfbe2c93de34c9406
Remote candidate: e6c4d311652b1f7676900d4a9bc0283a1cacd29c refs/heads/candidate/g4-insight-i1-i5-e6c4d311
Remote main: ad7959c60ea0d3004c44d65cfbe2c93de34c9406 refs/heads/main
Task input attachments: none
```

## Candidate Checkout

```sh
git switch --detach e6c4d311652b1f7676900d4a9bc0283a1cacd29c
git switch -c ac-exec/g5-insight-reacceptance-e6c4d311
```

## Code And Test Inspection

```sh
sed -n '1,220p' web/package.json
sed -n '1,620p' web/src/pages/InsightOverview.tsx
sed -n '1,520p' web/src/api/insights.ts
sed -n '1,280p' web/src/pages/InsightOverview.test.tsx
sed -n '1,260p' web/src/OrgContext.tsx
sed -n '1,300p' web/src/AppLayout.tsx
sed -n '1,760p' web/src/pages/InsightProjects.tsx
sed -n '1,260p' web/src/pages/InsightAgents.tsx
rg -n "Insight|insight|raw enum|object Object|state matrix|drilldown|drilldowns" -S .
```

## Build And Focused Tests

```sh
pnpm --dir web install --frozen-lockfile
pnpm --dir web exec vitest run src/pages/InsightOverview.test.tsx
pnpm --dir web build
```

Key outputs:

```text
InsightOverview.test.tsx: 8 passed
Build: built successfully
Build warning: Expected identifier but found "-" [css-syntax-error]
```

## Evidence Server And Served HEAD

```sh
PORT=4177 node docs/acceptance/g5-insight-e6c4d311/mock-server.mjs
agent-browser open http://127.0.0.1:4177/__head
agent-browser wait --load networkidle
agent-browser get text body
curl -sS http://127.0.0.1:4177/__head > docs/acceptance/g5-insight-e6c4d311/logs/served-head.json
curl -sS http://127.0.0.1:4177/__requests > docs/acceptance/g5-insight-e6c4d311/logs/served-requests.json
```

Key output:

```json
{"candidate_sha":"e6c4d311652b1f7676900d4a9bc0283a1cacd29c","served_from":".../internal/webconsole/spa/dist","request_count":98}
```

## Browser Evidence

```sh
agent-browser close
agent-browser errors --clear
agent-browser console --clear
agent-browser set viewport 1440 1000
agent-browser open http://127.0.0.1:4177/organizations/acme/insights/overview
agent-browser screenshot --full docs/acceptance/g5-insight-e6c4d311/screenshots/01-overview-desktop.png
agent-browser get text body > docs/acceptance/g5-insight-e6c4d311/logs/01-overview-desktop.txt
agent-browser open 'http://127.0.0.1:4177/organizations/acme/insights/executions?window=24h&agent_ref=agent%3Abuilder&project_id=proj-1&cursor=old'
agent-browser screenshot --full docs/acceptance/g5-insight-e6c4d311/screenshots/02-executions-filtered-desktop.png
agent-browser get text body > docs/acceptance/g5-insight-e6c4d311/logs/02-executions-filtered-desktop.txt
agent-browser open 'http://127.0.0.1:4177/organizations/acme/insights/executions/bad-time?window=24h&agent_ref=agent%3Abuilder&project_id=proj-1&cursor=old'
agent-browser screenshot --full docs/acceptance/g5-insight-e6c4d311/screenshots/03-execution-detail-invalid-quality.png
agent-browser get text body > docs/acceptance/g5-insight-e6c4d311/logs/03-execution-detail-invalid-quality.txt
agent-browser open 'http://127.0.0.1:4177/organizations/acme/insights/executions/missing'
agent-browser wait 6000
agent-browser screenshot --full docs/acceptance/g5-insight-e6c4d311/screenshots/04-execution-detail-not-found.png
agent-browser get text body > docs/acceptance/g5-insight-e6c4d311/logs/04-execution-detail-not-found.txt
agent-browser open 'http://127.0.0.1:4177/organizations/acme/insights/overview?case=rebuilding'
agent-browser wait 6000
agent-browser screenshot --full docs/acceptance/g5-insight-e6c4d311/screenshots/05-overview-rebuilding.png
agent-browser get text body > docs/acceptance/g5-insight-e6c4d311/logs/05-overview-rebuilding.txt
agent-browser open 'http://127.0.0.1:4177/organizations/acme/insights/overview?case=forbidden'
agent-browser wait 6000
agent-browser screenshot --full docs/acceptance/g5-insight-e6c4d311/screenshots/06-overview-forbidden.png
agent-browser get text body > docs/acceptance/g5-insight-e6c4d311/logs/06-overview-forbidden.txt
agent-browser open 'http://127.0.0.1:4177/organizations/acme/insights/overview?case=empty'
agent-browser screenshot --full docs/acceptance/g5-insight-e6c4d311/screenshots/07-overview-empty.png
agent-browser get text body > docs/acceptance/g5-insight-e6c4d311/logs/07-overview-empty.txt
agent-browser open 'http://127.0.0.1:4177/organizations/acme/insights/executions?case=rebuilding'
agent-browser wait 6000
agent-browser screenshot --full docs/acceptance/g5-insight-e6c4d311/screenshots/08-executions-rebuilding.png
agent-browser get text body > docs/acceptance/g5-insight-e6c4d311/logs/08-executions-rebuilding.txt
agent-browser open 'http://127.0.0.1:4177/organizations/acme/insights/agents'
agent-browser screenshot --full docs/acceptance/g5-insight-e6c4d311/screenshots/09-agents-list.png
agent-browser get text body > docs/acceptance/g5-insight-e6c4d311/logs/09-agents-list.txt
agent-browser open 'http://127.0.0.1:4177/organizations/acme/insights/agents/agent%3Abuilder'
agent-browser screenshot --full docs/acceptance/g5-insight-e6c4d311/screenshots/10-agent-detail.png
agent-browser get text body > docs/acceptance/g5-insight-e6c4d311/logs/10-agent-detail.txt
agent-browser open 'http://127.0.0.1:4177/organizations/acme/insights/projects'
agent-browser screenshot --full docs/acceptance/g5-insight-e6c4d311/screenshots/11-projects-list.png
agent-browser get text body > docs/acceptance/g5-insight-e6c4d311/logs/11-projects-list.txt
agent-browser open 'http://127.0.0.1:4177/organizations/acme/insights/projects/proj-1?plan_id=plan-1'
agent-browser screenshot --full docs/acceptance/g5-insight-e6c4d311/screenshots/12-project-detail-delivery-evolution.png
agent-browser get text body > docs/acceptance/g5-insight-e6c4d311/logs/12-project-detail-delivery-evolution.txt
agent-browser open 'http://127.0.0.1:4177/organizations/acme/insights/projects/proj-1/plans/plan-1/lineage'
agent-browser screenshot --full docs/acceptance/g5-insight-e6c4d311/screenshots/13-plan-lineage.png
agent-browser get text body > docs/acceptance/g5-insight-e6c4d311/logs/13-plan-lineage.txt
agent-browser set viewport 390 844
agent-browser open 'http://127.0.0.1:4177/organizations/acme/insights/overview'
agent-browser screenshot --full docs/acceptance/g5-insight-e6c4d311/screenshots/14-overview-mobile.png
agent-browser get text body > docs/acceptance/g5-insight-e6c4d311/logs/14-overview-mobile.txt
agent-browser open 'http://127.0.0.1:4177/organizations/acme/insights/executions/bad-time?case=unavailable'
agent-browser wait 6000
agent-browser screenshot --full docs/acceptance/g5-insight-e6c4d311/screenshots/15-execution-detail-unavailable.png
agent-browser get text body > docs/acceptance/g5-insight-e6c4d311/logs/15-execution-detail-unavailable.txt
```

## Regression Grep

```sh
rg -n "Something went wrong|\\[object Object\\]|quiet_finalized|new_enum|future_quality|future_status|future_reason_code|future_break_kind|raw_object|\\{\\\"" docs/acceptance/g5-insight-e6c4d311/logs/[0-9]*.txt docs/acceptance/g5-insight-e6c4d311/logs/final-browser-*.txt > docs/acceptance/g5-insight-e6c4d311/logs/raw-regression-grep.txt || true
```

Key output:

```text
logs/00-initial-mock-contract-error.txt: preserved initial mock-contract error
logs/10-agent-detail.txt: unknown_status raw_future_enum
logs/13-plan-lineage.txt: future_outcome
```

## Finalization

```sh
find docs/acceptance/g5-insight-e6c4d311 -type f ! -name artifact-manifest.sha256 | sort | xargs shasum -a 256 > docs/acceptance/g5-insight-e6c4d311/artifact-manifest.sha256
git status --short --branch
git diff --check
git add docs/acceptance/g5-insight-e6c4d311
git commit -m "docs(acceptance): record g5 insight reacceptance"
git push origin HEAD:refs/heads/ac-exec/g5-insight-reacceptance-e6c4d311
git remote show origin-push
git push origin-push HEAD:refs/heads/ac-exec/g5-insight-reacceptance-e6c4d311
git commit --amend --no-edit
git push --force-with-lease origin-push HEAD:refs/heads/ac-exec/g5-insight-reacceptance-e6c4d311
```

The direct `origin` push failed with `fatal: --mirror can't be combined with refspecs`.
The branch was pushed successfully with `origin-push`; this log was then amended
into the evidence commit and force-updated on the same evidence branch.
