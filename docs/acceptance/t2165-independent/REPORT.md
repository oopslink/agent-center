# T2165 Independent Collaboration Graph UI Acceptance

Verdict: **PASS**

## Provenance
- candidate_sha: working-tree-t2175
- install_root: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2165-independent-S9MvEd
- config_path: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2165-independent-S9MvEd/config.yaml
- web_origin: http://127.0.0.1:53940
- grpc_port: 53941
- admin_tcp_port: 53942
- database: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2165-independent-S9MvEd/agent-center.db
- duckdb: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2165-independent-S9MvEd/agent-center.duckdb
- runtime_identity: AGENT_CENTER_INVOCATION_ID=t2165-independent-ui
- cookie_namespace: ac_session@127.0.0.1
- fixture_provenance: Fresh isolated instance; data seeded via authenticated production HTTP endpoints only.
- organization_id: organization-66f81ed2
- organization_slug: org-19fffc0e
- worker_id: worker-bf52944d
- worker_bootstrap_host: 127.0.0.1:53942
- worker_fingerprint: sha256:F7:45:52:E8:B0:80:E8:8A:24:62:64:35:94:D1:AB:E1:A7:C7:32:3B:C8:58:8B:34:B0:2A:B2:01:5D:B1:75:47
- runtime_catalog_revision: 1
- agent_refs: agent:agent-b6e8247f, agent:agent-d910ab49
- project_id: project-e9b69876
- plan_id: plan-c7705000

## Hard Gates
| # | Gate | Verdict | Evidence | Shortest Repro |
|---|---|---|---|---|
| 1 | Authenticated navigation and direct URL refresh | PASS | nav_link=true; org_url=http://127.0.0.1:53940/organizations/org-19fffc0e/insights/collaboration; unexpected401=0; import_errors=0 | Sign in as the seeded owner, land on /organizations/{slug}/projects, open the Insight rail, and look for a Collaboration effects navigation link. |
| 2 | No-query first screen non-empty organization graph | PASS | url=http://127.0.0.1:53940/organizations/org-19fffc0e/insights/collaboration; svg_nodes=4; svg_edges=6; api_nodes=117; api_edges=250 |  |
| 3 | Agent-Agent, Agent-Task, Agent-Plan, Plan-Task same screen | PASS | agent_agent=true; agent_task=true; agent_plan=true; plan_task=true; effects=4; edge_text_sample=plan task · Neutral · strength 1 · 84 effects · evidence 0 \| agent task · Neutral · strength 1 · 84 effects · evidence 0 \| agent plan · Neutral · strength 1 · 1 effects · evidence 0 \| Dependency release · Positive · strength 3 · 1 effects · evidence 2 · 9/5/2026, 2:19:23 AM \| Complete · Positive · strength 2 · 1 effects · evidence 1 · 9/5/2026, 2:19:23 AM |  |
| 4 | Search/filter and clear restores full graph | PASS | filtered=http://127.0.0.1:53940/organizations/org-19fffc0e/insights/collaboration?project_id=project-e9b69876; cleared=http://127.0.0.1:53940/organizations/org-19fffc0e/insights/collaboration |  |
| 5 | Zoom/pan/drag/Fit/Reset/focus/restore interactions | PASS | {"pass":true,"before":{"viewBox":"120 0 320 304","nodes":4,"edges":6},"zoomIn":{"viewBox":"148.8 27.360000000000014 262.4 249.27999999999997","nodes":4,"edges":6},"zoomOut":{"viewBox":"125.18400000000003 4.924800000000033 309.63199999999995 294.15039999999993","nodes":4,"edges":6},"resetForWheel":{"viewBox":"120 0 320 304","nodes":4,"edges":6},"wheel":{"viewBox":"156.0619135253906 34.28960273437501 247.80800000000002 235.4176","nodes":4,"edges":6},"pan":{"viewBox":"158.82472229003906 -104.66074752807617 281.17527770996094 408.6607475280762","nodes":4,"edges":6},"fit":{"viewBox":"120 0 392.617919921875 304","nodes":4,"edges":6},"focus":{"viewBox":"312.617919921875 5.352790832519531 240 180","nodes":4,"edges":6},"reset":{"viewBox":"120 0 320 304","nodes":4,"edges":6},"circleBefore":{"cx":null,"x":"272"},"circleAfter":{"cx":null,"x":"344.617919921875"},"dragged":true,"continuousMs":431} | Open /organizations/{slug}/insights/collaboration, wheel over the SVG canvas, and compare the SVG viewBox before/after. |
| 6 | Evidence drill-down | PASS | drawer contains Effect evidence × collaboration.effect.dependency_release { "before": { "downstream_task_id": "task-02908e8a", "upstream_task_status": "running" }, "after": { "downstream_task_id": "task-02908e8a",  |  |
| 7 | LOD/cluster/truncation prompt | PASS | api_lod=cluster; clusters=1; api_truncated=true; ui_lod_notice=1; ui_load_more=0; ui_show_full=1; explicit_lod_text=true | Seed a >100-node organization graph, verify the real API reports lod=cluster/truncated, then open the UI and look for load-more or truncation guidance. |
| 8 | Real-scale first interactive and continuous interactions | PASS | {"first_graph_interactive_ms":0,"continuous_interaction_ms":431} |  |
| 9 | Contrast/readability and overlap | PASS | {"pass":true,"method":"visual screenshot inspection plus DOM graph density","note":"Primary graph viewport remained readable in the captured screenshot."} | Open the no-query organization graph with the captured large fixture and inspect the first viewport in evidence/02-organization-graph.png. |

## Raw Evidence
- `evidence/01-authenticated-projects.png`
- `evidence/02-organization-graph.png`
- `evidence/03-direct-refresh.png`
- `evidence/04-filtered-project.png`
- `evidence/05-agent-focus.png`
- `evidence/06-evidence-drawer.png`
- `evidence/network.har`, `evidence/network.json`, `evidence/console.json`, `evidence/server.log`
- `evidence/api-org-graph.json`, `evidence/api-lod-cluster.json`, `evidence/api-evidence.json`, `evidence/verdict.json`
- `evidence/make-build.log`, `evidence/go-focused-tests.log`

## Performance
```json
{
  "first_graph_interactive_ms": 0,
  "continuous_interaction_ms": 431
}
```
