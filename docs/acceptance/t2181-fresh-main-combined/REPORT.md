# T2181 Independent Acceptance

product_verdict: **PASS**

Validated candidate `6074ea621f178889bcfca5adca8cf27c3ad174da` on base `ddfb87d47e8253fb2e4f22d014f497301fa4b7ec`; merge-base is `ddfb87d47e8253fb2e4f22d014f497301fa4b7ec`. The candidate is a fresh-main combination commit; prior T2176/T2179 SHAs were checked for behavioral/file coverage, not ancestry.

## Isolation Provenance
- install_root: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2181-combined-yTffoi
- web_origin: http://127.0.0.1:64839
- grpc_port: 64840
- admin_tcp_port: 64841
- database: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2181-combined-yTffoi/agent-center.db
- duckdb: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2181-combined-yTffoi/agent-center.duckdb
- runtime_identity: AGENT_CENTER_INVOCATION_ID=t2181-combined-ui
- cookie_namespace: ac_session@127.0.0.1
- data_provenance: Seeded through authenticated production HTTP APIs in a fresh isolated instance; no dev server, MSW, mock API, production instance, or agent-center control-plane fallback.
- organization_slug: org-e6824511
- project_ids: project-1a8227f5, project-f6f629c2

## Graph Dataset
- nodes: 130
- edges: 282
- effects: 2
- node_kinds: agent, plan, project, task
- relation_types: agent_plan, agent_task, plan_task, project_plan, reassign, task_dependency
- multi_project: true

## Coverage Matrix
| Source | Status | Files | Behavior |
|---|---|---|---|
| T2176 1e14098b formal navigation | COVERED | web/src/AppLayout.tsx<br>web/src/shell/nav/InsightSecondaryNav.test.tsx<br>web/src/AppLayout.insightnav.test.tsx<br>web/src/AppLayout.mobilenav.test.tsx | Authenticated Insight rail shows Collaboration effects link; direct /organizations/{slug}/insights/collaboration refresh renders without import errors or unexpected 401. |
| T2176 1e14098b real wheel/zoom/pan/drag/focus/restore | COVERED | web/src/pages/InsightCollaboration.tsx | Buttons, real browser wheel event, pan, node drag, Fit, Reset, keyboard focus, and restore all changed SVG viewBox/node position as recorded in hard gate 5. |
| T2176 1e14098b LOD/truncation feedback | COVERED | web/src/api/insights.ts<br>web/src/pages/InsightCollaboration.tsx<br>web/src/i18n/locales/en/insights.json<br>web/src/i18n/locales/zh/insights.json | API accepts lod/max_nodes and returns cluster/truncated metadata; UI shows LOD notice and Show full action. |
| T2176 1e14098b large graph readability | COVERED | web/src/pages/InsightCollaboration.tsx | 130-node/282-edge org graph renders clustered first screen with readable SVG density and performance under threshold. |
| T2179 94945e8f project_id 503 fix | COVERED | internal/observability/collaborationeffect/graph_sqlite.go<br>internal/observability/collaborationeffect/query_test.go<br>internal/webconsole/api/handlers_collaboration_insight_test.go<br>web/src/api/insights.ts | Valid project_id API/UI returns 200 and scoped graph; clear restores org graph; malformed cursor is 400, unauth is 401, missing/cross-org project is 404, and no probe returns 503. |

## Hard Gates
| # | Gate | Result | Evidence |
|---|---|---|---|
| 1 | Authenticated navigation and direct URL refresh | PASS | nav_link=true; org_url=http://127.0.0.1:64839/organizations/org-e6824511/insights/collaboration; unexpected401=0; import_errors=0 |
| 2 | No-query first screen non-empty organization graph | PASS | url=http://127.0.0.1:64839/organizations/org-e6824511/insights/collaboration; svg_nodes=7; svg_edges=10; api_nodes=130; api_edges=282 |
| 3 | Agent-Agent, Agent-Task, Agent-Plan, Plan-Task same screen | PASS | agent_agent=true; agent_task=true; agent_plan=true; plan_task=true; effects=2; edge_text_sample=plan task · Neutral · strength 1 · 78 effects · evidence 0 \| agent task · Neutral · strength 1 · 78 effects · evidence 0 \| agent plan · Neutral · strength 1 · 1 effects · evidence 0 \| plan task · Neutral · strength 1 · 3 effects · evidence 0 \| agent task · Neutral · strength 1 · 3 effects · evidence 0 \| agent plan · Neutral · strength 1 · 1 effects · evidence 0 |
| 4 | Search/filter and clear restores full graph | PASS | filtered=http://127.0.0.1:64839/organizations/org-e6824511/insights/collaboration?project_id=project-1a8227f5; cleared=http://127.0.0.1:64839/organizations/org-e6824511/insights/collaboration |
| 5 | Zoom/pan/drag/Fit/Reset/focus/restore interactions | PASS | {"pass":true,"before":{"viewBox":"120 0 320 304","nodes":4,"edges":6},"zoomIn":{"viewBox":"148.8 42.120000000000005 262.4 383.76","nodes":7,"edges":10},"zoomOut":{"viewBox":"125.18400000000003 7.581600000000009 309.63199999999995 452.8368","nodes":7,"edges":10},"resetForWheel":{"viewBox":"120 0 320 468","nodes":7,"edges":10},"wheel":{"viewBox":"156.0619135253906 52.763769726562515 247.80800000000002 362.4192","nodes":7,"edges":10},"pan":{"viewBox":"132.70583435479682 43.415727892809365 247.80800000000002 362.4192","nodes":7,"edges":10},"fit":{"viewBox":"120 0 392.615966796875 468","nodes":7,"edges":10},"focus":{"viewBox":"312.615966796875 5.351829528808594 240 180","nodes":7,"edges":10},"reset":{"viewBox":"120 0 320 468","nodes":7,"edges":10},"circleBefore":{"cx":null,"x":"272"},"circleAfter":{"cx":null,"x":"344.615966796875"},"dragged":true,"continuousMs":423} |
| 6 | Evidence drill-down | PASS | drawer contains Effect evidence × collaboration.effect.complete { "before": { "task_status": "running" }, "after": { "task_status": "completed" } } pm.audit_recorded 9/5/2026, 3:26:33 AM user:user-7754d3f0 { "actor |
| 7 | LOD/cluster/truncation prompt | PASS | api_lod=cluster; clusters=2; api_truncated=true; ui_lod_notice=1; ui_load_more=0; ui_show_full=1; explicit_lod_text=true |
| 8 | Real-scale first interactive and continuous interactions | PASS | {"first_graph_interactive_ms":1,"continuous_interaction_ms":423} |
| 9 | Contrast/readability and overlap | PASS | {"pass":true,"method":"visual screenshot inspection plus DOM graph density","note":"Primary graph viewport remained readable in the captured screenshot."} |
| 10 | Project_id API scoping and fail-closed errors | PASS | {"slug":"org-e6824511","projectID":"project-1a8227f5","foreignProjectID":"project-802a018c","valid":{"status":200,"nodes":120,"edges":256,"effects":2},"badProject":{"status":404,"body":"{\"error\":\"not_found\",\"message\":\"project not found in this organization\"}\n"},"badCursor":{"status":400,"body":"{\"error\":\"invalid_cursor\",\"message\":\"collaboration insight: invalid cursor\"}\n"},"unauth":{"status":401,"body":"{\"error\":\"unauthenticated\",\"message\":\"no session cookie\"}\n"},"foreignProjectInOwnerOrg":{"status":404,"nodes":null,"edges":null,"effects":null,"body":"{\"error\":\"not_found\",\"message\":\"project not found in this organization\"}\n"}} |

## Quality Gates
| Gate | Result | Exit | Log | Command / Finding |
|---|---:|---:|---|---|
| origin_main_lint | BASELINE_FAIL | 2 | evidence/t2181/main_lint_rerun.log | web/src/components/WorkItemFilterBar.tsx:352:13 no-restricted-syntax checkbox rule |
| candidate_lint | BASELINE_FAIL_NO_NEW_FAILURE | 2 | evidence/t2181/candidate_lint.log | same WorkItemFilterBar.tsx:352:13 failure as origin/main; no added candidate lint failure observed |
| candidate_go_related_tests | PASS | 0 | evidence/t2181/candidate_quality.log | go test ./internal/observability/collaborationeffect ./internal/webconsole/api |
| candidate_web_tests | PASS | 0 | evidence/t2181/candidate_quality.log | pnpm --dir web test -- InsightCollaboration AppLayout.insightnav AppLayout.mobilenav App.test |
| candidate_typecheck | PASS | 0 | evidence/t2181/candidate_quality.log | pnpm --dir web exec tsc -b --force |
| candidate_web_build | PASS | 0 | evidence/t2181/candidate_quality.log | pnpm --dir web run build |
| candidate_make_build | PASS | 0 | evidence/t2181/candidate_quality.log | make build |

## Raw Evidence
- screenshots, video, HAR, API captures, browser console/network, and server log: `docs/acceptance/t2181-fresh-main-combined/evidence/`
- independent command logs, diff, graph summary: `evidence/t2181/`
- canonical structured verdict: `docs/acceptance/t2181-fresh-main-combined/evidence/verdict.json`
