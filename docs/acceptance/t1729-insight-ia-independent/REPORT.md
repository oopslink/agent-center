# T1729 Insight IA independent browser acceptance

## Verdict

**REJECT**

The locked candidate cannot satisfy the Insight information architecture acceptance contract from a real Web entry. A newly created real organization can reach the Insight navigation, but both core Insight routes fail before the user can judge data trustworthiness, capacity/runtime abnormality, problem concentration, or drill down from Overview to Agent/Project to Execution.

## Candidate lock

- Immutable candidate ref: `refs/heads/immutable/t1729-insight-api-ui-3cc52ed3`
- Candidate SHA: `3cc52ed300bfe00444d6487aded97e96280d2b16`
- Candidate subject: `Implement insight semantics IA`
- Remote readback: `git ls-remote origin refs/heads/immutable/t1729-insight-api-ui-3cc52ed3` returned `3cc52ed300bfe00444d6487aded97e96280d2b16`
- Base / merge-base against `origin/main`: `1f27bfe74e3dddaf3ffb7e0156c5a61ad55480b4`
- Task input package: `task-input/v1/README.md` and `task-input/v1/manifest.json` were not materialized anywhere under this workspace; candidate lock was therefore taken from the matching immutable remote ref and verified by `ls-remote`.

## Test environment

- Web entry: `http://127.0.0.1:4179/`
- Browser tool: `agent-browser`
- Session: `insight-ia`
- Account created through real UI:
  - Display name: `Insight Tester`
  - Email: `insight-tester@example.com`
  - Organization: `Insight Acceptance Org`
- Organization route observed: `/organizations/org-fce27903/...`

## Build and test results

| Command | Result | Evidence |
|---|---:|---|
| `pnpm --dir web install --frozen-lockfile` | PASS | `raw/01-web-pnpm-install.log` |
| `pnpm --dir web run test` | PASS, 194 files / 1817 tests | `raw/02-web-test.log` |
| `pnpm --dir web run build` | PASS, Vite built in 11.69s; CSS/chunk warnings only | `raw/03-web-build.log` |

## Acceptance matrix

| Area | Expected user outcome | Observed | Result | Evidence |
|---|---|---|---:|---|
| Candidate identity | Exact immutable candidate SHA is locked before testing | Locked `3cc52ed300bfe00444d6487aded97e96280d2b16` from remote immutable ref | PASS | `raw/provenance.md` |
| Real Web entry | User reaches Insight from real login/signup and org navigation | Signup succeeded and Insight nav was visible | PASS | `screenshots/03-post-register-state.png` |
| Overview decision path | Overview shows trustworthy, decision-useful status for capacity/runtime/problem concentration | Route first entered ErrorBoundary with `Cannot read properties of null (reading 'map')`; on later load stayed at `Loading Insight overview` without metrics or empty/stale explanation | REJECT | `screenshots/04-insight-overview-empty-org.png`, `screenshots/issue-001-result-after-wait.png`, `raw/04-console-after-issue-001.log` |
| Execution list | Execution route has no dangling details and renders empty/error/loading/stale states honestly | Direct route ended in ErrorBoundary with `Cannot read properties of null (reading 'length')`; no empty state or freshness/coverage context | REJECT | `screenshots/08-executions-after-10s.png`, `screenshots/12-desktop-executions-after-12s.png`, `raw/09-console-final.log` |
| Overview to Execution drilldown | `View all executions` preserves context and reaches execution list | Link exists, but destination cannot render a usable execution list | REJECT | `screenshots/04-insight-overview-empty-org.png`, `screenshots/08-executions-after-10s.png` |
| Normal / attention / severe / unknown data | User can distinguish normal, attention, severe, unknown states with controlled data | Not testable because the core Insight routes fail on a fresh real organization before scenario data can be interpreted | BLOCKED BY REJECT | `screenshots/issue-001-step-3-error.png` |
| Chart choice and labels | Charts communicate actionable, non-misleading semantics | Not reached; no charts rendered | REJECT | `screenshots/issue-001-result-after-wait.png` |
| Time window / coverage / freshness | Window, coverage, freshness and low-coverage semantics are visible | Overview never renders beyond loading/error; executions only shows `Past 24 hours (rolling) Loading executions` then crashes | REJECT | `screenshots/07-executions-direct-empty-org.png`, `screenshots/08-executions-after-10s.png` |
| Samples and measurement basis | UI explains sample size and basis | Not reached; no samples/basis rendered | REJECT | `screenshots/04-insight-overview-empty-org.png` |
| Filters / return context | Filters link across Overview, Agent/Project, Execution without losing context | Not testable because execution list route crashes | BLOCKED BY REJECT | `screenshots/12-desktop-executions-after-12s.png` |
| Loading / empty / error / stale / low coverage | States are distinct and honest; errors are not masked as zero | Fails: empty org is not represented as empty/unknown; user gets indefinite loading or raw crash text | REJECT | `screenshots/09-mobile-overview.png`, `screenshots/10-mobile-executions.png`, `screenshots/12-desktop-executions-after-12s.png` |
| i18n | Visible Insight text is localized/consistent | Only English copy observed; route failure prevented deeper i18n validation | BLOCKED BY REJECT | `screenshots/04-insight-overview-empty-org.png` |
| Keyboard/basic accessibility | Main controls are keyboard reachable and labeled | Basic nav controls expose accessible names; full workflow cannot be verified because routes fail | PARTIAL | `screenshots/11-mobile-keyboard-tabs.png` |
| Desktop and narrow screen | Desktop and 390px narrow viewport remain usable | Desktop routes crash; narrow routes remain stuck in loading during the capture window | REJECT | `screenshots/08-executions-after-10s.png`, `screenshots/09-mobile-overview.png`, `screenshots/10-mobile-executions.png` |
| Console/network | No route-level JS failures or hidden failed requests | Console records React ErrorBoundary crashes in `InsightOverview`; network request capture returned no browser-side request log | REJECT | `raw/04-console-after-issue-001.log`, `raw/09-console-final.log`, `raw/10-network-requests.log` |

## Findings

### ISSUE-001: Insight Overview crashes or stalls on a real empty organization

- Severity: **critical**
- Category: Functional / Console / UX
- Design clause: A user must be able to judge data trustworthiness, capacity/runtime abnormality, problem concentration, and coverage/freshness from Overview. Empty/unknown/low-coverage states must be explicit, not raw crashes or indefinite loading.
- Minimal repro:
  1. Open `http://127.0.0.1:4179/` on candidate `3cc52ed300bfe00444d6487aded97e96280d2b16`.
  2. Create an account and organization through Sign up.
  3. From Workspace, click the `Insight` module.
  4. Observe `Insight overview` never reaches a usable analytics view. One reproduction enters ErrorBoundary with `Cannot read properties of null (reading 'map')`; another remains at `Loading Insight overview` long enough to prevent user judgment.
- Expected: Fresh org should render a clear empty/unknown coverage state with freshness/window/sample basis, or a purposeful recoverable error.
- Actual: Raw React crash or unresolved loading state.
- Evidence:
  - Video: `videos/issue-001-insight-overview-crash.webm`
  - Steps: `screenshots/issue-001-step-1-workspace.png`, `screenshots/issue-001-step-2-loading-or-nav.png`, `screenshots/issue-001-step-3-error.png`
  - Confirmed error: `screenshots/issue-001-result-after-wait.png`
  - Console: `raw/04-console-after-issue-001.log`

### ISSUE-002: Task executions route crashes on the same real organization

- Severity: **critical**
- Category: Functional / Console / UX
- Design clause: There must be no dangling Execution details; execution list/detail states must distinguish loading, empty, error, stale and low coverage.
- Minimal repro:
  1. With the same signed-in organization, open `/organizations/org-fce27903/insights/executions`.
  2. Wait 10-12 seconds.
  3. Observe the route enters ErrorBoundary with `Cannot read properties of null (reading 'length')`.
- Expected: A clear empty execution list or unavailable/freshness envelope, with no raw crash.
- Actual: Raw ErrorBoundary crash, no trustworthy execution context, no drilldown path.
- Evidence:
  - `screenshots/07-executions-direct-empty-org.png`
  - `screenshots/08-executions-after-10s.png`
  - `screenshots/12-desktop-executions-after-12s.png`
  - `raw/09-console-final.log`

## Screenshot and video index

- Entry/auth: `screenshots/00-entry-desktop.png`, `screenshots/01-signup.png`, `screenshots/02-after-signup-state.png`, `screenshots/03-post-register-state.png`
- Desktop Insight: `screenshots/04-insight-overview-empty-org.png`, `screenshots/07-executions-direct-empty-org.png`, `screenshots/08-executions-after-10s.png`, `screenshots/12-desktop-executions-after-12s.png`
- Narrow screen: `screenshots/09-mobile-overview.png`, `screenshots/10-mobile-executions.png`, `screenshots/11-mobile-keyboard-tabs.png`
- Repro video: `videos/issue-001-insight-overview-crash.webm`

## Final decision

**REJECT**. The candidate fails critical routes from the real Web entry. This is not a cosmetic gap: the user cannot use Insight Overview or Task executions to decide whether data is trustworthy, whether capacity/runtime is abnormal, where problems concentrate, or how to drill down to execution evidence.
