import { test, expect } from "../fixtures/agent-center.js";

test.describe("Web API shared truth", () => {
  test("API-written messages are visible through the conversation read API", async ({
    request,
    authSession,
  }) => {
    // API creates the channel + sends 2 messages.
    const cR = await request.post(authSession.orgApiURL + "/conversations", {
      data: { kind: "channel", name: "ops-room" },
    });
    expect(cR.status(), `channel: ${await cR.text()}`).toBe(201);
    const channelID = ((await cR.json()) as { conversation_id: string })
      .conversation_id;

    const bodies = ["first message via api", "second message via api"];
    for (const body of bodies) {
      const r = await request.post(
        authSession.orgApiURL + "/conversations/" + channelID + "/messages",
        { data: { content: body } },
      );
      expect(r.status(), `send: ${await r.text()}`).toBe(201);
    }

    const read = await request.get(
      authSession.orgApiURL + "/conversations/" + channelID + "/messages",
    );
    expect(read.status(), `read messages: ${await read.text()}`).toBe(200);
    const rows = (await read.json()) as Array<{ content: string }>;
    const seenBodies = rows.map((r) => r.content);
    for (const body of bodies) {
      expect(seenBodies).toContain(body);
    }
  });
});
