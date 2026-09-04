# Collaboration Graph Main Acceptance

Verdict: **REJECT**

Date: 2026-09-04

## Scope

- Requested candidate: `origin/main@d2b969eb` or later `main` containing that commit.
- Tested candidate checkout: `d2b969eb746ede085fd4195cf3229d1b6092a807`
- Local `origin/main` observed during this run: `50316a712444017c8c10777500c4f1a88db63645`
- `git merge-base --is-ancestor d2b969eb origin/main`: exit `1`; the observed `origin/main` did not contain the requested commit.
- Implementation changes: none.

## Environment

- Workspace: `/Users/oopslink/.agent-center/workers/worker-edb09a0c/var/runtime/worktrees/exec-18b50efe`
- Browser tool: `agent-browser`, named sessions:
  - `collab-graph-main-d2b969eb`
  - `collab-graph-main-d2b969eb-auth`
  - `ac-login-probe-891`
  - `collab-graph-blocked-video`
- Web Console URL used: `http://127.0.0.1:7100`
- Existing server observed on `127.0.0.1:7100` and `*:7300`; the candidate binary was built, but a parallel candidate server was not attached to the live center database because another center process already owned the production ports.
- Candidate build command: `make build`
- Build observation: Vite completed with one CSS minifier warning, saved only in terminal output; no implementation files were changed intentionally.

## Data Range

The only stable authenticated page observed before Insight navigation failure was the organization project list:

- Organization: `ooo`
- Project `agent-center2`: `1703 tasks`, `149 issues`, `97 plans`, `1 repo`
- Other visible projects: `赛博功德`, `spark-manager`

Evidence: `screenshots/04-auth-projects-data-scope.png`

## Result Matrix

| Check | Result | Evidence |
| --- | --- | --- |
| Default first-screen full graph and filter/locate area | **NOT RUN** | Blocked before Collaboration Graph rendered. |
| Button zoom and wheel zoom | **NOT RUN** | Blocked before graph rendered. |
| Canvas pan | **NOT RUN** | Blocked before graph rendered. |
| Node drag | **NOT RUN** | Blocked before graph rendered. |
| Fit | **NOT RUN** | Blocked before graph rendered. |
| Reset | **NOT RUN** | Blocked before graph rendered. |
| Click node to focus neighborhood, then restore full graph | **NOT RUN** | Blocked before graph rendered. |
| Apply filter, then clear and restore organization full graph | **NOT RUN** | Blocked before graph rendered. |
| Dark background contrast | **NOT RUN** | Blocked before graph rendered. |
| Label truncation / hover / detail | **NOT RUN** | Blocked before graph rendered. |
| Node / edge / text overlap | **NOT RUN** | Blocked before graph rendered. |
| Default clustering / layering readability | **NOT RUN** | Blocked before graph rendered. |
| Console / page errors | **FAIL** | Insight navigation produced dynamic module load failure and repeated 401 console errors. |

Overall result is **REJECT** because the required real-page graph evidence could not be produced, and the requested `origin/main` ancestry condition was not satisfied by the observed remote ref.

## Shortest Reproduction

1. Open the real Web Console at `http://127.0.0.1:7100`.
2. Use an already-authenticated browser session on organization `ooo`.
3. From `/organizations/ooo/projects`, click `Insight`.
4. The app shows `Something went wrong. Failed to fetch dynamically imported module: http://127.0.0.1:7100/assets/InsightOverview-CkZblBBh.js` with only a `Reload` button.
5. Browser-level reload of `/organizations/ooo/insights/overview` redirects to `/signin`, preventing Collaboration Graph validation.

## Evidence Files

- `screenshots/00-entry.png` - fresh unauthenticated entry at Web Console.
- `screenshots/01-auth-entry.png` - attempted persisted state, still unauthenticated.
- `screenshots/02-insight-entry.png` - first Insight attempt showing only `Reload`.
- `screenshots/03-insight-after-reload.png` - reload after first Insight attempt returns to sign-in.
- `screenshots/04-auth-projects-data-scope.png` - authenticated organization project list and real data range.
- `screenshots/05-insight-attempt-2.png` - second Insight attempt showing only `Reload`.
- `screenshots/06-insight-browser-reload.png` - browser reload redirects to sign-in.
- `screenshots/07-video-blocked-insight-auth.png` - frame from blocked-current-state recording.
- `videos/blocked-insight-auth.webm` - recording of direct Insight URL resolving to sign-in.
- `logs/02-insight-entry-text.txt` - text of first Insight error state.
- `logs/02-console.txt` - console output for first Insight attempt.
- `logs/02-errors.txt` - page errors for first Insight attempt.
- `logs/05-insight-attempt-2-text.txt` - text of second Insight error state.
- `logs/05-console.txt` - console output for second Insight attempt.
- `logs/05-errors.txt` - page errors for second Insight attempt.
- `logs/06-insight-browser-reload-text.txt` - text after browser reload.
- `logs/06-console.txt` - console output after browser reload.
- `logs/06-errors.txt` - page errors after browser reload.
- `logs/candidate-git-head.txt` - tested checkout metadata.
- `logs/origin-main-git-head.txt` - observed `origin/main` metadata.
- `logs/origin-main-contains-d2b969eb.txt` - ancestry check result.
- `logs/git-status-before-report.txt` - status before report creation.
