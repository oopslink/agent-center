import { execFile as execFileCb } from "node:child_process";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { test, expect } from "../fixtures/agent-center.js";

const execFile = promisify(execFileCb);
const __filename = fileURLToPath(import.meta.url);
const REPO_ROOT = resolve(__filename, "../../../../..");
const BINARY = resolve(REPO_ROOT, "bin/agent-center");

test.describe("Web↔CLI shared truth", () => {
  test("API-written messages are visible to `agent-center conversation tail`", async ({
    request,
    agentCenter,
  }) => {
    // API creates the channel + sends 2 messages.
    const cR = await request.post(agentCenter.apiURL + "/conversations", {
      data: { kind: "channel", name: "ops-room" },
    });
    expect(cR.status(), `channel: ${await cR.text()}`).toBe(201);
    const channelID = ((await cR.json()) as { conversation_id: string })
      .conversation_id;

    const bodies = ["first message via api", "second message via api"];
    for (const body of bodies) {
      const r = await request.post(
        agentCenter.apiURL + "/conversations/" + channelID + "/messages",
        { data: { content: body } },
      );
      expect(r.status(), `send: ${await r.text()}`).toBe(201);
    }

    // CLI subprocess reads the same DB. This is the canonical
    // "Web ↔ CLI share one source of truth" proof.
    const { stdout } = await execFile(BINARY, [
      "conversation",
      "tail",
      channelID,
      "--tail=10",
      "--format=json",
      "--config",
      agentCenter.configPath,
    ]);
    // CLI `--format=json` emits one JSON object per line.
    const rows = stdout
      .trim()
      .split("\n")
      .filter((s) => s.length > 0)
      .map((s) => JSON.parse(s) as { content: string });

    const seenBodies = rows.map((r) => r.content);
    expect(seenBodies).toEqual(bodies);
  });
});
