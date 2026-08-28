import { test, expect } from "../fixtures/agent-center.js";

test.describe("Access RAM Role CRUD", () => {
  test("owner creates, edits, and deletes an unreferenced RAM Role through the Access UI", async ({
    page,
    agentCenter,
    authSession,
  }) => {
    await page.goto(`${agentCenter.baseURL}/organizations/${authSession.orgSlug}/access?view=ram-roles`);

    await expect(page.getByTestId("access-roles-view")).toBeVisible();
    await expect(page.getByRole("heading", { name: "RAM Roles" })).toBeVisible();

    const suffix = Date.now().toString(36);
    const name = `Browser CRUD ${suffix}`;
    const stableKey = `browser-crud-${suffix}`;

    await page.getByTestId("access-role-new").click();
    const create = page.getByTestId("access-role-drawer");
    await create.getByTestId("access-role-name").fill(name);
    await create.getByTestId("access-role-stable-key").fill(stableKey);
    await create.getByTestId("access-role-description").fill("browser regression role");
    await create.getByTestId("access-role-scope").selectOption("team");
    await create.getByText("team.read").click();
    await create.getByTestId("access-role-create-submit").click();

    await expect(page.getByTestId("access-toast")).toContainText(`Created RAM Role ${name}.`);
    await page.getByTestId("access-toast").getByLabel("Dismiss notification").click();
    await expect(page.getByTestId("access-role-detail")).toContainText(name);
    await expect(page.getByTestId("access-role-detail")).toContainText(stableKey);
    await expect(page.getByTestId("access-role-used-by")).toContainText("None");

    const detail = page.getByTestId("access-role-detail");
    await detail.getByTestId("access-role-edit-open").click();
    const edit = page.getByTestId("access-role-drawer");
    await edit.getByTestId("access-role-description").fill("browser regression role edited");
    await edit.getByRole("button", { name: "team.memory.review High risk" }).click();
    await edit.getByTestId("access-role-risk-ack").click();
    await edit.getByTestId("access-role-create-submit").click();

    await expect(page.getByTestId("access-toast")).toContainText(`Saved RAM Role ${name}.`);
    await page.getByTestId("access-toast").getByLabel("Dismiss notification").click();
    await expect(detail).toContainText("Latest v2");
    await expect(page.getByTestId("access-role-versions")).toContainText("team.memory.review");

    await detail.getByTestId("access-role-delete-name").fill(name);
    await detail.getByTestId("access-role-disable-submit").click();

    await expect(page.getByTestId("access-toast")).toContainText(`Deleted RAM Role ${name}.`);
    await expect(page.getByRole("row").filter({ hasText: name })).toHaveCount(0);
  });
});
