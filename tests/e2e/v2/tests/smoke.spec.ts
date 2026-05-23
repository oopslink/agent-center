import { test, expect } from "../fixtures/agent-center.js";

// S8 smoke: prove the scaffold works end-to-end:
// 1. binary starts on a temp port
// 2. SPA shell HTML serves at /
// 3. React mounts the AppLayout (nav with the 'Channels' link)
// 4. an XHR to /api/conversations succeeds — wires the api mux + DB

test.describe("smoke", () => {
  test("SPA loads and Channels nav link is visible", async ({
    page,
    agentCenter,
  }) => {
    await page.goto(agentCenter.baseURL + "/");

    // index.html title is set by vite (see web/index.html).
    await expect(page).toHaveTitle("agent-center");

    // The AppLayout renders a left nav; first link is 'Channels'.
    // Use a role-based locator so it survives styling changes.
    const channelsLink = page.getByRole("link", { name: "Channels" });
    await expect(channelsLink).toBeVisible();
  });

  test("API mux + DB respond to /api/conversations", async ({
    request,
    agentCenter,
  }) => {
    const r = await request.get(agentCenter.apiURL + "/conversations");
    expect(r.status()).toBe(200);
    const body = await r.json();
    // Fresh DB has zero conversations; assert the shape, not the count.
    expect(Array.isArray(body)).toBe(true);
  });
});
