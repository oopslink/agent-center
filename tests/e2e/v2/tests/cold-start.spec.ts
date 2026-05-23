import { execFile as execFileCb } from "node:child_process";
import { randomUUID } from "node:crypto";
import { promisify } from "node:util";

import { test, expect } from "../fixtures/agent-center.js";

const execFile = promisify(execFileCb);

// Seed a project via direct sqlite INSERT. We deliberately bypass the
// `agent-center project add` CLI because that subprocess emits a
// domain event with its own seq counter; running it concurrently with
// the live server race-conflicts on events.uniq_events_seq. Direct
// INSERT into the `projects` table skips the event sink (projects
// don't need an event for derive-issue to find them — the projects
// table is the truth) and stays race-free.
async function seedProject(dbPath: string): Promise<string> {
  const id = `p-demo-${randomUUID().slice(0, 8)}`;
  const now = new Date().toISOString();
  const sql = `INSERT INTO projects
    (id, name, created_at, updated_at, created_by_identity_id, version)
    VALUES ('${id}', 'Demo ${id}', '${now}', '${now}', 'user:hayang', 1);`;
  await execFile("sqlite3", [dbPath, sql]);
  return id;
}

test.describe("cold-start journey (Web Console surfaces)", () => {
  test("secret CRUD round-trip — value never echoed", async ({
    request,
    agentCenter,
  }) => {
    // initial list
    const r0 = await request.get(agentCenter.apiURL + "/secrets");
    expect(r0.status()).toBe(200);
    expect(await r0.json()).toEqual([]);

    // create
    const r1 = await request.post(agentCenter.apiURL + "/secrets", {
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
    const r2 = await request.get(agentCenter.apiURL + "/secrets");
    expect(r2.status()).toBe(200);
    const list = (await r2.json()) as Array<Record<string, unknown>>;
    expect(list).toHaveLength(1);
    expect(list[0].name).toBe("claude-key-1");
    expect(list[0]).not.toHaveProperty("value");
    expect(JSON.stringify(list)).not.toContain("TOP-SECRET-VALUE-XYZ");
  });

  test("channel → messages → derive issue → refs link source messages", async ({
    request,
    agentCenter,
  }) => {
    const projectID = await seedProject(agentCenter.dbPath);

    // create channel
    const cR = await request.post(agentCenter.apiURL + "/conversations", {
      data: { kind: "channel", name: "design-review" },
    });
    expect(cR.status(), `channel create: ${await cR.text()}`).toBe(201);
    const channel = (await cR.json()) as { conversation_id: string };
    const channelID = channel.conversation_id;

    // send 3 messages
    const sentIDs: string[] = [];
    for (let i = 0; i < 3; i++) {
      const mR = await request.post(
        agentCenter.apiURL + "/conversations/" + channelID + "/messages",
        {
          data: {
            sender_identity_id: "user:hayang",
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
      agentCenter.apiURL + "/conversations/" + channelID + "/messages",
    );
    expect(mlR.status()).toBe(200);
    const msgs = (await mlR.json()) as Array<Record<string, unknown>>;
    expect(msgs.length).toBeGreaterThanOrEqual(3);

    // derive Issue from first 2 messages
    const dR = await request.post(agentCenter.apiURL + "/issues", {
      data: {
        source_conversation_id: channelID,
        source_message_ids: sentIDs.slice(0, 2),
        project_id: projectID,
        title: "Investigate auth flow",
        description: "Carried over from design-review",
      },
    });
    expect(dR.status()).toBe(201);
    const derived = (await dR.json()) as {
      issue_id: string;
      conversation_id: string;
      reference_count: number;
    };
    expect(derived.reference_count).toBe(2);
    expect(derived.conversation_id).not.toBe(channelID);

    // refs of the new issue conversation point back to the source
    const refsR = await request.get(
      agentCenter.apiURL + "/conversations/" + derived.conversation_id + "/refs",
    );
    expect(refsR.status()).toBe(200);
    const refs = (await refsR.json()) as Array<{ source_message_id: string }>;
    const refIDs = refs.map((r) => r.source_message_id).sort();
    expect(refIDs).toEqual(sentIDs.slice(0, 2).sort());
  });

  test("error path: duplicate channel name → 409 already_exists", async ({
    request,
    agentCenter,
  }) => {
    const create = () =>
      request.post(agentCenter.apiURL + "/conversations", {
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
