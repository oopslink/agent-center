import { test, expect } from "../fixtures/agent-center.js";

// v2.1-C-3 — unread tracking end-to-end.
//
// Cold-start journey:
//   1. Seed a channel + N messages via the API (kept simple — same
//      pattern as the cold-start spec; direct sqlite seeding is only
//      mandatory when the API can't express the state).
//   2. Open /channels → assert the row's badge shows "N".
//   3. Open the channel → auto-mark-seen fires.
//   4. Back to /channels → assert the badge is gone.
test.describe("Unread tracking", () => {
  test("channel list badge ticks on new messages and clears on visit", async ({
    page,
    request,
    agentCenter,
  }) => {
    const channelName = "unread-room-" + Math.random().toString(36).slice(2, 8);
    const cR = await request.post(agentCenter.apiURL + "/conversations", {
      data: { kind: "channel", name: channelName },
    });
    expect(cR.status()).toBe(201);
    const channelID = ((await cR.json()) as { conversation_id: string })
      .conversation_id;

    // Seed 3 messages — these accumulate as unread since the
    // user_conversation_read_state row is absent.
    for (let i = 0; i < 3; i++) {
      const r = await request.post(
        agentCenter.apiURL + "/conversations/" + channelID + "/messages",
        { data: { content: "unread msg " + (i + 1) } },
      );
      expect(r.status()).toBe(201);
    }

    // Channel list shows the badge with count=3.
    await page.goto(agentCenter.baseURL + "/channels");
    const row = page.locator(
      `[data-testid="channel-row"][data-channel-name="${channelName}"]`,
    );
    await expect(row).toBeVisible();
    const badge = row.locator('[data-testid="unread-badge"]');
    await expect(badge).toHaveText("3");
    await expect(badge).toHaveAttribute("data-unread-count", "3");

    // Visit the channel → auto-mark-seen fires on mount.
    await row.locator("a").click();
    await expect(page.locator('[data-testid="page-ChannelDetail"]')).toBeVisible();
    // Wait for messages to render so the auto-mark-seen effect has run.
    await expect(page.locator('[data-testid="message-row"]')).toHaveCount(3);

    // Back to channels — the badge should disappear (count == 0 hides it).
    await page.goto(agentCenter.baseURL + "/channels");
    const rowAgain = page.locator(
      `[data-testid="channel-row"][data-channel-name="${channelName}"]`,
    );
    await expect(rowAgain).toBeVisible();
    // Badge is rendered only when count > 0 — assert absence.
    await expect(
      rowAgain.locator('[data-testid="unread-badge"]'),
    ).toHaveCount(0);
  });
});
