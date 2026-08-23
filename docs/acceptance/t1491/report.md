# T1491 fresh exact-SHA final acceptance

- Candidate and `origin/main`: `7700038b0f27c4d927b806149c7fb87c32c29f0c`
- Candidate parent / prior `origin/main`: `16c83c62a60779dfbcdb7ce102912a917299c661`
- Fresh install: `agent-center install test-instance --id t1491-nav --with-seed`
- Runtime build identity: version `t1491-7700038b`, branch `main`, commit `7700038b0f27c4d927b806149c7fb87c32c29f0c`
- Browser: Chromium through agent-browser, viewport `1672x941`, light scheme

## Result

PASS.

1. The remote candidate is exactly one clean commit ahead of the previous main and changes only `web/src/AppLayout.tsx` plus `web/src/App.test.tsx`.
2. Full web Vitest regression and production web build passed; formal `make build` passed with the exact commit embedded.
3. On the fresh installed runtime, the three URLs were independently opened and each exposed exactly one secondary-nav link with `aria-current=page`:
   - `?view=ram-roles` -> `RAM Roles`
   - `?view=team-role-mappings` -> `Team Role mappings`
   - `?view=subject-access` -> `Subject access`
4. RAM Role browser CRUD passed: create `T1491 Browser Role`, create v2 after editing description/permissions, then delete.
5. Subject direct-binding flow passed through scope, preview, confirm, and result. The valid project grant succeeded; the deliberately inapplicable org target remained `Not applicable`; the concurrent duplicate/conflict path surfaced a visible `409 conflict` instead of silently succeeding.
6. The final clean three-page navigation pass produced no JavaScript/page errors and no console errors. Browser network log showed the expected Access reads for RAM roles, teams, overview, effective permissions, and audit.
7. Team Role mapping CRUD/CAS and canonical full-state behavior were already accepted on the immediate parent by T1487/T1488; the final candidate contains no mapping/API/style change. The fresh exact-SHA mapping route remained reachable and rendered its optimistic-concurrency contract and permission catalog.

## Screenshots

- `ram-roles-1672x941.png`
- `team-role-mappings-1672x941.png`
- `subject-access-1672x941.png`

The screenshots visibly show the same three-page Access IA and exactly one purple active item in both the secondary rail and tab row. Dynamic seeded content is intentionally not treated as a pixel regression against another tenant's data; canonical structure and behavior are inherited unchanged from the accepted parent, while this candidate's only visual delta is the corrected active selection.
