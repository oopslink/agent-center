import type { APIRequestContext } from "@playwright/test";

import { test, expect } from "../fixtures/agent-center.js";

async function createProject(request: APIRequestContext, orgApiURL: string): Promise<string> {
  const r = await request.post(orgApiURL + "/projects", {
    data: { name: "Demo Project", description: "cold-start e2e" },
  });
  expect(r.status(), `project create: ${await r.text()}`).toBe(200);
  const body = (await r.json()) as { id: string };
  expect(body.id).toBeTruthy();
  return body.id;
}

test.describe("cold-start journey (Web Console surfaces)", () => {
  test("secret CRUD round-trip — value never echoed", async ({
    request,
    authSession,
  }) => {
    // initial list
    const r0 = await request.get(authSession.orgApiURL + "/secrets");
    expect(r0.status()).toBe(200);
    expect(await r0.json()).toEqual([]);

    // create
    const r1 = await request.post(authSession.orgApiURL + "/secrets", {
      data: {
        name: "claude-key-1",
        kind: "mcp",
        value: "TOP-SECRET-VALUE-XYZ",
      },
    });
    expect(r1.status()).toBe(201);
    const created = (await r1.json()) as Record<string, unknown>;
    expect(typeof created.id).toBe("string");
    expect(created.name).toBe("claude-key-1");
    // ADR-0026 § 5 — plaintext-never-echo: response MUST NOT carry value
    expect(created).not.toHaveProperty("value");
    expect(JSON.stringify(created)).not.toContain("TOP-SECRET-VALUE-XYZ");

    // list — same no-plaintext guarantee
    const r2 = await request.get(authSession.orgApiURL + "/secrets");
    expect(r2.status()).toBe(200);
    const list = (await r2.json()) as Array<Record<string, unknown>>;
    expect(list).toHaveLength(1);
    expect(list[0].name).toBe("claude-key-1");
    expect(list[0]).not.toHaveProperty("value");
    expect(JSON.stringify(list)).not.toContain("TOP-SECRET-VALUE-XYZ");
  });

  test("channel → messages → create PM issue in project", async ({
    request,
    authSession,
  }) => {
    const projectID = await createProject(request, authSession.orgApiURL);

    // create channel
    const cR = await request.post(authSession.orgApiURL + "/conversations", {
      data: { kind: "channel", name: "design-review" },
    });
    expect(cR.status(), `channel create: ${await cR.text()}`).toBe(201);
    const channel = (await cR.json()) as { conversation_id: string };
    const channelID = channel.conversation_id;

    // send 3 messages
    const sentIDs: string[] = [];
    for (let i = 0; i < 3; i++) {
      const mR = await request.post(
        authSession.orgApiURL + "/conversations/" + channelID + "/messages",
        {
          data: {
            content: `Message ${i + 1} — investigating auth flow`,
          },
        },
      );
      expect(mR.status()).toBe(201);
      const m = (await mR.json()) as { message_id: string };
      sentIDs.push(m.message_id);
    }

    // verify messages persisted
    const mlR = await request.get(
      authSession.orgApiURL + "/conversations/" + channelID + "/messages",
    );
    expect(mlR.status()).toBe(200);
    const msgs = (await mlR.json()) as Array<Record<string, unknown>>;
    expect(msgs.length).toBeGreaterThanOrEqual(3);

    // Create a ProjectManager issue in the project. The legacy flat
    // derive-from-message endpoint is retired; PM issue creation is now nested.
    const dR = await request.post(`${authSession.orgApiURL}/projects/${projectID}/issues`, {
      data: {
        title: "Investigate auth flow",
        description: "Carried over from design-review",
      },
    });
    expect(dR.status(), `issue create: ${await dR.text()}`).toBe(200);
    const derived = (await dR.json()) as {
      id: string;
      project_id: string;
      title: string;
    };
    expect(derived.project_id).toBe(projectID);
    expect(derived.title).toBe("Investigate auth flow");
    expect(sentIDs).toHaveLength(3);
  });

  test("error path: duplicate channel name → 409 already_exists", async ({
    request,
    authSession,
  }) => {
    const create = () =>
      request.post(authSession.orgApiURL + "/conversations", {
        data: { kind: "channel", name: "dupe-name" },
      });

    const first = await create();
    expect(first.status()).toBe(201);

    const second = await create();
    expect(second.status()).toBe(409);
    const body = (await second.json()) as { error?: string; message?: string };
    // writeError serializes the reason as `error` (see api/handlers.go
    // writeError); some external API conventions call this `code` —
    // ours is `error`.
    expect(body.error).toBe("already_exists");
  });
});
