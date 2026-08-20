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
    authSession,
  }) => {
    await page.goto(`${agentCenter.baseURL}/organizations/${authSession.orgSlug}/channels`);

    // index.html title is set by vite (see web/index.html).
    await expect(page).toHaveTitle("agent-center");

    await expect(page.getByTestId("page-Channels")).toBeVisible();
  });

  test("API mux + DB respond to /api/conversations", async ({
    request,
    authSession,
  }) => {
    const r = await request.get(authSession.orgApiURL + "/conversations");
    expect(r.status()).toBe(200);
    const body = await r.json();
    // Fresh DB has zero conversations; assert the shape, not the count.
    expect(Array.isArray(body)).toBe(true);
  });
});
