import { execFile as execFileCb } from "node:child_process";
import { randomUUID } from "node:crypto";
import { promisify } from "node:util";

import { test, expect } from "../fixtures/agent-center.js";

const execFile = promisify(execFileCb);

// Seed a project via direct sqlite (S9 codified rule).
async function seedProject(dbPath: string): Promise<string> {
  const id = `p-co-${randomUUID().slice(0, 8)}`;
  const now = new Date().toISOString();
  const sql = `INSERT INTO projects
    (id, name, created_at, updated_at, created_by_identity_id, version)
    VALUES ('${id}', 'Carry-over Demo', '${now}', '${now}', 'user:hayang', 1);`;
  await execFile("sqlite3", [dbPath, sql]);
  return id;
}

// v2.1-A — full carry-over derive UI flow: select → modal → pick project
// → fill title → submit → navigate to /issues/<id> → CarryOverDivider
// renders with source message IDs as data attributes.
//
// Supersedes the S11 "modal opens with selected count" assertion (which
// stopped before submit because the modal didn't thread project_id).
test.describe("Carry-over derive UI", () => {
  test("multi-select → derive Issue → navigate to new Issue page with carry-over", async ({
    page,
    request,
    agentCenter,
  }) => {
    const projectID = await seedProject(agentCenter.dbPath);

    // Seed channel + 3 messages via API.
    const channelName = "review-room";
    const cR = await request.post(agentCenter.apiURL + "/conversations", {
      data: { kind: "channel", name: channelName },
    });
    expect(cR.status()).toBe(201);
    const channelID = ((await cR.json()) as { conversation_id: string })
      .conversation_id;

    const sentIDs: string[] = [];
    for (let i = 0; i < 3; i++) {
      const r = await request.post(
        agentCenter.apiURL + "/conversations/" + channelID + "/messages",
        { data: { content: `msg ${i + 1} — investigating` } },
      );
      expect(r.status()).toBe(201);
      const m = (await r.json()) as { message_id: string };
      sentIDs.push(m.message_id);
    }

    // Navigate to the channel page.
    await page.goto(`${agentCenter.baseURL}/channels/${channelName}`);

    // Wait for messages to render.
    const rows = page.locator('[data-testid="message-row"]');
    await expect(rows).toHaveCount(3);

    // Enter select mode + tick first 2 messages.
    await page.locator('[data-testid="select-mode-toggle"]').click();
    const checkboxes = page.locator('[data-testid="message-select"]');
    await checkboxes.nth(0).click();
    await checkboxes.nth(1).click();
    await expect(page.locator('[data-testid="derive-bar-count"]')).toContainText("2");

    // Open Issue modal.
    await page.locator('[data-testid="derive-open-issue"]').click();
    const modal = page.locator('[data-testid="derive-modal"]');
    await expect(modal).toBeVisible();
    await expect(modal).toHaveAttribute("data-kind", "issue");

    // Pick the project — v2.1-A new picker.
    const projectSelect = page.locator('[data-testid="derive-project-select"]');
    await expect(projectSelect).toBeVisible();
    await projectSelect.selectOption({ value: projectID });

    // Fill title.
    await page
      .locator('[data-testid="derive-title-input"]')
      .fill("Investigate review-room");

    // Submit — should now succeed because project_id is threaded.
    await page.locator('[data-testid="derive-modal-submit"]').click();

    // Success pane appears + click View Issue link to navigate.
    await expect(page.locator('[data-testid="derive-success"]')).toBeVisible();
    const viewLink = page.locator('[data-testid="derive-success-link"]');
    const href = await viewLink.getAttribute("href");
    expect(href, "success link should point at /issues/<conv_id>").toMatch(
      /^\/issues\/[A-Z0-9]+$/,
    );
    await viewLink.click();

    // Navigated to the new Issue's conversation page.
    await expect(page).toHaveURL(new RegExp(`${href}$`));

    // CarryOverDivider shows the source messages we picked.
    await expect(
      page.locator('[data-testid="carry-over-section"]'),
    ).toBeVisible();
    const carryMsgs = page.locator('[data-testid="carry-over-message"]');
    await expect(carryMsgs).toHaveCount(2);
    const renderedIDs = await carryMsgs.evaluateAll((nodes) =>
      nodes.map((n) => n.getAttribute("data-message-id")),
    );
    expect(renderedIDs.sort()).toEqual(sentIDs.slice(0, 2).sort());
  });
});
