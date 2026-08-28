import { resolve } from "node:path";
import type { APIRequestContext } from "@playwright/test";
import { test, expect } from "../fixtures/agent-center.js";

const OUT = resolve(import.meta.dirname, "../../../../docs/acceptance/s4-team-role");

async function json(request: APIRequestContext, method: "get" | "post", url: string, data?: unknown) {
  const response = await request[method](url, data === undefined ? undefined : { data });
  expect(response.ok(), `${method.toUpperCase()} ${url}: ${await response.text()}`).toBeTruthy();
  return response.json();
}

test("captures canonical Team Role states from the production web chain", async ({ agentCenter, authSession, request, page }) => {
  test.setTimeout(60_000);
  const api = authSession.orgApiURL;
  const runtime = await json(request, "get", `${api}/ai-runtime`);
  let revision = runtime.revision;
  if (!runtime.clis?.some((item: { key: string }) => item.key === "codex")) {
    const next = await json(request, "post", `${api}/ai-runtime/clis`, { expected_revision: revision, value: { key: "codex", display_name: "Codex", executable: "codex", required_features: [], enabled: true } });
    revision = next.revision;
  }
  if (!runtime.models?.some((item: { key: string }) => item.key === "gpt-5")) {
    await json(request, "post", `${api}/ai-runtime/models`, { expected_revision: revision, value: { key: "gpt-5", model_key: "gpt-5", display_name: "GPT-5", compatible_cli_keys: ["codex"], default_parameters: {}, enabled: true } });
  }

  const contributor = await json(request, "post", `${api}/access/ram-roles`, { name: "Project contributor", stable_key: "project.contributor", description: "Project delivery permissions.", scope: "team", permissions: ["team.read", "project.read", "project.write"] });
  const reader = await json(request, "post", `${api}/access/ram-roles`, { name: "Artifact reader", stable_key: "artifact.reader", description: "Read-only artifact access.", scope: "team", permissions: ["team.read", "project.read"] });
  const reviewer = await json(request, "post", `${api}/access/ram-roles`, { name: "Review gate", stable_key: "review.gate", description: "Review and approval permissions.", scope: "team", permissions: ["team.read", "team.memory.review"] });
  const team = await json(request, "post", `${api}/teams`, {
    name: "Platform Team",
    description: "Canonical Team Role acceptance team.",
    visibility: "private",
    roles: [
      { role: "developer", cli: "codex", model: "gpt-5", max_concurrency: 4, count: 3, tags: "frontend,backend,ram", ram_role_keys: ["Project contributor", "Artifact reader"], access_requirements: ["project.read", "project.write"] },
      { role: "operator", cli: "codex", model: "gpt-5", max_concurrency: 1, count: 1, tags: "ops", ram_role_keys: [], access_requirements: [] },
    ],
  });
  const saved = await request.put(`${api}/teams/${team.id}/roles/developer/ram-roles`, { data: { ram_role_ids: [contributor.id, reader.id], expected_version: 1 } });
  expect(saved.ok(), await saved.text()).toBeTruthy();

  await page.setViewportSize({ width: 1672, height: 941 });
  await page.goto(`${agentCenter.baseURL}/organizations/${authSession.orgSlug}/teams`, { waitUntil: "domcontentloaded" });
  await expect(page.getByTestId("page-Teams")).toBeVisible();
  await page.getByTestId(`team-row-${team.id}`).click();
  await page.getByRole("tab", { name: "Roles" }).click();
  await page.getByTestId("team-role-open-developer").click();
  await expect(page.getByTestId("page-TeamRoleDetail")).toBeVisible();
  await expect(page.getByTestId("team-role-access-configuration")).toContainText("Project contributor");
  await page.screenshot({ path: `${OUT}/01-ready-1672.png` });

  await page.getByTestId("team-role-edit").click();
  await page.getByTestId("team-role-ram-roles-trigger").click();
  await page.getByTestId("team-role-ram-roles-option").filter({ hasText: "Review gate" }).click();
  await expect(page.getByTestId("team-role-impact")).toContainText("Affected members");
  await page.getByTestId("team-role-ram-roles-trigger").click();
  await page.getByTestId("team-role-impact").scrollIntoViewIfNeeded();
  await page.screenshot({ path: `${OUT}/02-editor-impact-1672.png` });
  await page.getByTestId("team-role-save").click();
  await expect(page.getByTestId("team-role-success")).toContainText("Server readback confirmed version 3");
  await page.screenshot({ path: `${OUT}/03-save-readback-1672.png` });

  await page.setViewportSize({ width: 1280, height: 720 });
  await page.screenshot({ path: `${OUT}/04-ready-1280.png` });
  await page.getByTestId("team-role-edit").click();
  await page.getByRole("button", { name: "Remove" }).first().click();
  await expect(page.getByTestId("team-role-impact")).toContainText("RAM Roles");
  await page.route(`**/teams/${team.id}/roles/developer/ram-roles`, async (route) => {
    if (route.request().method() === "PUT") await route.fulfill({ status: 409, contentType: "application/json", body: JSON.stringify({ error: "version_conflict", message: "version_conflict" }) });
    else await route.continue();
  });
  await page.getByTestId("team-role-save").click();
  await expect(page.getByTestId("team-role-conflict")).toContainText("409");
  await page.getByTestId("team-role-conflict").scrollIntoViewIfNeeded();
  await page.screenshot({ path: `${OUT}/05-conflict-1280.png` });
  await page.unroute(`**/teams/${team.id}/roles/developer/ram-roles`);

  await page.setViewportSize({ width: 1672, height: 941 });
  await page.route(`**/teams/${team.id}`, async (route) => { await new Promise((done) => setTimeout(done, 2000)); await route.abort(); });
  await page.goto(`${agentCenter.baseURL}/organizations/${authSession.orgSlug}/teams/${team.id}/roles/developer`, { waitUntil: "domcontentloaded" });
  await expect(page.getByTestId("team-role-loading")).toBeVisible();
  await page.screenshot({ path: `${OUT}/06-loading-1672.png` });
  await page.waitForTimeout(2100);
  await page.unroute(`**/teams/${team.id}`);

  await page.route(`**/teams/${team.id}`, (route) => route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ message: "Team Role service unavailable" }) }));
  await page.reload({ waitUntil: "domcontentloaded" });
  await expect(page.getByTestId("team-role-error")).toContainText("Team Role service unavailable");
  await page.screenshot({ path: `${OUT}/07-error-1672.png` });
  await page.unroute(`**/teams/${team.id}`);

  await page.route("**/permissions/effective?**", (route) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ subject_ref: `user:${authSession.identityID}`, resource: { kind: "team", id: team.id }, permissions: [{ key: "org.read", source: "org_role", evidence_ref: "members:read-only" }] }) }));
  await page.reload({ waitUntil: "domcontentloaded" });
  await expect(page.getByTestId("team-role-permission-gate")).toBeVisible();
  await page.screenshot({ path: `${OUT}/08-forbidden-1672.png` });
  await page.unroute("**/permissions/effective?**");

  await page.goto(`${agentCenter.baseURL}/organizations/${authSession.orgSlug}/teams/${team.id}/roles/operator`, { waitUntil: "domcontentloaded" });
  await expect(page.getByTestId("team-role-ram-roles-empty")).toBeVisible();
  await page.screenshot({ path: `${OUT}/09-empty-1672.png` });
  await page.getByTestId("team-role-edit").click();
  await page.getByTestId("team-role-delete").click();
  await expect(page.getByTestId("team-role-delete-impact")).toContainText("members");
  await page.setViewportSize({ width: 1280, height: 720 });
  await page.screenshot({ path: `${OUT}/10-delete-protection-1280.png` });
});
