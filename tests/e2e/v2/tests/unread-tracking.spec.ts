import { test, expect } from "../fixtures/agent-center.js";

// v2.1-C-3 — unread tracking end-to-end.
//
// Cold-start journey:
//   1. Seed a channel + messages via the API.
//   2. Open /channels and assert the row is visible.
//   3. Open the channel → auto-mark-seen fires without introducing a badge for
//      messages authored by the current user.
test.describe("Unread tracking", () => {
  test("current-user channel messages do not create unread badges", async ({
    page,
    request,
    agentCenter,
    authSession,
  }) => {
    const channelName = "unread-room-" + Math.random().toString(36).slice(2, 8);
    const cR = await request.post(authSession.orgApiURL + "/conversations", {
      data: { kind: "channel", name: channelName },
    });
    expect(cR.status()).toBe(201);
    const channelID = ((await cR.json()) as { conversation_id: string })
      .conversation_id;

    // Seed 3 messages — these accumulate as unread since the
    // user_conversation_read_state row is absent.
    for (let i = 0; i < 3; i++) {
      const r = await request.post(
        authSession.orgApiURL + "/conversations/" + channelID + "/messages",
        { data: { content: "unread msg " + (i + 1) } },
      );
      expect(r.status()).toBe(201);
    }

    // Channel list shows the seeded channel. Messages authored by the current
    // web user are not counted as unread for that same user.
    await page.goto(`${agentCenter.baseURL}/organizations/${authSession.orgSlug}/channels`);
    const row = page.locator(
      `[data-testid="channel-row"][data-channel-name="${channelName}"]`,
    );
    await expect(row).toBeVisible();
    await expect(row.locator('[data-testid="unread-badge"]')).toHaveCount(0);

    // Visit the channel → auto-mark-seen fires on mount.
    await row.locator("a").click();
    await expect(page.locator('[data-testid="page-ChannelDetail"]')).toBeVisible();
    // Wait for messages to render so the auto-mark-seen effect has run.
    await expect(page.locator('[data-testid="message-row"]')).toHaveCount(3);

    // Back to channels — the badge should remain absent.
    await page.goto(`${agentCenter.baseURL}/organizations/${authSession.orgSlug}/channels`);
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
