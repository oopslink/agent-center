# T2181 Independent Acceptance

Product verdict: **PASS**

## Candidate
- verified SHA: 6074ea621f178889bcfca5adca8cf27c3ad174da
- merge-base(origin/main): ddfb87d47e8253fb2e4f22d014f497301fa4b7ec
- graph: fresh-main single commit; no main merge performed here.

## Coverage Matrix
- T2176 formal navigation: PASS — Insight module default path and secondary/mobile nav expose Collaboration effects; authenticated click and URL refresh loaded without unexpected 401/import errors.
- T2176 real wheel and viewport controls: PASS — SVG wheel listener, +, -, Fit, Reset, pan, node drag, keyboard focus and reset changed viewBox/position in browser evidence.
- T2176 LOD/truncated feedback: PASS — Cluster LOD and truncation are represented in API normalization and UI notice with Show full graph action.
- T2176 large-scale readability: PASS — Real fixture produced 120 API nodes/256 edges and clustered first screen remained readable with interaction timings under 3s; visual note records minor edge-label proximity but no blocking overlap.
- T2179 project_id 503 fix: PASS — Graph reader applies project/org scoped structure queries; HTTP/API probes return valid 200, bad cursor 400, missing/cross-org 404, unauth 401, and no 503.

## Hard Gates
- hard_gate_1: PASS — Authenticated navigation and direct URL refresh; nav_link=true; org_url=http://127.0.0.1:62184/organizations/org-27a26777/insights/collaboration; unexpected401=0; import_errors=0
- hard_gate_2: PASS — No-query first screen non-empty organization graph; url=http://127.0.0.1:62184/organizations/org-27a26777/insights/collaboration; svg_nodes=4; svg_edges=6; api_nodes=120; api_edges=256
- hard_gate_3: PASS — Agent-Agent, Agent-Task, Agent-Plan, Plan-Task same screen; agent_agent=true; agent_task=true; agent_plan=true; plan_task=true; effects=2; edge_text_sample=plan task · Neutral · strength 1 · 84 effects · evidence 0 | agent task · Neutral · strength 1 · 84 effects · evidence 0 | agent plan · Neutral · strength 1 · 1 effects · evidence 0 | Dependency release · Positive · strength 3 · 1 effects · evidence 2 · 9/5/2026, 4:06:32 AM | Complete · Positive · strength 2 · 1 effects · evidence 1 · 9/5/2026, 4:06:32 AM
- hard_gate_4: PASS — Search/filter and clear restores full graph; filtered=http://127.0.0.1:62184/organizations/org-27a26777/insights/collaboration?project_id=project-048db05c; cleared=http://127.0.0.1:62184/organizations/org-27a26777/insights/collaboration
- hard_gate_5: PASS — Zoom/pan/drag/Fit/Reset/focus/restore interactions; {"pass":true,"before":{"viewBox":"120 0 320 304","nodes":4,"edges":6},"zoomIn":{"viewBox":"148.8 27.360000000000014 262.4 249.27999999999997","nodes":4,"edges":6},"zoomOut":{"viewBox":"125.18400000000003 4.924800000000033 309.63199999999995 294.15039999999993","nodes":4,"edges":6},"resetForWheel":{"viewBox":"120 0 320 304","nodes":4,"edges":6},"wheel":{"viewBox":"156.0619135253906 34.28960273437501 247.80800000000002 235.4176","nodes":4,"edges":6},"pan":{"viewBox":"158.82472229003906 -104.66074752807617 281.17527770996094 408.6607475280762","nodes":4,"edges":6},"fit":{"viewBox":"120 0 392.617919921875 304","nodes":4,"edges":6},"focus":{"viewBox":"312.617919921875 5.352790832519531 240 180","nodes":4,"edges":6},"reset":{"viewBox":"120 0 320 304","nodes":4,"edges":6},"circleBefore":{"cx":null,"x":"272"},"circleAfter":{"cx":null,"x":"344.617919921875"},"dragged":true,"continuousMs":448}
- hard_gate_6: PASS — Evidence drill-down; drawer contains Effect evidence × collaboration.effect.dependency_release { "before": { "downstream_task_id": "task-6299759e", "upstream_task_status": "running" }, "after": { "downstream_task_id": "task-6299759e", 
- hard_gate_7: PASS — LOD/cluster/truncation prompt; api_lod=cluster; clusters=1; api_truncated=true; ui_lod_notice=1; ui_load_more=0; ui_show_full=1; explicit_lod_text=true
- hard_gate_8: PASS — Real-scale first interactive and continuous interactions; {"first_graph_interactive_ms":0,"continuous_interaction_ms":448}
- hard_gate_9: PASS — Contrast/readability and overlap; {"pass":true,"method":"visual screenshot inspection plus DOM graph density","note":"Primary graph viewport remained readable in the captured screenshot."}
- hard_gate_10: PASS — Project_id API scoping and fail-closed errors; {"slug":"org-27a26777","projectID":"project-048db05c","foreignProjectID":"project-05c33f60","valid":{"status":200,"nodes":120,"edges":256,"effects":2},"badProject":{"status":404,"body":"{\"error\":\"not_found\",\"message\":\"project not found in this organization\"}\n"},"badCursor":{"status":400,"body":"{\"error\":\"invalid_cursor\",\"message\":\"collaboration insight: invalid cursor\"}\n"},"unauth":{"status":401,"body":"{\"error\":\"unauthenticated\",\"message\":\"no session cookie\"}\n"},"foreignProjectInOwnerOrg":{"status":404,"nodes":null,"edges":null,"effects":null,"body":"{\"error\":\"not_found\",\"message\":\"project not found in this organization\"}\n"}}

## Quality Gates
- fresh origin/main make lint: BASELINE_FAIL; pnpm_install_exit:0
make_lint_exit:2; Existing web/src/components/WorkItemFilterBar.tsx checkbox eslint rule.
- candidate make lint: BASELINE_FAIL_NOT_NEW; make_lint_exit:2; Same file/rule as fresh origin/main; no candidate-added lint failure.
- candidate web typecheck: PASS; web_typecheck_exit:0
- candidate related Go tests: PASS; go_related_exit:0
- candidate related Web tests: PASS; web_related_vitest_exit:0
- candidate make build: PASS; make_build_exit:0
- isolated production-like acceptance: PASS; isolated_acceptance_rerun_exit:0

## Evidence
- machine verdict: `docs/acceptance/t2181-independent/verdict.json`
- screenshots/HAR/video/API/server logs: `docs/acceptance/t2181-independent/evidence/`
- raw command logs and selected diff: `docs/acceptance/t2181-independent/raw/`

## Task Input Note
task-input/v1 describes T1850, while this executor contract explicitly fixes T2181 candidate 6074ea621f178889bcfca5adca8cf27c3ad174da; acceptance used the explicit current contract.
