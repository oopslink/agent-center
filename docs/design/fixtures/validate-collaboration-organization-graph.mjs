#!/usr/bin/env node
import fs from 'node:fs';
import process from 'node:process';

const fixturePath = process.argv[2] || 'docs/design/fixtures/collaboration-organization-graph-v1.json';
const fixture = JSON.parse(fs.readFileSync(fixturePath, 'utf8'));
const failures = [];
let checks = 0;

function check(value, message) {
  checks += 1;
  if (!value) failures.push(message);
}

const { scale, generation, acceptance } = fixture;
const agents = Array.from({ length: scale.agent_count }, (_, i) => ({ id: `agent:fixture-${i + 1}` }));
const plans = Array.from({ length: scale.plan_count }, (_, i) => ({ id: `plan:fixture-${i + 1}` }));
const plannedTasks = plans.flatMap((plan, planIndex) =>
  Array.from({ length: scale.tasks_per_plan }, (_, taskIndex) => ({
    id: `task:fixture-${planIndex + 1}-${taskIndex + 1}`,
    plan_id: plan.id,
  })),
);
const floatingTasks = Array.from({ length: scale.floating_task_count }, (_, i) => ({ id: `task:floating-${i + 1}`, plan_id: null }));
const tasks = [...plannedTasks, ...floatingTasks];
const nodes = [...agents, ...plans, ...tasks];

// Materialize representative edges deterministically. The final agent is kept
// at degree zero to prove roster inclusion is independent of activity.
const activeAgents = agents.slice(0, -1);
const edges = [];
for (const [i, task] of tasks.entries()) {
  const agent = activeAgents[i % activeAgents.length];
  edges.push({ id: `at-${i}`, kind: 'agent_task', source: agent.id, target: task.id, evidence_refs: [`effect:${i}`] });
  if (task.plan_id) edges.push({ id: `pt-${i}`, kind: 'plan_task', source: task.plan_id, target: task.id, evidence_refs: [`membership:${i}`] });
}
for (const [i, plan] of plans.entries()) {
  edges.push({ id: `ap-${i}`, kind: 'agent_plan', source: activeAgents[i].id, target: plan.id, evidence_refs: [`aggregate:plan:${i}`] });
}
for (let i = 0; i < activeAgents.length - 1; i += 1) {
  edges.push({ id: `aa-${i}`, kind: 'agent_agent', source: activeAgents[i].id, target: activeAgents[i + 1].id, evidence_refs: [`aggregate:agent:${i}`] });
}

check(fixture.contract_version === 'collaboration-graph.org.v1', 'contract_version must be collaboration-graph.org.v1');
check(/^[0-9a-f]{40}$/.test(fixture.base_origin_main_sha), 'base_origin_main_sha must be a full SHA');
check(Boolean(fixture.organization?.slug), 'organization.slug is required');
check(generation.seed === 't2108-org-graph-v1', 'generator seed changed');
check(Number.isInteger(generation.page_size) && generation.page_size >= 50 && generation.page_size <= 500, 'page_size must be 50..500');
check(!Number.isNaN(Date.parse(generation.as_of)) && /Z$/.test(generation.as_of), 'as_of must be RFC3339 UTC');
check(acceptance.initial_query_required === false, 'organization landing must not require a query');
check(acceptance.search_is_focus_only === true, 'search must remain a focus control');
check(acceptance.evidence_is_lazy === true, 'evidence must be lazy');
check(acceptance.llm_adjudication_allowed === false, 'LLM adjudication must be forbidden');
check(acceptance.writes_business_truth === false, 'projection must not write business truth');

const actual = { agents: agents.length, plans: plans.length, tasks: tasks.length, nodes: nodes.length };
for (const [key, expected] of Object.entries(acceptance.expected_materialized)) {
  check(actual[key] === expected, `${key}: got ${actual[key]}, want ${expected}`);
}
check(new Set(nodes.map((node) => node.id)).size === nodes.length, 'materialized node ids must be unique');
check(floatingTasks.some((task) => task.plan_id === null), 'fixture must contain a floating task');

const nodeIDs = new Set(nodes.map((node) => node.id));
for (const edge of edges) {
  check(nodeIDs.has(edge.source), `${edge.id}: dangling source ${edge.source}`);
  check(nodeIDs.has(edge.target), `${edge.id}: dangling target ${edge.target}`);
  check(edge.evidence_refs.length > 0, `${edge.id}: evidence_refs must not be empty`);
}
const edgeKinds = new Set(edges.map((edge) => edge.kind));
for (const kind of fixture.required_edge_kinds) check(edgeKinds.has(kind), `missing edge kind ${kind}`);

const zeroDegree = agents[fixture.pinned_cases.find((item) => item.id === 'zero-degree-agent').agent_index];
check(!edges.some((edge) => edge.source === zeroDegree.id || edge.target === zeroDegree.id), 'zero-degree agent acquired an edge');
check(fixture.pinned_cases.some((item) => item.id === 'floating-task' && item.plan_id === null), 'floating-task pin missing');
check(fixture.pinned_cases.some((item) => item.id === 'directed-agent-contact'), 'directed agent contact pin missing');
check(fixture.pinned_cases.some((item) => item.id === 'mixed-polarity-aggregate'), 'mixed polarity pin missing');
check(fixture.pinned_cases.some((item) => item.id === 'disconnected-component'), 'disconnected component pin missing');

const requiredScenarios = ['org-overview-loaded', 'org-overview-progressive', 'org-focus-task', 'org-edge-evidence', 'org-zero-degree-agent', 'org-empty', 'org-error-retry', 'org-reduced-motion'];
const scenarioIDs = new Set(fixture.screenshot_scenarios.map((scenario) => scenario.id));
for (const id of requiredScenarios) check(scenarioIDs.has(id), `missing screenshot scenario ${id}`);
for (const scenario of fixture.screenshot_scenarios) {
  check(scenario.viewports.length > 0, `${scenario.id}: viewports must not be empty`);
  check(scenario.viewports.every((value) => /^\d+x\d+$/.test(value)), `${scenario.id}: malformed viewport`);
}

const result = {
  contract_version: fixture.contract_version,
  seed: generation.seed,
  materialized: actual,
  edge_count: edges.length,
  edge_kinds: [...edgeKinds].sort(),
  screenshot_scenarios: scenarioIDs.size,
  checks,
  failures,
};
console.log(JSON.stringify(result, null, 2));
if (failures.length) process.exit(1);
