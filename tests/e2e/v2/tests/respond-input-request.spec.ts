import { execFile as execFileCb } from "node:child_process";
import { randomUUID } from "node:crypto";
import { promisify } from "node:util";

import { test, expect } from "../fixtures/agent-center.js";

const execFile = promisify(execFileCb);

// Seed a project/task/task_execution/IR chain via direct sqlite
// INSERT — per the S9 codified rule ("never CLI subprocess while
// server runs"). Each seed call uses fresh randomized IDs so tests
// stay independent under parallel workers.
async function seedIRChain(dbPath: string): Promise<{
  projectID: string;
  taskID: string;
  executionID: string;
  irID: string;
}> {
  const projectID = `p-ir-${randomUUID().slice(0, 8)}`;
  const taskID = `T-${randomUUID().slice(0, 8)}`;
  const executionID = `E-${randomUUID().slice(0, 8)}`;
  const irID = `IR-${randomUUID().slice(0, 8)}`;
  const now = new Date().toISOString();

  const sql = `
    INSERT INTO projects (id, name, created_at, updated_at, created_by_identity_id, version)
      VALUES ('${projectID}', 'IR Demo', '${now}', '${now}', 'user:hayang', 1);
    INSERT INTO tasks (id, project_id, title, status, priority, requires_worktree, created_by, created_at, updated_at, version)
      VALUES ('${taskID}', '${projectID}', 'Q1 audit', 'open', 'medium', 1, 'user:hayang', '${now}', '${now}', 1);
    INSERT INTO task_executions
      (id, task_id, worker_id, agent_cli, workspace_mode, status, started_at, working_started_at, created_at, updated_at, version)
      VALUES ('${executionID}', '${taskID}', 'W-1', 'claudecode', 'worktree', 'input_required', '${now}', '${now}', '${now}', '${now}', 1);
    INSERT INTO input_requests
      (id, task_execution_id, status, question, options, urgency, requested_at, created_at, updated_at, version)
      VALUES ('${irID}', '${executionID}', 'pending', 'Approve audit scope?', '[]', 'normal', '${now}', '${now}', '${now}', 1);
  `;
  await execFile("sqlite3", [dbPath, sql]);
  return { projectID, taskID, executionID, irID };
}

async function readIRStatus(dbPath: string, irID: string): Promise<string> {
  const { stdout } = await execFile("sqlite3", [
    dbPath,
    `SELECT status FROM input_requests WHERE id='${irID}';`,
  ]);
  return stdout.trim();
}

async function readExecStatus(
  dbPath: string,
  executionID: string,
): Promise<string> {
  const { stdout } = await execFile("sqlite3", [
    dbPath,
    `SELECT status FROM task_executions WHERE id='${executionID}';`,
  ]);
  return stdout.trim();
}

test.describe("input-request respond", () => {
  test("happy path: pending IR → respond → status flips + exec leaves input_required", async ({
    request,
    agentCenter,
  }) => {
    const { executionID, irID } = await seedIRChain(agentCenter.dbPath);

    // visible in pending list
    const lR = await request.get(agentCenter.apiURL + "/input_requests");
    expect(lR.status()).toBe(200);
    const list = (await lR.json()) as Array<Record<string, unknown>>;
    const me = list.find((row) => row.id === irID);
    expect(me, `IR ${irID} should be in pending list`).toBeDefined();
    expect(me?.status).toBe("pending");
    expect(me?.question).toBe("Approve audit scope?");

    // respond
    const rR = await request.post(
      agentCenter.apiURL + "/input_requests/" + irID + "/respond",
      {
        data: { answer: "yes — proceed", decided_by: "user:hayang" },
      },
    );
    expect(rR.status(), `respond: ${await rR.text()}`).toBe(200);
    expect(await rR.json()).toEqual({ answered: true });

    // pending list no longer shows it
    const l2 = await request.get(agentCenter.apiURL + "/input_requests");
    const list2 = (await l2.json()) as Array<Record<string, unknown>>;
    expect(list2.find((row) => row.id === irID)).toBeUndefined();

    // DB state proof
    expect(await readIRStatus(agentCenter.dbPath, irID)).toBe("responded");
    // execution.status: input_required → working (LeaveInputRequired)
    expect(await readExecStatus(agentCenter.dbPath, executionID)).toBe(
      "working",
    );
  });

  test("error path: respond to nonexistent IR → 404 not_found", async ({
    request,
    agentCenter,
  }) => {
    const r = await request.post(
      agentCenter.apiURL + "/input_requests/IR-does-not-exist/respond",
      { data: { answer: "x", decided_by: "user:hayang" } },
    );
    expect(r.status()).toBe(404);
    const body = (await r.json()) as { error?: string };
    expect(body.error).toBe("not_found");
  });
});
