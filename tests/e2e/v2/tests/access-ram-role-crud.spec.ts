import { test, expect } from "../fixtures/agent-center.js";

test.describe("Access RAM Role CRUD", () => {
  test("owner creates, edits, and deletes an unreferenced RAM Role through the Access UI", async ({
    page,
    agentCenter,
    authSession,
  }) => {
    await page.goto(`${agentCenter.baseURL}/organizations/${authSession.orgSlug}/access?view=roles-and-mappings`);

    await expect(page.getByTestId("access-roles-view")).toBeVisible();
    await expect(page.getByRole("heading", { name: "RAM Roles" })).toBeVisible();

    const suffix = Date.now().toString(36);
    const name = `Browser CRUD ${suffix}`;
    const stableKey = `browser-crud-${suffix}`;

    const create = page.getByTestId("access-role-create");
    await create.getByTestId("access-role-name").fill(name);
    await create.getByTestId("access-role-stable-key").fill(stableKey);
    await create.getByTestId("access-role-description").fill("browser regression role");
    await create.getByTestId("access-role-scope").fill("team");
    await create.getByText("team.read").click();
    await create.getByTestId("access-role-create-submit").click();

    await expect(page.getByTestId("access-role-detail")).toContainText(name);
    await expect(page.getByTestId("access-role-detail")).toContainText(stableKey);
    await expect(page.getByTestId("access-role-used-by")).toContainText("None");

    const detail = page.getByTestId("access-role-detail");
    const editedStableKey = `${stableKey}-v2`;
    await detail.getByTestId("access-role-edit-stable-key").fill(editedStableKey);
    await detail.getByTestId("access-role-edit-description").fill("browser regression role edited");
    await detail.getByRole("button", { name: "team.memory.review High risk" }).click();
    await detail.getByTestId("access-role-new-version-submit").click();

    await expect(detail).toContainText("Latest v2");
    await expect(detail).toContainText(editedStableKey);
    await expect(page.getByTestId("access-role-versions")).toContainText("team.memory.review");

    await detail.getByTestId("access-role-delete-name").fill(name);
    await detail.getByTestId("access-role-disable-submit").click();
    await expect(page.getByTestId("confirm-modal")).toContainText("second confirmation");
    await page.getByTestId("confirm-modal-confirm").click();

    await expect(page.getByRole("row").filter({ hasText: name })).toHaveCount(0);
  });

  test("fresh deployed SPA exposes distinct RAM Role, Team Role CRUD, and Subject access states", async ({
    page,
    agentCenter,
    authSession,
    request,
  }) => {
    const catalogResponse = await request.get(`${authSession.orgApiURL}/ai-runtime`);
    expect(catalogResponse.status()).toBe(200);
    const catalog = (await catalogResponse.json()) as { revision: number };
    const modelResponse = await request.post(`${authSession.orgApiURL}/ai-runtime/models`, {
      data: {
        expected_revision: catalog.revision,
        value: {
          key: "gpt-5-s3",
          model_key: "gpt-5-s3",
          display_name: "GPT-5 S3",
          compatible_cli_keys: ["codex"],
          default_parameters: {},
          enabled: true,
        },
      },
    });
    expect(modelResponse.status(), await modelResponse.text()).toBe(201);

    const teamResponse = await request.post(`${authSession.orgApiURL}/teams`, {
      data: {
        name: "S3 product acceptance",
        description: "fresh three-surface acceptance",
        visibility: "org-private",
        roles: [{
          role: "operator",
          cli: "codex",
          model: "gpt-5-s3",
          max_concurrency: 1,
          count: 1,
          tags: "acceptance",
          ram_role_keys: [],
        }],
      },
    });
    expect(teamResponse.status()).toBe(201);
    const team = (await teamResponse.json()) as { id: string };

    const ramRoleResponse = await request.post(`${authSession.orgApiURL}/access/ram-roles`, {
      data: {
        name: "S3 analytics reader",
        stable_key: "s3.analytics-reader",
        description: "selected by direct binding",
        scope: "org",
        permissions: ["org.analytics.read"],
      },
    });
    expect(ramRoleResponse.status(), await ramRoleResponse.text()).toBe(201);

    await page.goto(`${agentCenter.baseURL}/organizations/${authSession.orgSlug}/teams/${team.id}`);
    await expect(page.getByRole("heading", { name: "S3 product acceptance" })).toBeVisible();
    await page.getByTestId("team-edit-roles").click();
    const roleEditor = page.getByTestId("edit-team-roles-modal");
    await expect(roleEditor).toBeVisible();
    await roleEditor.getByTestId("edit-team-role-0-duplicate").click();
    await roleEditor.getByTestId("edit-team-role-1-name").fill("auditor");
    await roleEditor.getByTestId("team-save-roles").click();
    await expect(page.getByTestId("team-role-save-success")).toContainText("Role changes saved");
    await expect(page.getByTestId("team-role-used-by-auditor")).toBeVisible();

    await page.goto(`${agentCenter.baseURL}/organizations/${authSession.orgSlug}/access?view=roles-and-mappings`);
    await expect(page.getByTestId("access-roles-view")).toContainText("S3 analytics reader");
    await expect(page.getByTestId("access-team-role-mappings-view")).toContainText("auditor");

    await page.getByTestId("access-view-subjects").click();
    await expect(page.getByTestId("access-subject-view")).toBeVisible();
    await expect(page.getByTestId("access-subject-sidebar")).toContainText("Permission trace");
    await page.getByTestId("access-open-direct-binding").click();
    const direct = page.getByTestId("access-batch-drawer");
    await expect(direct).toContainText("RAM Role");
    await expect(direct.getByRole("button", { name: /S3 analytics reader/ })).toBeVisible();
    await expect(direct).not.toContainText("Batch authorization");
  });
});
