import { mkdir } from "node:fs/promises";
import { resolve } from "node:path";
import { test, expect } from "../fixtures/agent-center.js";

const evidenceDir = resolve(process.cwd(), "../../../docs/acceptance/t1456-s3");

const permissions = ["team.read", "team.write", "team.memory.read", "team.memory.propose", "team.memory.review"];
const baseRoles = Array.from({ length: 12 }, (_, index) => ({
  id: index === 0 ? "deploy-operator" : `role-${index + 1}`,
  stable_key: index === 0 ? "deploy-operator" : `registry-role-${index + 1}`,
  name: index === 0 ? "Deploy operator" : `Registry role ${index + 1}`,
  kind: "custom",
  description: index === 0 ? "Deploy and review team changes." : "Registry-backed access role.",
  scope: index === 10 ? "project" : "team",
  version: index === 0 ? 3 : 1,
  permissions: index === 0 ? permissions : [index % 2 ? "team.read" : "team.write"],
  risk: index === 0 ? "high" : index % 2 ? "low" : "medium",
  references: index === 0 ? 1 : 0,
}));

function detailFor(role: (typeof baseRoles)[number], referenced = false) {
  const v2 = { ...role, version: Math.max(2, role.version - 1), permissions: role.permissions.slice(0, -1), risk: "medium" };
  return {
    ...role,
    latest: role,
    versions: role.version > 1 ? [role, v2, { ...v2, version: 1, permissions: ["team.read"], risk: "low" }] : [role],
    references: referenced ? [{ team_id: "team-core", team_name: "Platform Team", team_role: "developer" }] : [],
  };
}

test.describe("RAM Roles visual state evidence", () => {
  test("captures canonical states, themes, widths, CRUD, references, and CAS", async ({ page, agentCenter, authSession }) => {
    await mkdir(evidenceDir, { recursive: true });
    let mode: "ready" | "loading" | "empty" | "error" | "forbidden" = "ready";
    let referenced = true;
    let roles = [...baseRoles];

    await page.route("**/api/orgs/*/access/ram-roles**", async (route) => {
      const request = route.request();
      const url = new URL(request.url());
      const tail = url.pathname.split("/access/ram-roles")[1];
      if (mode === "loading" && request.method() === "GET" && tail === "") {
        await new Promise((resolveDelay) => setTimeout(resolveDelay, 20_000));
      }
      if (mode === "error" && request.method() === "GET" && tail === "") {
        return route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ message: "permission registry temporarily unavailable" }) });
      }
      if (request.method() === "GET" && tail === "") {
        return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ roles: mode === "empty" ? [] : roles }) });
      }
      const id = decodeURIComponent(tail.split("/").filter(Boolean)[0] ?? "");
      const role = roles.find((item) => item.id === id) ?? roles[0];
      if (request.method() === "GET") {
        return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(detailFor(role, referenced && id === "deploy-operator")) });
      }
      if (request.method() === "POST" && tail === "") {
        const body = request.postDataJSON() as { name: string; stable_key: string; description: string; scope: string; permissions: string[]; risk: string };
        const created = { id: "visual-created", kind: "custom", version: 1, references: 0, ...body } as (typeof baseRoles)[number];
        roles = [created, ...roles];
        return route.fulfill({ status: 201, contentType: "application/json", body: JSON.stringify(detailFor(created)) });
      }
      if (request.method() === "PATCH") {
        return route.fulfill({ status: 409, contentType: "application/json", body: JSON.stringify({ error: "version_conflict", message: "RAM Role was updated by another administrator" }) });
      }
      return route.fulfill({ status: 204, body: "" });
    });

    await page.route("**/api/orgs/*/teams", async (route) => route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(referenced ? [{
        id: "team-core", org_id: authSession.organizationID, name: "Platform Team", glyph: "PT", status: "active", description: "Core platform", projects_count: 1, members_count: 3, created: "2026/8/24", version: 4,
        roles: [{ role: "developer", count: 3, cli: "codex", model: "gpt-5.6", max_concurrency: 2, capability_tags: ["ship"], ram_role_keys: ["Deploy operator"], access_requirements: permissions }],
      }] : []),
    }));
    await page.route("**/api/orgs/*/teams/team-core/roles/developer/ram-roles", async (route) => route.fulfill({
      status: 200, contentType: "application/json", body: JSON.stringify({ team_id: "team-core", team_role: "developer", ram_role_ids: ["deploy-operator"], version: 7 }),
    }));
    await page.route("**/api/orgs/*/teams/team-core/members", async (route) => route.fulfill({ status: 200, contentType: "application/json", body: "[]" }));

    const url = `${agentCenter.baseURL}/organizations/${authSession.orgSlug}/access?view=ram-roles`;
    await page.setViewportSize({ width: 1672, height: 941 });
    await page.goto(url);
    await expect(page.getByTestId("access-roles-view")).toBeVisible();
    await page.screenshot({ path: resolve(evidenceDir, "01-ready-1672-light.png"), fullPage: true });
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.screenshot({ path: resolve(evidenceDir, "ready-1280-light.png"), fullPage: true });
    await page.setViewportSize({ width: 1672, height: 941 });

    await page.evaluate(() => { localStorage.setItem("ac.theme", "dark"); document.documentElement.classList.add("dark"); });
    await page.screenshot({ path: resolve(evidenceDir, "02-ready-1672-dark.png"), fullPage: true });
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.screenshot({ path: resolve(evidenceDir, "03-ready-1280-dark.png"), fullPage: true });
    await page.getByTestId("access-role-density").selectOption("compact");
    await page.screenshot({ path: resolve(evidenceDir, "04-compact-1280-dark.png"), fullPage: true });

    await page.getByTestId("access-role-row-deploy-operator").click();
    await page.getByTestId("access-role-view-references").click();
    await page.screenshot({ path: resolve(evidenceDir, "05-referenced-delete-blocked-1280-dark.png"), fullPage: true });

    await page.getByTestId("access-role-new").click();
    await page.screenshot({ path: resolve(evidenceDir, "06-create-drawer-1280-dark.png"), fullPage: true });
    await page.getByTestId("access-role-name").fill("Visual auditor");
    await page.getByTestId("access-role-stable-key").fill("visual-auditor");
    await page.getByText("team.read", { exact: true }).last().click();
    await page.getByTestId("access-role-create-submit").click();
    await expect(page.getByTestId("access-toast")).toContainText("Created RAM Role");
    await page.screenshot({ path: resolve(evidenceDir, "07-create-success-toast-1280-dark.png"), fullPage: true });

    referenced = false;
    await page.getByTestId("access-role-row-deploy-operator").click();
    await page.getByTestId("access-role-edit-open").click();
    await page.screenshot({ path: resolve(evidenceDir, "08-edit-drawer-1280-dark.png"), fullPage: true });
    await page.getByTestId("access-role-description").fill("Changed concurrently");
    await page.getByTestId("access-role-risk-ack").click();
    await page.getByTestId("access-role-create-submit").click();
    await expect(page.getByTestId("access-toast")).toContainText("409");
    await page.screenshot({ path: resolve(evidenceDir, "09-cas-409-toast-1280-dark.png"), fullPage: true });
    await page.getByLabel("Close").click();
    await page.getByTestId("access-role-row-role-2").click();
    await page.getByTestId("access-role-delete-name").fill("Registry role 2");
    await page.screenshot({ path: resolve(evidenceDir, "10-typed-delete-1280-dark.png"), fullPage: true });

    await page.setViewportSize({ width: 1672, height: 941 });
    await page.evaluate(() => { localStorage.setItem("ac.theme", "light"); document.documentElement.classList.remove("dark"); });
    mode = "empty";
    await page.reload();
    await expect(page.getByTestId("access-role-empty")).toBeVisible();
    await page.screenshot({ path: resolve(evidenceDir, "11-empty-1672-light.png"), fullPage: true });
    mode = "error";
    await page.reload();
    await expect(page.getByTestId("access-role-list-error")).toBeVisible();
    await page.screenshot({ path: resolve(evidenceDir, "12-error-1672-light.png"), fullPage: true });
    mode = "loading";
    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.getByTestId("access-role-list-loading")).toBeVisible();
    await page.screenshot({ path: resolve(evidenceDir, "13-loading-1672-light.png"), fullPage: true });

    mode = "forbidden";
    await page.route("**/api/orgs/*/permissions/effective**", async (route) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ subject_ref: authSession.identityID, resource: { kind: "org", id: authSession.organizationID }, permissions: [{ key: "org.read", source: "org_role", evidence_ref: "members:visual" }] }) }));
    await page.reload();
    await expect(page.getByTestId("access-forbidden")).toBeVisible();
    await page.screenshot({ path: resolve(evidenceDir, "14-forbidden-1672-light.png"), fullPage: true });
  });
});
