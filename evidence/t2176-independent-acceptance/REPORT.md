# T2176 Independent Collaboration Graph UI Acceptance

Verdict: **REJECT**

## Provenance
- candidate_sha: 1e14098b213d97087c3aa2fbda9a90f065c74a3e
- install_root: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2176-independent-h7n0mI
- config_path: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2176-independent-h7n0mI/config.yaml
- web_origin: http://127.0.0.1:57719
- grpc_port: 57720
- admin_tcp_port: 57721
- database: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2176-independent-h7n0mI/agent-center.db
- duckdb: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2176-independent-h7n0mI/agent-center.duckdb
- runtime_identity: AGENT_CENTER_INVOCATION_ID=t2176-independent-ui
- cookie_namespace: ac_session@127.0.0.1
- fixture_provenance: Fresh isolated instance; data seeded via authenticated production HTTP endpoints only.
- organization_id: organization-ee50e3d0
- organization_slug: org-fe22cb50
- worker_id: worker-08f3eb7d
- worker_bootstrap_host: 127.0.0.1:57721
- worker_fingerprint: sha256:BB:8A:96:7D:14:4B:05:45:55:CE:9E:81:D5:F7:D7:F8:D2:25:16:D7:EB:82:3A:7F:2C:A0:EE:18:71:4E:CF:42
- runtime_catalog_revision: 1
- agent_refs: agent:agent-921fac57, agent:agent-b2398ace
- project_id: project-c91a7aeb
- plan_id: plan-f224cbd0

## Hard Gates
| # | Gate | Verdict | Evidence | Shortest Repro |
|---|---|---|---|---|
| 1 | Authenticated navigation and direct URL refresh | PASS | nav_link=true; org_url=http://127.0.0.1:57719/organizations/org-fe22cb50/insights/collaboration; unexpected401=0; import_errors=0 | Sign in as the seeded owner, land on /organizations/{slug}/projects, open the Insight rail, and look for a Collaboration effects navigation link. |
| 2 | No-query first screen non-empty 117+/250+ organization graph | PASS | url=http://127.0.0.1:57719/organizations/org-fe22cb50/insights/collaboration; svg_nodes=4; svg_edges=6; api_nodes=118; api_edges=252 |  |
| 3 | Agent-Agent, Agent-Task, Agent-Plan, Plan-Task same screen | PASS | agent_agent=true; agent_task=true; agent_plan=true; plan_task=true; effects=2; edge_text_sample=plan task · Neutral · strength 1 · 84 effects · evidence 0 \| agent task · Neutral · strength 1 · 84 effects · evidence 0 \| agent plan · Neutral · strength 1 · 1 effects · evidence 0 \| Dependency release · Positive · strength 3 · 1 effects · evidence 2 · 9/5/2026, 2:30:23 AM \| Complete · Positive · strength 2 · 1 effects · evidence 1 · 9/5/2026, 2:30:23 AM |  |
| 4 | Search/filter and clear restores full graph without 503 | FAIL | filtered=http://127.0.0.1:57719/organizations/org-fe22cb50/insights/collaboration?project_id=project-c91a7aeb; cleared=http://127.0.0.1:57719/organizations/org-fe22cb50/insights/collaboration; project_filter_5xx=1; first_5xx=503 http://127.0.0.1:57719/api/orgs/org-fe22cb50/insights/collaboration-effects?limit=100&project_id=project-c91a7aeb | Sign in, open /organizations/org-fe22cb50/insights/collaboration, choose project project-c91a7aeb; observe 503 on /api/orgs/org-fe22cb50/insights/collaboration-effects?limit=100&project_id=project-c91a7aeb. |
| 5 | Zoom/pan/drag/Fit/Reset/focus/restore interactions | PASS | {"pass":true,"before":{"viewBox":"120 0 320 304","nodes":4,"edges":6},"zoomIn":{"viewBox":"148.8 27.360000000000014 262.4 249.27999999999997","nodes":4,"edges":6},"zoomOut":{"viewBox":"125.18400000000003 4.924800000000033 309.63199999999995 294.15039999999993","nodes":4,"edges":6},"resetForWheel":{"viewBox":"120 0 320 304","nodes":4,"edges":6},"wheel":{"viewBox":"156.0619135253906 34.28960273437501 247.80800000000002 235.4176","nodes":4,"edges":6},"pan":{"viewBox":"158.82472229003906 -104.66074752807617 281.17527770996094 408.6607475280762","nodes":4,"edges":6},"fit":{"viewBox":"120 0 392.617919921875 304","nodes":4,"edges":6},"focus":{"viewBox":"312.617919921875 5.352790832519531 240 180","nodes":4,"edges":6},"reset":{"viewBox":"120 0 320 304","nodes":4,"edges":6},"circleBefore":{"cx":null,"x":"272"},"circleAfter":{"cx":null,"x":"344.617919921875"},"dragged":true,"continuousMs":398} | Open /organizations/{slug}/insights/collaboration, wheel over the SVG canvas, and compare the SVG viewBox before/after. |
| 6 | Evidence drill-down | PASS | drawer contains Effect evidence × collaboration.effect.dependency_release { "before": { "downstream_task_id": "task-ac93af21", "upstream_task_status": "running" }, "after": { "downstream_task_id": "task-ac93af21",  |  |
| 7 | LOD/cluster/truncation prompt | PASS | api_lod=cluster; clusters=1; api_truncated=true; ui_lod_notice=1; ui_load_more=0; ui_show_full=1; explicit_lod_text=true | Seed a >100-node organization graph, verify the real API reports lod=cluster/truncated, then open the UI and look for load-more or truncation guidance. |
| 8 | Real-scale first interactive and continuous interactions | PASS | {"first_graph_interactive_ms":3,"continuous_interaction_ms":398} |  |
| 9 | Contrast/readability and overlap | PASS | {"pass":true,"method":"visual screenshot inspection plus DOM graph density","note":"Primary graph viewport remained readable in the captured screenshot."} | Open the no-query organization graph with the captured large fixture and inspect the first viewport in evidence/02-organization-graph.png. |

## Raw Evidence
- `raw/01-authenticated-projects.png`
- `raw/02-organization-graph.png`
- `raw/03-direct-refresh.png`
- `raw/04-filtered-project.png`
- `raw/05-agent-focus.png`
- `raw/06-evidence-drawer.png`
- `raw/network.har`, `raw/network.json`, `raw/console.json`, `raw/server.log`
- `raw/api-org-graph.json`, `raw/api-lod-cluster.json`, `raw/api-evidence.json`, `raw/verdict.json`
- `build.log`, `e2e-pnpm-install.log`, `acceptance-run.log`

## Performance
```json
{
  "first_graph_interactive_ms": 3,
  "continuous_interaction_ms": 398
}
```
