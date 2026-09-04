# T2181 Fresh-Main Combined Collaboration Graph Acceptance

Verdict: **PASS**

## Provenance
- candidate_sha: 6074ea621f178889bcfca5adca8cf27c3ad174da
- install_root: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2181-combined-HC7pLp
- config_path: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2181-combined-HC7pLp/config.yaml
- web_origin: http://127.0.0.1:62184
- grpc_port: 62185
- admin_tcp_port: 62186
- database: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2181-combined-HC7pLp/agent-center.db
- duckdb: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2181-combined-HC7pLp/agent-center.duckdb
- runtime_identity: AGENT_CENTER_INVOCATION_ID=t2181-combined-ui
- cookie_namespace: ac_session@127.0.0.1
- fixture_provenance: Fresh isolated instance; data seeded via authenticated production HTTP endpoints only.
- organization_id: organization-477a1288
- organization_slug: org-27a26777
- worker_id: worker-496125a3
- worker_bootstrap_host: 127.0.0.1:62186
- worker_fingerprint: sha256:DD:68:DE:85:AA:21:94:ED:A3:92:A9:56:CD:DD:3E:EB:EB:A7:6F:46:DA:8D:9B:E1:39:A9:65:78:40:BD:E5:64
- runtime_catalog_revision: 1
- agent_refs: agent:agent-4e25bea7, agent:agent-85ab2575
- project_id: project-048db05c
- plan_id: plan-8e175823

## Hard Gates
| # | Gate | Verdict | Evidence | Shortest Repro |
|---|---|---|---|---|
| 1 | Authenticated navigation and direct URL refresh | PASS | nav_link=true; org_url=http://127.0.0.1:62184/organizations/org-27a26777/insights/collaboration; unexpected401=0; import_errors=0 | Sign in as the seeded owner, land on /organizations/{slug}/projects, open the Insight rail, and look for a Collaboration effects navigation link. |
| 2 | No-query first screen non-empty organization graph | PASS | url=http://127.0.0.1:62184/organizations/org-27a26777/insights/collaboration; svg_nodes=4; svg_edges=6; api_nodes=120; api_edges=256 |  |
| 3 | Agent-Agent, Agent-Task, Agent-Plan, Plan-Task same screen | PASS | agent_agent=true; agent_task=true; agent_plan=true; plan_task=true; effects=2; edge_text_sample=plan task · Neutral · strength 1 · 84 effects · evidence 0 \| agent task · Neutral · strength 1 · 84 effects · evidence 0 \| agent plan · Neutral · strength 1 · 1 effects · evidence 0 \| Dependency release · Positive · strength 3 · 1 effects · evidence 2 · 9/5/2026, 4:06:32 AM \| Complete · Positive · strength 2 · 1 effects · evidence 1 · 9/5/2026, 4:06:32 AM |  |
| 4 | Search/filter and clear restores full graph | PASS | filtered=http://127.0.0.1:62184/organizations/org-27a26777/insights/collaboration?project_id=project-048db05c; cleared=http://127.0.0.1:62184/organizations/org-27a26777/insights/collaboration |  |
| 5 | Zoom/pan/drag/Fit/Reset/focus/restore interactions | PASS | {"pass":true,"before":{"viewBox":"120 0 320 304","nodes":4,"edges":6},"zoomIn":{"viewBox":"148.8 27.360000000000014 262.4 249.27999999999997","nodes":4,"edges":6},"zoomOut":{"viewBox":"125.18400000000003 4.924800000000033 309.63199999999995 294.15039999999993","nodes":4,"edges":6},"resetForWheel":{"viewBox":"120 0 320 304","nodes":4,"edges":6},"wheel":{"viewBox":"156.0619135253906 34.28960273437501 247.80800000000002 235.4176","nodes":4,"edges":6},"pan":{"viewBox":"158.82472229003906 -104.66074752807617 281.17527770996094 408.6607475280762","nodes":4,"edges":6},"fit":{"viewBox":"120 0 392.617919921875 304","nodes":4,"edges":6},"focus":{"viewBox":"312.617919921875 5.352790832519531 240 180","nodes":4,"edges":6},"reset":{"viewBox":"120 0 320 304","nodes":4,"edges":6},"circleBefore":{"cx":null,"x":"272"},"circleAfter":{"cx":null,"x":"344.617919921875"},"dragged":true,"continuousMs":448} | Open /organizations/{slug}/insights/collaboration, wheel over the SVG canvas, and compare the SVG viewBox before/after. |
| 6 | Evidence drill-down | PASS | drawer contains Effect evidence × collaboration.effect.dependency_release { "before": { "downstream_task_id": "task-6299759e", "upstream_task_status": "running" }, "after": { "downstream_task_id": "task-6299759e",  |  |
| 7 | LOD/cluster/truncation prompt | PASS | api_lod=cluster; clusters=1; api_truncated=true; ui_lod_notice=1; ui_load_more=0; ui_show_full=1; explicit_lod_text=true | Seed a >100-node organization graph, verify the real API reports lod=cluster/truncated, then open the UI and look for load-more or truncation guidance. |
| 8 | Real-scale first interactive and continuous interactions | PASS | {"first_graph_interactive_ms":0,"continuous_interaction_ms":448} |  |
| 9 | Contrast/readability and overlap | PASS | {"pass":true,"method":"visual screenshot inspection plus DOM graph density","note":"Primary graph viewport remained readable in the captured screenshot."} | Open the no-query organization graph with the captured large fixture and inspect the first viewport in evidence/02-organization-graph.png. |
| 10 | Project_id API scoping and fail-closed errors | PASS | {"slug":"org-27a26777","projectID":"project-048db05c","foreignProjectID":"project-05c33f60","valid":{"status":200,"nodes":120,"edges":256,"effects":2},"badProject":{"status":404,"body":"{\"error\":\"not_found\",\"message\":\"project not found in this organization\"}\n"},"badCursor":{"status":400,"body":"{\"error\":\"invalid_cursor\",\"message\":\"collaboration insight: invalid cursor\"}\n"},"unauth":{"status":401,"body":"{\"error\":\"unauthenticated\",\"message\":\"no session cookie\"}\n"},"foreignProjectInOwnerOrg":{"status":404,"nodes":null,"edges":null,"effects":null,"body":"{\"error\":\"not_found\",\"message\":\"project not found in this organization\"}\n"}} | Call the production HTTP API with valid project_id, foreign project_id, no auth, malformed project_id, and malformed cursor; none may produce HTTP 503. |

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
  "first_graph_interactive_ms": 0,
  "continuous_interaction_ms": 448
}
```
