import { test, expect } from "../fixtures/agent-center.js";

const project = {
  id: "proj-a",
  organization_id: "org-test",
  name: "Project Alpha",
  description: "",
  status: "active",
  created_by: "user:owner",
  version: 1,
  created_at: "2026-07-20T01:00:00Z",
  updated_at: "2026-07-20T01:00:00Z",
};

const plan = {
  id: "PL-1",
  project_id: "proj-a",
  name: "Plan gate acceptance",
  description: "Verify stage gate audit layout",
  status: "running",
  org_ref: "P117",
  creator_ref: "user:owner",
  conversation_id: "",
  has_failed: false,
  progress: { done: 0, total: 2 },
  created_at: "2026-07-20T01:00:00Z",
  nodes: [
    { task_id: "n1", title: "Desktop layout", assignee_ref: "agent:dev", task_status: "running", node_status: "running", depends_on: [] },
    { task_id: "n2", title: "Mobile audit", assignee_ref: "agent:dev", task_status: "open", node_status: "ready", depends_on: ["n1"] },
  ],
};

const stages = {
  stages: [{
    id: "st-ui",
    name: "Responsive UI",
    status: "reopen",
    rounds: 1,
    max_rounds: 3,
    depends_on_stages: [],
    gate_node_id: "gate-ui",
    gate_task_id: "gate-task-ui",
    gate_spec: {
      evaluator_kind: "human",
      assignee_ref: "agent:reviewer",
      acceptance_contract: "Verify desktop and mobile audit fields with real browser evidence.",
      pass_route: "downstream",
      reject_route: "reopen_stage",
      exhausted_route: "escalate",
    },
    gate_outcome: "",
    gate_evidence: "",
    gate_reviewed_sha: "",
    diagnostics: [{ code: "missing_browser_evidence", message: "Attach screenshots" }],
    members: [
      { task_id: "n1", title: "Desktop layout", task_status: "running" },
      { task_id: "n2", title: "Mobile audit", task_status: "open" },
    ],
  }],
};

async function mockPlanGateApis(page: import("@playwright/test").Page) {
  await page.route("**/api/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.startsWith("/api/auth/") || path === "/api/orgs") return route.continue();
    const resource = path.replace(/^\/api\/orgs\/[^/]+/, "/api");
    if (resource === "/api/projects/proj-a") return route.fulfill({ json: project });
    if (resource === "/api/projects/proj-a/plans/PL-1") return route.fulfill({ json: plan });
    if (resource === "/api/projects/proj-a/plans/PL-1/stages") return route.fulfill({ json: stages });
    if (resource === "/api/projects/proj-a/plans/PL-1/graph") return route.fulfill({ json: { has_graph: false } });
    if (resource.endsWith("/related-issues")) return route.fulfill({ json: [] });
    return route.fulfill({ json: [] });
  });
}

test("stage gate audit does not overlap member cards and remains complete on mobile", async ({ page, agentCenter }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const signup = await page.request.post(`${agentCenter.apiURL}/auth/signup`, {
    data: {
      display_name: "Acceptance Reviewer",
      email: "reviewer@example.test",
      passcode: "Review1!",
      organization_name: "Acceptance",
      organization_slug: "acceptance",
    },
  });
  expect(signup.ok()).toBeTruthy();
  const signin = await page.request.post(`${agentCenter.apiURL}/auth/signin`, {
    data: { display_name: "Acceptance Reviewer", passcode: "Review1!" },
  });
  const session = /ac_session=([^;]+)/.exec(signin.headers()["set-cookie"] || "")?.[1];
  expect(session).toBeTruthy();
  await page.context().addCookies([{
    name: "ac_session",
    value: session!,
    domain: "127.0.0.1",
    path: "/",
    httpOnly: true,
    sameSite: "Lax",
  }]);
  const orgs = await (await page.request.get(`${agentCenter.apiURL}/orgs`)).json();
  const slug = orgs[0].slug as string;
  await mockPlanGateApis(page);
  await page.goto(`${agentCenter.baseURL}/organizations/${slug}/projects/proj-a/plans/PL-1`);
  await page.getByTestId("plan-tab-dag").click();

  const audit = page.getByTestId("plan-stage-gate-audit-st-ui");
  const firstMember = page.getByTestId("plan-dag-node").first();
  await expect(audit).toBeVisible();
  await expect(firstMember).toBeVisible();

  const [auditBox, memberBox] = await Promise.all([audit.boundingBox(), firstMember.boundingBox()]);
  expect(auditBox).not.toBeNull();
  expect(memberBox).not.toBeNull();
  expect(auditBox!.y + auditBox!.height).toBeLessThanOrEqual(memberBox!.y);

  const evidenceDir = process.env.PLAN_GATE_EVIDENCE_DIR;
  if (evidenceDir) {
    await page.screenshot({ path: `${evidenceDir}/desktop-light.png`, fullPage: true });
    await page.emulateMedia({ colorScheme: "dark" });
    await page.screenshot({ path: `${evidenceDir}/desktop-dark.png`, fullPage: true });
    await page.emulateMedia({ colorScheme: "light" });
  }

  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.getByTestId("plan-stage-mobile-gate-evaluator-st-ui")).toContainText("human · agent:reviewer");
  await expect(page.getByTestId("plan-stage-mobile-gate-contract-st-ui")).toContainText("Verify desktop and mobile");
  await expect(page.getByTestId("plan-stage-mobile-gate-evidence-st-ui")).toContainText("Outcome pending");
  await expect(page.getByTestId("plan-stage-mobile-gate-evidence-st-ui")).toContainText("No evidence");
  await expect(page.getByTestId("plan-stage-mobile-gate-evidence-st-ui")).toContainText("No reviewed SHA");
  await expect(page.getByTestId("plan-stage-mobile-gate-diagnostics-st-ui")).toContainText("missing_browser_evidence");
  if (evidenceDir) {
    await page.screenshot({ path: `${evidenceDir}/mobile-390x844.png`, fullPage: true });
  }
});
