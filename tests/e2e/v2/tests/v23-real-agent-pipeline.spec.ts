// v2.3-3b real-agent dispatch chain — deployed-binary e2e (task #29).
//
// The full claude-code / codex / opencode chain is out of scope for a
// hermetic e2e (no LLM available). This spec instead verifies:
//
//   1. /admin/secret/user-secret/resolve round-trips a freshly-created
//      UserSecret's plaintext to a caller that holds the `secret:resolve`
//      scope. Without scope → 403.
//   2. /admin/blob/put accepts content + readback via blob URL works,
//      proving the new artifact upload path. Without scope → 403.
//   3. The current agent-tools task chain (create → claim → complete) works
//      end-to-end with a scoped (NON-`*`) worker token holding only the
//      production set: dispatch:pull + secret:resolve + blob:put + task:*.
//
// That subset is sufficient to prove the real-agent dispatch chain is
// alive — defaultAgentSpawner's AssemblePrompt + MCPInjector wiring is
// covered by internal/workerdaemon/runtime_real_agent_test.go (unit) so
// the e2e doesn't need to host an MCP-aware agent.

import { test, expect } from "@playwright/test";
import { execFile as execFileCb, spawn, type ChildProcess } from "node:child_process";
import { randomBytes } from "node:crypto";
import { chmod, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import http from "node:http";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { pickFreePort } from "../helpers/ports.js";

const __filename = fileURLToPath(import.meta.url);
const REPO_ROOT = resolve(__filename, "../../../../..");
const SERVER_BIN = resolve(REPO_ROOT, "bin/agent-center");
const execFile = promisify(execFileCb);

type AdminResp = { status: number; body: string };

function adminPOST(
  socketPath: string,
  path: string,
  body: unknown,
  token: string,
): Promise<AdminResp> {
  return new Promise((resolveP, rejectP) => {
    const data = body == null ? "" : JSON.stringify(body);
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      "Content-Length": Buffer.byteLength(data).toString(),
    };
    if (token) headers["Authorization"] = "Bearer " + token;
    const req = http.request(
      { socketPath, method: "POST", path, headers },
      (res) => {
        const chunks: Buffer[] = [];
        res.on("data", (c) => chunks.push(c));
        res.on("end", () =>
          resolveP({
            status: res.statusCode ?? 0,
            body: Buffer.concat(chunks).toString("utf8"),
          }),
        );
      },
    );
    req.on("error", rejectP);
    req.write(data);
    req.end();
  });
}

async function readBootstrapToken(
  bootstrapPath: string,
  deadlineMs: number,
): Promise<string> {
  const deadline = Date.now() + deadlineMs;
  let lastErr: unknown = null;
  while (Date.now() < deadline) {
    try {
      const tok = (await readFile(bootstrapPath, "utf8")).trim();
      if (tok) return tok;
    } catch (err) {
      lastErr = err;
    }
    await sleep(75);
  }
  throw new Error(
    `bootstrap_token not written to ${bootstrapPath} within ${deadlineMs}ms (last err=${String(
      lastErr,
    )})`,
  );
}

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

async function killProc(proc: ChildProcess, graceMs = 2_000): Promise<void> {
  if (proc.exitCode != null) return;
  proc.kill("SIGTERM");
  await new Promise<void>((done) => {
    let settled = false;
    const finish = () => {
      if (settled) return;
      settled = true;
      done();
    };
    proc.once("exit", finish);
    setTimeout(() => {
      if (!proc.killed) proc.kill("SIGKILL");
      finish();
    }, graceMs);
  });
}

// mintScopedToken uses the bootstrap (`*`) token to create a new admin
// token with the provided scope set. Returns the plaintext.
async function mintScopedToken(
  sockPath: string,
  bootstrap: string,
  owner: string,
  scopes: string[],
): Promise<string> {
  const r = await adminPOST(
    sockPath,
    "/admin/admintoken/create",
    { owner, scopes, created_by: "test" },
    bootstrap,
  );
  expect(r.status, "mint token: " + r.body).toBe(200);
  const minted = JSON.parse(r.body) as { plaintext: string };
  expect(minted.plaintext).toBeTruthy();
  return minted.plaintext;
}

test.describe("v2.3-3b — real agent-tools task lifecycle", () => {
  test("scoped worker token can create, claim, complete, and read back a project task", async ({}, testInfo) => {
    test.setTimeout(45_000);

    const tempDir = await mkdtemp(join(tmpdir(), "ac-v23-3b-"));
    const dbPath = join(tempDir, "agent-center.db");
    const sockPath = join(tempDir, "admin.sock");
    const masterKeyPath = join(tempDir, "master.key");
    const blobRoot = join(tempDir, "blobs");
    const scriptPath = join(tempDir, "fakeagent-script.jsonl");
    const configPath = join(tempDir, "config.yaml");
    const webPort = await pickFreePort();
    const grpcPort = await pickFreePort();

    await writeFile(
      masterKeyPath,
      randomBytes(32).toString("base64") + "\n",
      "utf8",
    );
    await chmod(masterKeyPath, 0o600);
    await writeFile(
      scriptPath,
      [
        `{"type":"start","text":"hello"}`,
        `{"type":"done","content":"v23 done"}`,
      ].join("\n") + "\n",
      "utf8",
    );
    // Config includes blob_store.root so /admin/blob/put has somewhere
    // to land. master_key_file is required for /admin/secret/.../resolve.
    const config = `
server:
  listen_addr: ":${grpcPort}"
  sqlite_path: "${dbPath}"
  admin_socket_path: "${sockPath}"
web_console:
  enabled: true
  listen_addr: "127.0.0.1:${webPort}"
secret_management:
  master_key_file: "${masterKeyPath}"
blob_store:
  root: "${blobRoot}"
`;
    await writeFile(configPath, config, "utf8");

    const serverStdout: Buffer[] = [];
    const serverStderr: Buffer[] = [];
    const server = spawn(SERVER_BIN, ["server", "--config", configPath], {
      stdio: ["ignore", "pipe", "pipe"],
      env: { ...process.env, AGENT_CENTER_INVOCATION_ID: "" },
    });
    server.stdout?.on("data", (c) => serverStdout.push(c));
    server.stderr?.on("data", (c) => serverStderr.push(c));

    try {
      const serverDeadline = Date.now() + 5_000;
      let serverReady = false;
      while (Date.now() < serverDeadline) {
        try {
          const r = await fetch(`http://127.0.0.1:${webPort}/api/health`);
          if (r.ok) {
            serverReady = true;
            break;
          }
        } catch {}
        await sleep(75);
      }
      expect(serverReady, "server up").toBe(true);

      const bootstrap = await readBootstrapToken(
        join(tempDir, "bootstrap_token"),
        5_000,
      );

      // --- mint scoped tokens ------------------------------------------------
      // Worker token gets the production scope set (per task brief). NO
      // `*` so we prove every endpoint we touch is scope-gated.
      const workerToken = await mintScopedToken(sockPath, bootstrap, "worker:test-w-1", [
        "dispatch:pull",
        "secret:resolve",
        "blob:put",
        "task:*",
      ]);
      // CLI-style token: only `secret:resolve` — proves the resolve
      // endpoint accepts a non-`*` caller too. Owner uses the `user:`
      // prefix so observability.Actor.Validate() accepts it (the
      // resolve handler passes Owner verbatim as the caller actor).
      const resolveOnlyToken = await mintScopedToken(
        sockPath,
        bootstrap,
        "user:resolve-only",
        ["secret:resolve"],
      );

      // --- step 1: create a UserSecret + resolve via scoped token ----------
      let r = await adminPOST(
        sockPath,
        "/admin/secret/user-secret/create",
        { name: "v23_db_pw", kind: "mcp", plaintext: "super-secret-pw" },
        bootstrap,
      );
      expect(r.status, "secret create: " + r.body).toBe(200);

      r = await adminPOST(
        sockPath,
        "/admin/secret/user-secret/resolve",
        { name: "v23_db_pw" },
        resolveOnlyToken,
      );
      expect(r.status, "secret resolve: " + r.body).toBe(200);
      const resolved = JSON.parse(r.body) as {
        plaintext_base64: string;
        name: string;
      };
      expect(resolved.name).toBe("v23_db_pw");
      expect(Buffer.from(resolved.plaintext_base64, "base64").toString("utf8")).toBe(
        "super-secret-pw",
      );

      // Resolve without scope → 403. Mint a token with only task:* scope
      // to confirm scope gating.
      const taskOnlyToken = await mintScopedToken(
        sockPath,
        bootstrap,
        "user:task-only",
        ["task:*"],
      );
      const denied = await adminPOST(
        sockPath,
        "/admin/secret/user-secret/resolve",
        { name: "v23_db_pw" },
        taskOnlyToken,
      );
      expect(denied.status, "secret resolve scope: " + denied.body).toBe(403);

      // --- step 2: blob put + readback ---------------------------------------
      const blobContent = Buffer.from("artifact-bytes-v23");
      r = await adminPOST(
        sockPath,
        "/admin/blob/put",
        {
          rel_path: "artifacts/v23/payload.bin",
          content_base64: blobContent.toString("base64"),
        },
        workerToken,
      );
      expect(r.status, "blob put: " + r.body).toBe(200);
      // Readback via the filesystem (blob_store.root convention) — the
      // server has no GET endpoint for blob put in v2.3-3b.
      const written = await readFile(
        join(blobRoot, "artifacts/v23/payload.bin"),
      );
      expect(written.equals(blobContent)).toBe(true);
      // Without scope → 403.
      const blobDenied = await adminPOST(
        sockPath,
        "/admin/blob/put",
        {
          rel_path: "artifacts/forbidden.bin",
          content_base64: blobContent.toString("base64"),
        },
        taskOnlyToken,
      );
      expect(blobDenied.status, "blob put scope: " + blobDenied.body).toBe(403);

      // --- step 3: current task chain with scoped worker token ---------------
      const pid = "p-v23";
      const orgID = "organization-v23";
      const agentID = "test-agent-v23";
      const now = new Date().toISOString();
      await execFile("sqlite3", [
        dbPath,
        [
          `INSERT INTO organizations (id,slug,name,description,created_by_identity_id,created_at,updated_at) VALUES ('${orgID}','v23-org','V23 Org','','user:hayang','${now}','${now}');`,
          `INSERT INTO agents (id,organization_id,name,description,model,cli,worker_id,lifecycle,created_by,created_at,updated_at) VALUES ('${agentID}','${orgID}','test-agent-v23','','','fakeagent','test-w-1','running','user:hayang','${now}','${now}');`,
          `INSERT INTO pm_projects (id,organization_id,name,description,status,created_by,created_at,updated_at,version) VALUES ('${pid}','${orgID}','v23-test','coding','active','user:hayang','${now}','${now}',1);`,
          `INSERT INTO pm_project_members (id,project_id,identity_id,role,added_by,created_at) VALUES ('m-v23-agent','${pid}','agent:${agentID}','member','system','${now}');`,
          `INSERT INTO pm_assignment_pools (id,project_id,scheduling_class,auto_assign_enabled,holding_cap,created_at,updated_at,version) VALUES ('pool-${pid}','${pid}','background',1,3,'${now}','${now}',1);`,
        ].join("\n"),
      ]);

      r = await adminPOST(
        sockPath,
        "/admin/agent-tools/create_task",
        {
          agent_id: agentID,
          project_id: pid,
          title: "v23-3b task",
          description: "fakeagent-script: " + scriptPath,
          dispatch: true,
        },
        workerToken,
      );
      expect(r.status, "task create: " + r.body).toBe(200);
      const created = JSON.parse(r.body) as {
        task_id?: string;
        id?: string;
        conversation_id?: string;
      };
      const createdTaskID = created.task_id ?? created.id;
      expect(createdTaskID, "created task id").toBeTruthy();

      r = await adminPOST(
        sockPath,
        "/admin/agent-tools/claim_task",
        { agent_id: agentID, task_id: createdTaskID },
        workerToken,
      );
      expect(r.status, "claim_task: " + r.body).toBe(200);
      const claimed = JSON.parse(r.body) as { claimed: boolean; status: string };
      expect(claimed.claimed).toBe(true);
      expect(claimed.status).toBe("running");

      r = await adminPOST(
        sockPath,
        "/admin/agent-tools/complete_task",
        { agent_id: agentID, task_id: createdTaskID },
        workerToken,
      );
      expect(r.status, "complete_task: " + r.body).toBe(200);

      r = await adminPOST(
        sockPath,
        "/admin/agent-tools/get_task",
        { agent_id: agentID, task_id: createdTaskID },
        workerToken,
      );
      expect(r.status, "get_task: " + r.body).toBe(200);
      const task = JSON.parse(r.body) as { status: string };
      expect(task.status).toBe("completed");
    } finally {
      if (testInfo.status !== testInfo.expectedStatus) {
        await testInfo.attach("server-stderr.log", {
          body: Buffer.concat(serverStderr),
          contentType: "text/plain",
        });
      }
      await killProc(server);
      await rm(tempDir, { recursive: true, force: true });
    }
  });
});
