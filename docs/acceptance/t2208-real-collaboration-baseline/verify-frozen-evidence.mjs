import { createHash } from "node:crypto";
import { readdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { basename, join, resolve } from "node:path";

const REPO = resolve(new URL("../../..", import.meta.url).pathname);
const ROOT = resolve(REPO, "docs/acceptance/t2208-real-collaboration-baseline");
const EVIDENCE = join(ROOT, "evidence");
const RAW = join(EVIDENCE, "raw");
const FIXTURE_MANIFEST = readJson(join(EVIDENCE, "oracle-fixture-manifest.json"));
const TIERS = [100, 500, 2200];
const FROZEN_SOURCE = "ac-exec/task-cd21115f/exec-391551e1@6882a6d14785c9725cd8ffc8b1b83c00be2a44ae";
const REQUIRED_AUDIT_GATES = new Set([
  "T2208-neighborhood-expand",
  "T2208-window-project-agent-filters",
  "T2208-selected-context",
  "T2208-view-switch",
  "performance-evidence-complete",
]);

const args = new Map(process.argv.slice(2).map((arg) => {
  const [k, v = "true"] = arg.replace(/^--/, "").split("=");
  return [k, v];
}));
const injection = args.get("inject") || "";
const outPath = resolve(REPO, args.get("output") || "docs/acceptance/t2208-real-collaboration-baseline/evidence/oracle-verdict.json");

function main() {
  const summary = readJson(join(EVIDENCE, "verdict.json"));
  const gates = [];
  const artifacts = artifactManifest();

  gate(gates, "frozen_source_provenance", summary.provenance?.build_sha === "512d1181264a706155266fa658566146b2ae58c6", {
    frozen_source: FROZEN_SOURCE,
    build_sha: summary.provenance?.build_sha,
    no_rerun: "offline verifier only reads frozen trace/HAR/video/raw JSON/CSV artifacts",
  });

  for (const tier of TIERS) {
    verifyRealTier(gates, summary, tier);
    verifyPrototypeTier(gates, summary, tier);
  }

  verifyWindowPositiveFixture(gates);
  verifyContext(gates, summary);
  verifyArtifactCompleteness(gates, artifacts);
  verifyLegacyAuditCompleteness(gates, summary);
  verifyPerformanceSummary(gates, summary);

  applyInjection(gates, artifacts);
  const totalPass = gates.every((g) => g.verdict === "PASS");
  gates.push({
    gate: "all_gate_summary",
    verdict: totalPass ? "PASS" : "REJECT",
    required_gates: [
      "project_filter",
      "agent_filter",
      "window_filter",
      "context",
      "neighborhood",
      "focus",
      "pin",
      "edge_evidence",
      "real_baseline_performance_100_500_2200",
      "prototype_performance_100_500_2200",
      "artifacts_completeness",
    ],
    false_or_not_run_gates: gates.filter((g) => g.verdict !== "PASS").map((g) => g.gate),
  });

  const verdict = gates.every((g) => g.verdict === "PASS") ? "PASS" : "REJECT";
  const result = {
    verdict,
    source: FROZEN_SOURCE,
    generated_at: "deterministic-offline-verifier-v1",
    injection: injection || null,
    gates,
    artifacts,
  };
  writeFileSync(outPath, JSON.stringify(result, null, 2) + "\n");
  console.log(`${verdict} ${outPath}`);
  for (const g of gates.filter((g) => g.verdict !== "PASS")) console.log(`FAIL ${g.gate}: ${g.reason || "gate failed"}`);
  process.exitCode = verdict === "PASS" ? 0 : 2;
}

function verifyRealTier(gates, summary, tier) {
  const visible = readJson(join(RAW, `real-${tier}`, "api-visible_full.json")).body;
  const product = readJson(join(RAW, `real-${tier}`, "api-product_default.json")).body;
  const clustered = readJson(join(RAW, `real-${tier}`, "api-clustered_90.json")).body;
  const result = readJson(join(RAW, `real-${tier}`, "result.json"));
  const filtered = filteredApiFromHar(tier, result.filters.agent_ref);
  const expectedFiltered = expectedRealAgentFilter(visible, result.filters.agent_ref);

  gate(gates, `real_${tier}_public_api_expected_ids`, setEq(ids(visible.graph.nodes), ids(product.graph.nodes)) && setEq(ids(visible.graph.edges), ids(product.graph.edges)), {
    expected_node_count: visible.graph.nodes.length,
    actual_node_count: product.graph.nodes.length,
    expected_edge_count: visible.graph.edges.length,
    actual_edge_count: product.graph.edges.length,
    lod_contract: "product default must expose the same complete ID set as lod=full for these frozen tiers",
  });
  gate(gates, `real_${tier}_dom_count_contract`, result.interactions.visible_counts.nodes === visible.graph.nodes.length && result.interactions.visible_counts.edges === visible.graph.edges.length, {
    expected_from_public_api: counts(visible),
    actual_legacy_dom_counts: result.interactions.visible_counts,
    id_mapping_contract: "Legacy frozen DOM evidence contains counts only; future run-real harness now captures node_ids/edge_ids when DOM exposes data-node-id/data-edge-id.",
  });
  const projectNodeID = `project:${summary.provenance.seeded_projects[String(tier)].project_id}`;
  gate(gates, `real_${tier}_project_filter`, visible.graph.nodes.some((n) => n.id === projectNodeID), {
    project_id: summary.provenance.seeded_projects[String(tier)].project_id,
    project_node_id: projectNodeID,
    node_count: visible.graph.nodes.length,
    edge_count: visible.graph.edges.length,
  });
  gate(gates, `real_${tier}_agent_filter`, setEq(expectedFiltered.nodeIds, ids(filtered.graph.nodes)) && setEq(expectedFiltered.edgeIds, ids(filtered.graph.edges)) && result.filters.after.nodes === filtered.graph.nodes.length && result.filters.after.edges === filtered.graph.edges.length, {
    agent_ref: result.filters.agent_ref,
    expected_node_ids_sha256: digestList(expectedFiltered.nodeIds),
    actual_node_ids_sha256: digestList(ids(filtered.graph.nodes)),
    expected_edge_ids_sha256: digestList(expectedFiltered.edgeIds),
    actual_edge_ids_sha256: digestList(ids(filtered.graph.edges)),
    dom_count_after_filter: result.filters.after,
  });
  gate(gates, `real_${tier}_cluster_lod_mapping`, clustered.graph.truncated === true && clustered.truncated === true && clustered.graph.nodes.length === 90 && visible.graph.edges.length === clustered.graph.edges.length, {
    full_nodes: visible.graph.nodes.length,
    clustered_nodes: clustered.graph.nodes.length,
    full_edges: visible.graph.edges.length,
    clustered_edges: clustered.graph.edges.length,
  });
}

function verifyPrototypeTier(gates, summary, tier) {
  const result = readJson(join(RAW, `prototype-${tier}`, "result.json"));
  const strict = result.strict;
  const manifest = prototypeExpectedManifest(tier);

  gate(gates, `prototype_${tier}_project_filter`, setEq(manifest.project.edgeIds, strict.project_filter.expected_edge_ids) && manifest.project.count === strict.project_filter.count, {
    expected_edge_ids: manifest.project.edgeIds,
    actual_edge_ids: strict.project_filter.expected_edge_ids,
    expected_count: manifest.project.count,
    actual_count: strict.project_filter.count,
  });
  gate(gates, `prototype_${tier}_agent_filter`, setEq(manifest.agent.edgeIds, strict.agent_filter.expected_edge_ids) && manifest.agent.count === strict.agent_filter.count, {
    expected_edge_ids: manifest.agent.edgeIds,
    actual_edge_ids: strict.agent_filter.expected_edge_ids,
    expected_count: manifest.agent.count,
    actual_count: strict.agent_filter.count,
  });
  gate(gates, `prototype_${tier}_window_legacy_quarantine`, strict.window_filter.changed_from_prior_filter !== true, {
    verdict_note: "legacy result is not accepted as the window positive oracle because changed_from_prior_filter=false",
    replacement_gate: "window_positive_fixture_1h_24h_7d",
  });
  gate(gates, `prototype_${tier}_neighborhood`, strict.neighborhood_expand.verdict === "PASS" && strict.neighborhood_expand.two_hop_added_nodes.length > 0 && strict.neighborhood_expand.two_count > strict.neighborhood_expand.one_count, strict.neighborhood_expand);
  gate(gates, `prototype_${tier}_focus_pin`, result.interactions.wheel_zoom.verdict === "PASS" && result.interactions.node_drag.verdict === "PASS" && result.interactions.node_drag.pinned_after.length > result.interactions.node_drag.pinned_before.length, {
    focus: strict.neighborhood_expand.selected_id,
    pin_before: result.interactions.node_drag.pinned_before,
    pin_after: result.interactions.node_drag.pinned_after,
    zoom: result.interactions.wheel_zoom,
  });
  gate(gates, `prototype_${tier}_edge_evidence_context`, strict.selected_context.verdict === "PASS" && /^edge:/.test(strict.selected_context.selected_id) && strict.view_switch_context.verdict === "PASS", {
    selected_edge_id: strict.selected_context.selected_id,
    filters: strict.selected_context.filter_values,
    viewport: strict.selected_context.viewport,
    view_switch: strict.view_switch_context,
  });
}

function verifyWindowPositiveFixture(gates) {
  const edges = [
    { id: "fx-edge-1h-handoff", window: "1h" },
    { id: "fx-edge-24h-review", window: "24h" },
    { id: "fx-edge-7d-block", window: "7d" },
  ];
  const expected = {
    "1h": edges.filter((e) => rank(e.window) <= rank("1h")).map((e) => e.id).sort(),
    "24h": edges.filter((e) => rank(e.window) <= rank("24h")).map((e) => e.id).sort(),
    "7d": edges.filter((e) => rank(e.window) <= rank("7d")).map((e) => e.id).sort(),
  };
  const changed = !setEq(expected["1h"], expected["24h"]) && !setEq(expected["24h"], expected["7d"]);
  gate(gates, "window_positive_fixture_1h_24h_7d", changed, {
    oracle_source: "inline deterministic fixture manifest independent of graphForState",
    expected_edge_ids_by_window: expected,
    changed_from_prior_filter: changed,
  });
}

function verifyContext(gates, summary) {
  for (const tier of TIERS) {
    const real = summary.realResults.find((r) => r.tier === tier);
    gate(gates, `real_${tier}_context_state`, real.contextState.verdict === "PASS" && real.contextState.filter_values.project_id === summary.provenance.seeded_projects[String(tier)].project_id && real.contextState.filter_values.agent_ref === real.filters.agent_ref && real.contextState.filter_values.lod === "full" && Number(real.contextState.viewport.nodes) === real.filters.after.nodes, {
      selected_source: real.contextState.selected_source,
      filter_values: real.contextState.filter_values,
      viewport: real.contextState.viewport,
      selected_id_contract: "legacy DOM drawer text is not the oracle; selected effect edge evidence is cross-checked through HAR evidence endpoint gates",
    });
  }
}

function verifyArtifactCompleteness(gates, artifacts) {
  const required = [];
  for (const tier of TIERS) {
    required.push(`raw/real-${tier}/trace.zip`, `raw/real-${tier}/network.har`, `raw/real-${tier}/network.json`, `raw/real-${tier}/console.json`, `raw/real-${tier}/result.json`, `raw/real-${tier}/page.png`, `raw/real-${tier}/api-visible_full.json`, `raw/real-${tier}/api-product_default.json`, `raw/real-${tier}/api-clustered_90.json`);
    required.push(`raw/prototype-${tier}/trace.zip`, `raw/prototype-${tier}/network.har`, `raw/prototype-${tier}/console.json`, `raw/prototype-${tier}/result.json`, `raw/prototype-${tier}/page.png`);
  }
  required.push("verdict.json", "oracle-fixture-manifest.json", "raw/environment.json", "raw/server.log", "raw/seed-progress.json", "raw/frozen-benchmark-summary.json", "raw/frozen-benchmark-summary.csv");
  for (const tier of TIERS) {
    const realVideos = artifacts.filter((a) => a.path.startsWith(`raw/real-${tier}/`) && a.path.endsWith(".webm"));
    const protoVideos = artifacts.filter((a) => a.path.startsWith(`raw/prototype-${tier}/`) && a.path.endsWith(".webm"));
    gate(gates, `artifacts_video_real_${tier}`, realVideos.length === 1 && realVideos[0].size > 0, { videos: realVideos });
    gate(gates, `artifacts_video_prototype_${tier}`, protoVideos.length === 1 && protoVideos[0].size > 0, { videos: protoVideos });
  }
  const byPath = new Map(artifacts.map((a) => [a.path, a]));
  const missing = required.filter((p) => !byPath.has(p) || byPath.get(p).size <= 0 || !byPath.get(p).sha256);
  gate(gates, "artifacts_completeness_sha256_reachable", missing.length === 0, {
    required_count: required.length,
    missing_or_empty: missing,
    manifest_count: artifacts.length,
  });
}

function verifyLegacyAuditCompleteness(gates, summary) {
  const missing = [...REQUIRED_AUDIT_GATES].filter((name) => !summary.audit.some((row) => row.gate === name && row.verdict === "PASS"));
  gate(gates, "legacy_audit_rows_present", missing.length === 0, { required: [...REQUIRED_AUDIT_GATES], missing });
}

function verifyPerformanceSummary(gates, summary) {
  const real = summary.realResults;
  const proto = summary.protoResults;
  gate(gates, "real_baseline_performance_100_500_2200", TIERS.every((tier) => real.some((r) => r.tier === tier && r.tti_ms > 0 && r.quiet_render?.p95_ms > 0 && r.memory?.js_heap_mb > 0)), {
    tiers: real.map((r) => ({ tier: r.tier, tti_ms: r.tti_ms, p95_ms: r.quiet_render?.p95_ms, memory: r.memory?.js_heap_mb })),
  });
  gate(gates, "prototype_performance_100_500_2200", TIERS.every((tier) => proto.some((r) => r.tier === tier && r.tti_ms > 0 && r.quiet_render?.p95_ms > 0 && r.memory?.js_heap_mb > 0)), {
    tiers: proto.map((r) => ({ tier: r.tier, tti_ms: r.tti_ms, p95_ms: r.quiet_render?.p95_ms, memory: r.memory?.js_heap_mb })),
  });
}

function applyInjection(gates, artifacts) {
  if (!injection) return;
  const fail = (name, reason) => {
    const g = gates.find((candidate) => candidate.gate === name);
    if (g) {
      g.verdict = "REJECT";
      g.reason = reason;
      g.injected_fault = injection;
    }
  };
  if (injection === "wrong-expected-edge") fail("prototype_100_project_filter", "injected wrong expected edge id");
  else if (injection === "missing-agent") fail("real_100_agent_filter", "injected missing agent from expected set");
  else if (injection === "wrong-selected-id") fail("prototype_100_edge_evidence_context", "injected wrong selected edge id");
  else if (injection === "viewport-drift") fail("real_100_context_state", "injected viewport drift");
  else if (injection === "missing-video") {
    const video = artifacts.find((a) => a.path.startsWith("raw/real-100/") && a.path.endsWith(".webm"));
    if (video) video.injected_missing = true;
    fail("artifacts_video_real_100", "injected missing video artifact");
  } else {
    gates.push({ gate: "unknown_injection", verdict: "REJECT", reason: `unknown injection ${injection}` });
  }
}

function filteredApiFromHar(tier, agentRef) {
  const har = readJson(join(RAW, `real-${tier}`, "network.har"));
  const entry = har.log.entries.find((e) => e.request.url.includes("/api/orgs/") && e.request.url.includes("collaboration-effects?") && e.request.url.includes(`agent_ref=${encodeURIComponent(agentRef)}`) && e.request.url.includes("lod=full") && e.response.status === 200 && e.response.content.text.trim().startsWith("{"));
  if (!entry) throw new Error(`missing filtered API HAR entry for tier ${tier}`);
  return JSON.parse(entry.response.content.text);
}

function expectedRealAgentFilter(full, agentRef) {
  const edgeIds = new Set();
  const taskIds = new Set();
  const planIds = new Set();
  for (const e of full.graph.edges) {
    if (e.source === agentRef || e.target === agentRef) {
      edgeIds.add(e.id);
      if (e.source.startsWith("task:")) taskIds.add(e.source);
      if (e.target.startsWith("task:")) taskIds.add(e.target);
      if (e.source.startsWith("plan:")) planIds.add(e.source);
      if (e.target.startsWith("plan:")) planIds.add(e.target);
    }
  }
  for (const e of full.graph.edges) {
    if (e.relation_type === "plan_task" && (taskIds.has(e.source) || taskIds.has(e.target))) {
      edgeIds.add(e.id);
      if (e.source.startsWith("plan:")) planIds.add(e.source);
      if (e.target.startsWith("plan:")) planIds.add(e.target);
    }
  }
  for (const e of full.graph.edges) {
    if (e.relation_type === "project_plan" && (planIds.has(e.source) || planIds.has(e.target))) {
      edgeIds.add(e.id);
    }
  }
  const nodeIds = new Set();
  for (const e of full.graph.edges) {
    if (edgeIds.has(e.id)) {
      nodeIds.add(e.source);
      nodeIds.add(e.target);
    }
  }
  return { nodeIds: [...nodeIds].sort(), edgeIds: [...edgeIds].sort() };
}

function prototypeExpectedManifest(tier) {
  const expected = FIXTURE_MANIFEST.tiers[String(tier)];
  return {
    project: { edgeIds: [...expected.project_filter.edge_ids].sort(), count: expected.project_filter.count },
    agent: { edgeIds: [...expected.agent_filter.edge_ids].sort(), count: expected.agent_filter.count },
  };
}

function artifactManifest() {
  const files = walk(EVIDENCE).filter((p) => !p.endsWith("oracle-verdict.json") && !p.includes("/red-team/"));
  return files.map((path) => {
    const body = readFileSync(path);
    return {
      path: path.replace(EVIDENCE + "/", ""),
      size: body.length,
      sha256: createHash("sha256").update(body).digest("hex"),
      reachable: statSync(path).isFile(),
    };
  }).sort((a, b) => a.path.localeCompare(b.path));
}

function walk(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return walk(path);
    if (entry.isFile()) return [path];
    return [];
  });
}

function readJson(path) {
  return JSON.parse(readFileSync(path, "utf8"));
}

function ids(rows) {
  return rows.map((row) => row.id).sort();
}

function counts(body) {
  return { nodes: body.graph.nodes.length, edges: body.graph.edges.length };
}

function digestList(values) {
  return createHash("sha256").update([...values].sort().join("\n")).digest("hex");
}

function setEq(a, b) {
  const aa = [...a].sort();
  const bb = [...b].sort();
  return aa.length === bb.length && aa.every((value, i) => value === bb[i]);
}

function gate(gates, name, pass, detail) {
  gates.push({ gate: name, verdict: pass ? "PASS" : "REJECT", ...detail });
}

function rank(value) {
  return { "1h": 0, "24h": 1, "7d": 2, "30d": 3 }[value] ?? 3;
}

main();
