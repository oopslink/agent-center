# T1184 Plan Gate UI acceptance

Baseline: `origin/main@3e3d425c`

## Browser matrix

- Chromium desktop light, 1440x900: `desktop-light.png`
- Chromium desktop dark, 1440x900: `desktop-dark.png`
- Chromium mobile light, 390x844: `mobile-390x844.png`

All captures use the production SPA embedded in the locally built
`bin/agent-center`, with authenticated browser navigation and controlled API
responses for a reopened human-gated stage.

## Automated checks

`tests/e2e/v2/tests/plan-gate-audit-layout.spec.ts` asserts with real browser
bounding boxes that the desktop stage audit bottom is at or above the first
member card top. The same run changes the viewport to 390x844 and verifies:

- evaluator and owner
- acceptance contract
- pending outcome, empty evidence, and empty reviewed SHA labels
- diagnostics

The focused Vitest suite also covers the explicit mobile stage API error state
and layout algebra reserves the full stage audit header before member cards.

## Commands

```sh
pnpm --dir web typecheck
pnpm --dir web exec vitest run src/pages/PlanDetail.test.tsx src/pages/planGraphLayout.test.ts
make build-frontend build-backend
PLAN_GATE_EVIDENCE_DIR="$PWD/docs/plans/evidence-t1184" \
  pnpm --dir tests/e2e/v2 exec playwright test \
  tests/plan-gate-audit-layout.spec.ts --project=chromium-mac
```

Result: 111 focused unit/component tests passed; 1 real Chromium acceptance
test passed.
