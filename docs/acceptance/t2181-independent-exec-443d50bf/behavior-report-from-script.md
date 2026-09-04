# T2181 Fresh-Main Combined Collaboration Graph Acceptance

Verdict: **PASS**

## Provenance
- candidate_sha: 6074ea621f178889bcfca5adca8cf27c3ad174da
- install_root: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2181-combined-kbYLu7
- config_path: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2181-combined-kbYLu7/config.yaml
- web_origin: http://127.0.0.1:65154
- grpc_port: 65155
- admin_tcp_port: 65156
- database: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2181-combined-kbYLu7/agent-center.db
- duckdb: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2181-combined-kbYLu7/agent-center.duckdb
- runtime_identity: AGENT_CENTER_INVOCATION_ID=t2181-combined-ui
- cookie_namespace: ac_session@127.0.0.1
- fixture_provenance: Fresh isolated instance; data seeded via authenticated production HTTP endpoints only.
- organization_id: organization-5c4a3920
- organization_slug: org-dea95ad1
- worker_id: worker-905d88f3
- worker_bootstrap_host: 127.0.0.1:65156
- worker_fingerprint: sha256:B5:3B:BD:33:98:AB:B0:32:BA:55:D1:3F:8E:E9:C5:11:38:FA:58:F3:2B:BA:F6:D9:5F:D8:27:5E:E6:0B:14:CE
- runtime_catalog_revision: 1
- agent_refs: agent:agent-93585dc9, agent:agent-5ccab799
- project_id: project-108157c6
- plan_id: plan-5f1c5dc2

## Hard Gates
| # | Gate | Verdict | Evidence | Shortest Repro |
|---|---|---|---|---|
| 1 | Authenticated navigation and direct URL refresh | PASS | nav_link=true; org_url=http://127.0.0.1:65154/organizations/org-dea95ad1/insights/collaboration; unexpected401=0; import_errors=0 | Sign in as the seeded owner, land on /organizations/{slug}/projects, open the Insight rail, and look for a Collaboration effects navigation link. |
| 2 | No-query first screen non-empty organization graph | PASS | url=http://127.0.0.1:65154/organizations/org-dea95ad1/insights/collaboration; svg_nodes=4; svg_edges=6; api_nodes=120; api_edges=256 |  |
| 3 | Agent-Agent, Agent-Task, Agent-Plan, Plan-Task same screen | PASS | agent_agent=true; agent_task=true; agent_plan=true; plan_task=true; effects=2; edge_text_sample=plan task · Neutral · strength 1 · 84 effects · evidence 0 \| agent task · Neutral · strength 1 · 84 effects · evidence 0 \| agent plan · Neutral · strength 1 · 1 effects · evidence 0 \| Complete · Positive · strength 2 · 1 effects · evidence 1 · 9/5/2026, 5:04:28 AM \| Dependency release · Positive · strength 3 · 1 effects · evidence 2 · 9/5/2026, 5:04:28 AM |  |
| 4 | Search/filter and clear restores full graph | PASS | filtered=http://127.0.0.1:65154/organizations/org-dea95ad1/insights/collaboration?project_id=project-108157c6; cleared=http://127.0.0.1:65154/organizations/org-dea95ad1/insights/collaboration |  |
| 5 | Zoom/pan/drag/Fit/Reset/focus/restore interactions | PASS | {"pass":true,"before":{"viewBox":"120 0 320 304","nodes":4,"edges":6},"zoomIn":{"viewBox":"148.8 27.360000000000014 262.4 249.27999999999997","nodes":4,"edges":6},"zoomOut":{"viewBox":"125.18400000000003 4.924800000000033 309.63199999999995 294.15039999999993","nodes":4,"edges":6},"resetForWheel":{"viewBox":"120 0 320 304","nodes":4,"edges":6},"wheel":{"viewBox":"156.0619135253906 34.28960273437501 247.80800000000002 235.4176","nodes":4,"edges":6},"pan":{"viewBox":"158.82472229003906 -104.66074752807617 281.17527770996094 408.6607475280762","nodes":4,"edges":6},"fit":{"viewBox":"120 0 392.617919921875 304","nodes":4,"edges":6},"focus":{"viewBox":"312.617919921875 5.352790832519531 240 180","nodes":4,"edges":6},"reset":{"viewBox":"120 0 320 304","nodes":4,"edges":6},"circleBefore":{"cx":null,"x":"272"},"circleAfter":{"cx":null,"x":"344.617919921875"},"dragged":true,"continuousMs":397} | Open /organizations/{slug}/insights/collaboration, wheel over the SVG canvas, and compare the SVG viewBox before/after. |
| 6 | Evidence drill-down | PASS | drawer contains Effect evidence × collaboration.effect.complete { "before": { "task_status": "running" }, "after": { "task_status": "completed" } } pm.audit_recorded 9/5/2026, 5:04:28 AM user:user-f77d64e8 { "actor |  |
| 7 | LOD/cluster/truncation prompt | PASS | api_lod=cluster; clusters=1; api_truncated=true; ui_lod_notice=1; ui_load_more=0; ui_show_full=1; explicit_lod_text=true | Seed a >100-node organization graph, verify the real API reports lod=cluster/truncated, then open the UI and look for load-more or truncation guidance. |
| 8 | Real-scale first interactive and continuous interactions | PASS | {"first_graph_interactive_ms":1,"continuous_interaction_ms":397} |  |
| 9 | Contrast/readability and overlap | PASS | {"pass":true,"method":"visual screenshot inspection plus DOM graph density","note":"Primary graph viewport remained readable in the captured screenshot."} | Open the no-query organization graph with the captured large fixture and inspect the first viewport in evidence/02-organization-graph.png. |
| 10 | Project_id API scoping and fail-closed errors | PASS | {"slug":"org-dea95ad1","projectID":"project-108157c6","foreignProjectID":"project-cb227815","valid":{"status":200,"nodes":120,"edges":256,"effects":2},"badProject":{"status":404,"body":"{\"error\":\"not_found\",\"message\":\"project not found in this organization\"}\n"},"badCursor":{"status":400,"body":"{\"error\":\"invalid_cursor\",\"message\":\"collaboration insight: invalid cursor\"}\n"},"unauth":{"status":401,"body":"{\"error\":\"unauthenticated\",\"message\":\"no session cookie\"}\n"},"foreignProjectInOwnerOrg":{"status":404,"nodes":null,"edges":null,"effects":null,"body":"{\"error\":\"not_found\",\"message\":\"project not found in this organization\"}\n"}} | Call the production HTTP API with valid project_id, foreign project_id, no auth, malformed project_id, and malformed cursor; none may produce HTTP 503. |

## Raw Evidence
- `evidence/01-authenticated-projects.png`
- `evidence/02-organization-graph.png`
- `evidence/03-direct-refresh.png`
- `evidence/04-filtered-project.png`
- `evidence/05-agent-focus.png`
- `evidence/06-evidence-drawer.png`
- `evidence/network.har`, `evidence/network.json`, `evidence/console.json`, `evidence/server.log`
- `evidence/api-org-graph.json`, `evidence/api-lod-cluster.json`, `evidence/api-evidence.json`, `evidence/api-project-filter-probes.json`, `evidence/verdict.json`
- `evidence/make-build.log`, `evidence/go-focused-tests.log`

## Performance
```json
{
  "first_graph_interactive_ms": 1,
  "continuous_interaction_ms": 397
}
```
