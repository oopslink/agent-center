import { test, expect } from "../fixtures/agent-center.js";
import { subscribeSSE, type SSEEvent } from "../helpers/sse.js";

test.describe("DM flow", () => {
  test("happy path: create DM → send msg → SSE delivers conversation.message_added", async ({
    request,
    agentCenter,
  }) => {
    // Create DM with one extra peer (owner = user:hayang via fixture
    // default; member = user:peer-alice). The fixture's default actor
    // becomes the implicit owner.
    const cR = await request.post(agentCenter.apiURL + "/conversations", {
      data: { kind: "dm", members: ["user:peer-alice"], name: "alice" },
    });
    expect(cR.status(), `dm create: ${await cR.text()}`).toBe(201);
    const dm = (await cR.json()) as { conversation_id: string };
    const dmID = dm.conversation_id;

    // Subscribe user:hayang's SSE stream to this DM. The subscribe
    // endpoint is what tells the bus to fan events for this user.
    const subR = await request.post(agentCenter.apiURL + "/sse/subscribe", {
      data: { user_id: "user:hayang", conversation_id: dmID },
    });
    expect(subR.status(), `subscribe: ${await subR.text()}`).toBe(200);

    // Open the actual SSE stream BEFORE we send the message so we
    // don't miss the event. The bus delivers events to currently
    // connected sessions.
    const events: SSEEvent[] = [];
    const stop = await subscribeSSE(
      `${agentCenter.baseURL}/api/sse?user_id=user:hayang`,
      (ev) => events.push(ev),
    );

    try {
      // small await to let the SSE handshake settle before we trigger
      // an event. Without this, fast servers can complete the POST
      // (and thus the event emit) before the fetch's stream is open.
      await new Promise((r) => setTimeout(r, 100));

      // Send a message — fires conversation.message_added.
      const mR = await request.post(
        agentCenter.apiURL + "/conversations/" + dmID + "/messages",
        { data: { content: "hi alice — checking sse" } },
      );
      expect(mR.status(), `send msg: ${await mR.text()}`).toBe(201);

      // Wait (with auto-retry) for the message_added event to land in
      // our captured stream. expect.poll retries until actionTimeout.
      await expect
        .poll(
          () =>
            events.some((ev) => {
              if (ev.event !== "conversation.message_added") return false;
              try {
                const data = JSON.parse(ev.data) as Record<string, unknown>;
                return data.conversation_id === dmID;
              } catch {
                return false;
              }
            }),
          { message: "expected SSE event conversation.message_added for the DM" },
        )
        .toBe(true);
    } finally {
      stop();
    }

    // Confirm via REST too — message landed in the DM.
    const lR = await request.get(
      agentCenter.apiURL + "/conversations/" + dmID + "/messages",
    );
    expect(lR.status()).toBe(200);
    const msgs = (await lR.json()) as Array<Record<string, unknown>>;
    expect(msgs.length).toBe(1);
    expect(msgs[0].content).toBe("hi alice — checking sse");
  });

  test("error path: DM create without members → 400 invalid_input", async ({
    request,
    agentCenter,
  }) => {
    const r = await request.post(agentCenter.apiURL + "/conversations", {
      data: { kind: "dm" },
    });
    expect(r.status()).toBe(400);
    const body = (await r.json()) as { error?: string };
    expect(body.error).toBe("invalid_input");
  });
});
