import { test, expect } from "../fixtures/agent-center.js";
import { subscribeSSE, type SSEEvent } from "../helpers/sse.js";

interface ParsedMsgEvent {
  raw: SSEEvent;
  data: { conversation_id: string; message_id?: string };
}

function parseMessageEvents(events: SSEEvent[]): ParsedMsgEvent[] {
  const out: ParsedMsgEvent[] = [];
  for (const ev of events) {
    try {
      const parsed = JSON.parse(ev.data) as ParsedMsgEvent["data"] & { event_type?: string };
      if (parsed.event_type !== "conversation.message_added") continue;
      out.push({ raw: ev, data: parsed });
    } catch {
      // ignore malformed
    }
  }
  return out;
}

test.describe("SSE Last-Event-ID recovery", () => {
  test("reconnect with last_event_id receives events sent during the gap", async ({
    request,
    agentCenter,
    authSession,
  }) => {
    // Setup: channel + SSE subscription.
    const cR = await request.post(authSession.orgApiURL + "/conversations", {
      data: { kind: "channel", name: "recovery-room", members: ["agent:recovery"] },
    });
    expect(cR.status(), `channel: ${await cR.text()}`).toBe(201);
    const channelID = ((await cR.json()) as { conversation_id: string })
      .conversation_id;

    const userRef = `user:${authSession.identityID}`;
    const subR = await request.post(agentCenter.apiURL + "/sse/subscribe", {
      data: { user_id: userRef, conversation_id: channelID },
    });
    expect(subR.status()).toBe(200);

    // ---- Phase A: open stream, send 1, capture last event id ----
    const eventsA: SSEEvent[] = [];
    const stopA = await subscribeSSE(
      `${agentCenter.baseURL}/api/sse?user_id=${encodeURIComponent(userRef)}`,
      (ev) => eventsA.push(ev),
      { headers: { Cookie: `ac_session=${authSession.sessionCookie}` } },
    );
    await new Promise((r) => setTimeout(r, 100)); // handshake settle

    const m1 = await request.post(
      authSession.orgApiURL + "/conversations/" + channelID + "/messages",
      { data: { content: "before disconnect" } },
    );
    expect(m1.status()).toBe(201);

    await expect
      .poll(() => parseMessageEvents(eventsA).length, {
        message: "stream A should observe the first message",
      })
      .toBeGreaterThanOrEqual(1);

    const lastEventA = parseMessageEvents(eventsA).at(-1)!.raw.id;
    expect(
      lastEventA,
      "SSE event must carry an id for Last-Event-ID recovery",
    ).toBeTruthy();

    // ---- Disconnect ----
    stopA();
    await new Promise((r) => setTimeout(r, 100)); // let abort settle

    // ---- Phase B: 2 messages sent while disconnected ----
    const m2 = await request.post(
      authSession.orgApiURL + "/conversations/" + channelID + "/messages",
      { data: { content: "during gap 1" } },
    );
    expect(m2.status()).toBe(201);
    const m3 = await request.post(
      authSession.orgApiURL + "/conversations/" + channelID + "/messages",
      { data: { content: "during gap 2" } },
    );
    expect(m3.status()).toBe(201);

    // ---- Phase C: reconnect with last_event_id; expect 2 replays ----
    const eventsB: SSEEvent[] = [];
    const stopB = await subscribeSSE(
      `${agentCenter.baseURL}/api/sse?user_id=${encodeURIComponent(userRef)}&last_event_id=${encodeURIComponent(
        lastEventA!,
      )}`,
      (ev) => eventsB.push(ev),
      { headers: { Cookie: `ac_session=${authSession.sessionCookie}` } },
    );

    try {
      await expect
        .poll(
          () => parseMessageEvents(eventsB).length,
          {
            message:
              "stream B with last_event_id should replay the 2 missed messages",
          },
        )
        .toBeGreaterThanOrEqual(2);

      // None of the replayed events should be the pre-disconnect one.
      const replayed = parseMessageEvents(eventsB);
      const idsB = replayed.map((m) => m.raw.id);
      expect(idsB).not.toContain(lastEventA);
    } finally {
      stopB();
    }
  });
});
