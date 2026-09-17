# I166 all-three findings regression report

Plan: docs/plans/i166-generation-boundaries-test-plan.md.

| Check | Result |
| --- | --- |
| 1. R3 decisions without new tasks | PASS: component integration test; browser R3 summary, affected cards and readable focus; original R1 labels remain |
| 2. Historical links | PASS: six historical edge/ELK/legacy tests retained; two actual inactive SVG paths visible in the R3 browser sample |
| 3. Start/End | PASS: root/leaf and empty/all-cancelled tests; two controls visible in component and browser; snapshot/task counts unchanged |
| 4. Existing graph/stage/legacy behaviour | PASS: graph layout and PlanDetail suites |
| 5. Browser | PASS: light/dark, English/Chinese and 390px mobile summary; screenshots included |
| 6. Full checks | PASS: 202 files / 1,954 frontend tests; make lint and make build exit 0 |

Images are a frontend regression sample, not a production screenshot. Reproduce by
copying fixture.tsx to web/i166-all3.tsx and serving an HTML root with that module;
use the existing readability fixture's Vite optimizeDeps es2022 configuration.
The fixture has R1 original review → integration → release, R2 remediation, R3
cancellation-only, and R4 follow-up, allowing all three findings on one view.

Deployment is outside this code delivery; production verification is not claimed.
