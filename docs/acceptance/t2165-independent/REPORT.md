# T2165 Independent Collaboration Graph UI Acceptance

Verdict: **REJECT**

## Provenance
- candidate_sha: 35d932edfe9293c3f0198aa65608af94a4bd1f73
- install_root: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2165-independent-mPvu9A
- config_path: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2165-independent-mPvu9A/config.yaml
- web_origin: http://127.0.0.1:53951
- grpc_port: 53952
- admin_tcp_port: 53953
- database: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2165-independent-mPvu9A/agent-center.db
- duckdb: /var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/t2165-independent-mPvu9A/agent-center.duckdb
- runtime_identity: AGENT_CENTER_INVOCATION_ID=t2165-independent-ui
- cookie_namespace: ac_session@127.0.0.1
- fixture_provenance: Fresh isolated instance; data seeded via authenticated production HTTP endpoints only.
- organization_id: organization-c111866d
- organization_slug: org-e4d95ceb
- worker_id: worker-d97e4897
- worker_bootstrap_host: 127.0.0.1:53953
- worker_fingerprint: sha256:34:6F:7B:C6:6F:65:28:B3:88:68:F0:3C:64:01:9F:82:26:5C:3D:F1:46:FA:2F:C3:4C:D4:CB:28:A2:9F:AE:1E
- runtime_catalog_revision: 1
- agent_refs: agent:agent-3fe7898d, agent:agent-84c17812
- project_id: project-71c415a5
- plan_id: plan-896986b7

## Hard Gates
| # | Gate | Verdict | Evidence | Shortest Repro |
|---|---|---|---|---|
| 1 | Authenticated navigation and direct URL refresh | FAIL | nav_link=false; org_url=http://127.0.0.1:53951/organizations/org-e4d95ceb/insights/collaboration; unexpected401=0; import_errors=0 | Sign in as the seeded owner, land on /organizations/{slug}/projects, open the Insight rail, and look for a Collaboration effects navigation link. |
| 2 | No-query first screen non-empty organization graph | PASS | url=http://127.0.0.1:53951/organizations/org-e4d95ceb/insights/collaboration; svg_nodes=117; svg_edges=250; api_nodes=116; api_edges=248 |  |
| 3 | Agent-Agent, Agent-Task, Agent-Plan, Plan-Task same screen | PASS | agent_agent=true; agent_task=true; agent_plan=true; plan_task=true; effects=2; edge_text_sample=Reassign · Mixed · strength 2 · 1 effects · evidence 1 · 9/5/2026, 12:46:40 AM \| Reassign · Mixed · strength 2 · 1 effects · evidence 1 · 9/5/2026, 12:46:40 AM \| Dependency release · Positive · strength 3 · 1 effects · evidence 2 · 9/5/2026, 12:46:40 AM \| Complete · Positive · strength 2 · 1 effects · evidence 1 · 9/5/2026, 12:46:40 AM |  |
| 4 | Search/filter and clear restores full graph | PASS | filtered=http://127.0.0.1:53951/organizations/org-e4d95ceb/insights/collaboration?project_id=project-71c415a5; cleared=http://127.0.0.1:53951/organizations/org-e4d95ceb/insights/collaboration |  |
| 5 | Zoom/pan/drag/Fit/Reset/focus/restore interactions | FAIL | {"pass":false,"before":{"viewBox":"-15 0 755 7688","nodes":117,"edges":250},"zoomIn":{"viewBox":"52.950000000000045 3144 619.0999999999999 1400","nodes":117,"edges":250},"zoomOut":{"viewBox":"-2.7689999999999486 3144 730.5379999999999 1400","nodes":117,"edges":250},"resetForWheel":{"viewBox":"-15 0 755 7688","nodes":117,"edges":250},"wheel":{"viewBox":"-15 0 755 7688","nodes":117,"edges":250},"pan":{"viewBox":"-86.1592836946277 -28.512189327167683 755 7688","nodes":117,"edges":250},"fit":{"viewBox":"-15 0 755 7688","nodes":117,"edges":250},"focus":{"viewBox":"2.0349502563476562 4.952796936035156 240 180","nodes":117,"edges":250},"reset":{"viewBox":"-15 0 755 7688","nodes":117,"edges":250},"circleBefore":{"cx":"65","x":null},"circleAfter":{"cx":"122.03495025634766","x":null},"dragged":true,"continuousMs":518} | Open /organizations/{slug}/insights/collaboration, wheel over the SVG canvas, and compare the SVG viewBox before/after. |
| 6 | Evidence drill-down | PASS | drawer contains Effect evidence × collaboration.effect.reassign { "before": { "assignee": "agent:agent-84c17812" }, "after": { "assignee": "agent:agent-3fe7898d" } } pm.audit_recorded 9/5/2026, 12:46:40 AM user:user-5 |  |
| 7 | LOD/cluster/truncation prompt | FAIL | api_lod=cluster; clusters=1; api_truncated=true; ui_load_more=0; explicit_truncation_text=false | Seed a >100-node organization graph, verify the real API reports lod=cluster/truncated, then open the UI and look for load-more or truncation guidance. |
| 8 | Real-scale first interactive and continuous interactions | PASS | {"first_graph_interactive_ms":103,"continuous_interaction_ms":518} |  |
| 9 | Contrast/readability and overlap | FAIL | {"pass":false,"method":"visual screenshot inspection plus DOM graph density","note":"Captured first screen renders 117 SVG nodes and 250 SVG edges at once; labels/edges visibly overlap and node labels have poor contrast in evidence/02-organization-graph.png."} | Open the no-query organization graph with the captured large fixture and inspect the first viewport in evidence/02-organization-graph.png. |

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
  "first_graph_interactive_ms": 103,
  "continuous_interaction_ms": 518
}
```
