// Deployed-binary smoke — the canonical `make smoke` gate (conventions § 0.4
// enforce mechanism #4; testing.md § 2.3 "deployed-smoke count ≥ 1").
//
// WHAT THIS PROVES (against FRESHLY-BUILT binaries, over the REAL admin unix
// socket — no in-process shortcuts, no real LLM):
//
//   1. bin/agent-center server boots on the CURRENT config schema and mints a
//      bootstrap token. (Pins the v2.7 #162 regression class: a removed config
//      key like the old `identity.default_user` makes the server refuse to boot
//      — `config: unknown YAML key …` — which silently disabled this gate.)
//   2. bin/agent-center worker run (the deployed worker binary) boots, probes
//      its agent CLIs, enrolls over the admin socket, and reaches `online`.
//   3. The CURRENT admin route surface answers (/admin/workforce/worker/*,
//      /admin/agent-tools/*) AND the retired v2.2/v2.3 routes are gone (404) —
//      so a future rewrite can't silently regress onto stale endpoints.
//   4. The task-dispatch pipeline closes at the control plane: a worker-token +
//      worker-bound agent creates a task via agent-tools, it is DISPATCHED into
//      the project's built-in assignment pool (ADR-0047), shows up in the agent's
//      list_tasks, and the agent CLAIMS it (open → running).
//
// SCOPE / WHY NO "task → completed" + no real agent subprocess (T212):
//   The original v2.2 design drove `cmd/fakeagent` through the worker to close
//   task → done LLM-free. That execution path was RETIRED in the v2.7
//   supervisor/claude-stream architecture: the worker only spawns real
//   claude/codex supervisor sessions (claude-stream-json), the `--fake-agent`
//   override is dead (RuntimeConfig.AgentCLIOverrides is never read), fakeagent's
//   ad-hoc JSONL is parsed as `unknown` (no task effect), and a pull-pool claimed
//   task has NO WorkItem so `complete_task` (requireOwnTask) cannot terminate it.
//   Re-enabling a real LLM-free task→completed pipeline needs NEW code (a fake
//   that speaks claude-stream-json + plumbed ClaudeBin override + a completion
//   hook) — tracked as a separate feature ("Option B"), NOT this test-debt fix.
//   So this smoke deliberately drives the deploy-critical surface up through the
//   agent CLAIMING dispatched work (open → running); the agent's own subprocess
//   execution is out of scope here.
//
// Seeding note: the v2.7 Agent BC, the project, its members and the built-in
// assignment pool are seeded by direct sqlite INSERT (the established e2e
// shortcut — see cold-start.spec.ts). There is no admin route to create a v2.7
// Agent (only /api/members/agent on the web console, which needs a JWT session),
// and a raw `pm_projects` INSERT lacks the ADR-0047 built-in pool, so the pool
// row is seeded too. The DISPATCH + CLAIM themselves go over the REAL routes.

import { test, expect } from "@playwright/test";
import { execFile as execFileCb, spawn, type ChildProcess } from "node:child_process";
import { randomBytes } from "node:crypto";
import { chmod, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import http from "node:http";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";

import { pickFreePort } from "../helpers/ports.js";

const execFile = promisify(execFileCb);

const __filename = fileURLToPath(import.meta.url);
const REPO_ROOT = resolve(__filename, "../../../../..");
const SERVER_BIN = resolve(REPO_ROOT, "bin/agent-center");
// v2.7 (b): the worker runs as the unified `agent-center worker run` (the
// standalone agent-center-worker-daemon is retired).
const WORKER_BIN = resolve(REPO_ROOT, "bin/agent-center");
const FAKEAGENT_BIN = resolve(REPO_ROOT, "bin/fakeagent");

// adminPOST issues an HTTP POST over the admin unix socket with a bearer token.
function adminPOST(
  socketPath: string,
  path: string,
  body: unknown,
  token: string,
): Promise<{ status: number; body: string }> {
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

// adminGET issues an HTTP GET over the admin unix socket with a bearer token.
function adminGET(
  socketPath: string,
  path: string,
  token: string,
): Promise<{ status: number; body: string }> {
  return new Promise((resolveP, rejectP) => {
    const headers: Record<string, string> = {};
    if (token) headers["Authorization"] = "Bearer " + token;
    const req = http.request(
      { socketPath, method: "GET", path, headers },
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
    req.end();
  });
}

async function waitAdminGET(
  socketPath: string,
  path: string,
  token: string,
  deadlineMs: number,
): Promise<{ status: number; body: string }> {
  const deadline = Date.now() + deadlineMs;
  let lastErr: unknown = null;
  while (Date.now() < deadline) {
    try {
      return await adminGET(socketPath, path, token);
    } catch (err) {
      lastErr = err;
      await sleep(75);
    }
  }
  throw new Error(`admin socket not ready within ${deadlineMs}ms (last err=${String(lastErr)})`);
}

// readBootstrapToken waits for the server to write <sqlite_dir>/bootstrap_token
// (server-side EnsureBootstrapToken at boot) and returns the trimmed plaintext.
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
    `bootstrap_token not written to ${bootstrapPath} within ${deadlineMs}ms (last err=${String(lastErr)})`,
  );
}

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

// killProc SIGTERM + force-kill after grace.
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

test.describe("deployed-binary smoke — control-plane task-dispatch pipeline", () => {
  test("server + worker enroll + agent-tools dispatch → claim (open → running)", async ({}, testInfo) => {
    test.setTimeout(45_000);

    // --- temp scaffolding -------------------------------------------------
    const tempDir = await mkdtemp(join(tmpdir(), "ac-v22d-"));
    const dbPath = join(tempDir, "agent-center.db");
    const sockPath = join(tempDir, "admin.sock");
    const masterKeyPath = join(tempDir, "master.key");
    const configPath = join(tempDir, "config.yaml");
    const webPort = await pickFreePort();
    const grpcPort = await pickFreePort();

    // Master key — the webconsole JWT signing key. The server REFUSES to boot
    // with the webconsole enabled unless secret_management.master_key_file is set.
    await writeFile(masterKeyPath, randomBytes(32).toString("base64") + "\n", "utf8");
    await chmod(masterKeyPath, 0o600);

    // CURRENT config schema — NO `identity:` block (removed in v2.7 #162; its
    // presence is exactly what broke this gate). If a future schema change drops
    // another key the smoke writes, the server won't boot and this test fails —
    // which is the point.
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
`;
    await writeFile(configPath, config, "utf8");

    const serverStdout: Buffer[] = [];
    const serverStderr: Buffer[] = [];
    const workerStdout: Buffer[] = [];
    const workerStderr: Buffer[] = [];
    let worker: ChildProcess | null = null;

    // --- PHASE 1: server boots on the current config schema ----------------
    const server = spawn(SERVER_BIN, ["server", "--config", configPath], {
      stdio: ["ignore", "pipe", "pipe"],
      env: { ...process.env, AGENT_CENTER_INVOCATION_ID: "" },
    });
    server.stdout?.on("data", (c) => serverStdout.push(c));
    server.stderr?.on("data", (c) => serverStderr.push(c));

    try {
      // The bootstrap token file appearing proves the server booted far enough
      // to open SQLite, run migrations, and mint the `*` admin token.
      const bootstrapPath = join(tempDir, "bootstrap_token");
      let adminToken = "";
      try {
        adminToken = await readBootstrapToken(bootstrapPath, 8_000);
      } catch (e) {
        throw new Error(
          String(e) +
            "\n--- server stderr ---\n" +
            Buffer.concat(serverStderr).toString("utf8").slice(-1500),
        );
      }
      expect(adminToken, "bootstrap token plaintext").toBeTruthy();

      // admin /health over the socket — server is serving the admin endpoint.
      let health: { status: number; body: string };
      try {
        health = await waitAdminGET(sockPath, "/admin/health", adminToken, 8_000);
      } catch (e) {
        throw new Error(
          String(e) +
            "\n--- server stderr ---\n" +
            Buffer.concat(serverStderr).toString("utf8").slice(-1500),
        );
      }
      expect(health.status, "admin /health: " + health.body).toBe(200);

      // --- PHASE 2: the worker BINARY boots + enrolls over the socket -------
      worker = spawn(
        WORKER_BIN,
        [
          "worker",
          "run",
          "--config",
          configPath,
          "--worker-id",
          "smoke-run-w",
          "--worker-name",
          "smoke run worker",
          "--admin-target",
          "unix:" + sockPath,
          "--admin-token",
          adminToken,
          "--fake-agent",
          FAKEAGENT_BIN,
        ],
        {
          stdio: ["ignore", "pipe", "pipe"],
          env: { ...process.env, AGENT_CENTER_INVOCATION_ID: "" },
        },
      );
      worker.stdout?.on("data", (c) => workerStdout.push(c));
      worker.stderr?.on("data", (c) => workerStderr.push(c));

      // Poll the operator read route until the worker is enrolled + online.
      let workerOnline = false;
      const enrollDeadline = Date.now() + 12_000;
      while (Date.now() < enrollDeadline) {
        const r = await adminGET(sockPath, "/admin/workforce/worker/find-all", adminToken);
        if (r.status === 200 && r.body.includes("smoke-run-w")) {
          const list = JSON.parse(r.body) as Array<{ worker_id: string; status: string }>;
          const w = list.find((x) => x.worker_id === "smoke-run-w");
          if (w && w.status === "online") {
            workerOnline = true;
            break;
          }
        }
        await sleep(200);
      }
      expect(
        workerOnline,
        "worker binary enroll (worker stderr tail: " +
          Buffer.concat(workerStderr).toString("utf8").slice(-1200) +
          ")",
      ).toBe(true);

      // The worker's control loop needs an org-install to CONNECT (409
      // worker_not_org_enrolled otherwise); that is out of this smoke's scope —
      // enroll + online over the real socket is the deployed-worker signal.

      // --- PHASE 3: current route surface answers; stale routes are gone ----
      for (const stale of [
        "/admin/taskruntime/task/create", // v2.2/v2.3 — removed
        "/admin/workforce/project/add", // v2.2/v2.3 — removed
      ]) {
        const r = await adminPOST(sockPath, stale, {}, adminToken);
        expect(r.status, `stale route ${stale} must be 404`).toBe(404);
      }

      // --- PHASE 4: task-dispatch pipeline (control plane, LLM-free) --------
      // Enroll a SECOND worker dedicated to the dispatch flow and capture its
      // long-term worker token (agent-tools require a worker:<id> bearer + the
      // target agent bound to that worker).
      const enroll = await adminPOST(
        sockPath,
        "/admin/workforce/worker/enroll",
        { worker_id: "smoke-ctl-w", name: "smoke ctl worker", capabilities: ["fakeagent"] },
        adminToken,
      );
      expect(enroll.status, "worker enroll: " + enroll.body).toBe(200);
      const workerToken = (JSON.parse(enroll.body) as { admin_token: string }).admin_token;
      expect(workerToken, "minted worker token").toBeTruthy();

      // Seed (sqlite): org + Agent (bound to smoke-ctl-w) + project + membership
      // + the project's built-in assignment pool plan (ADR-0047).
      const now = new Date().toISOString();
      const orgID = "organization-smoke01";
      const agentID = "smoke-agent-0001"; // raw Agent BC id (identity = agent:<id>)
      const projectID = "p-smoke";
      const seedSQL = [
        `INSERT INTO organizations (id,slug,name,description,created_by_identity_id,created_at,updated_at) VALUES ('${orgID}','smoke-org','Smoke Org','','user:hayang','${now}','${now}');`,
        `INSERT INTO agents (id,organization_id,name,description,model,cli,worker_id,lifecycle,created_by,created_at,updated_at) VALUES ('${agentID}','${orgID}','smoke-agent','','','fakeagent','smoke-ctl-w','running','user:hayang','${now}','${now}');`,
        `INSERT INTO pm_projects (id,organization_id,name,description,status,created_by,created_at,updated_at,version) VALUES ('${projectID}','${orgID}','Smoke Project','smoke','active','user:hayang','${now}','${now}',1);`,
        `INSERT INTO pm_project_members (id,project_id,identity_id,role,added_by,created_at) VALUES ('m-smoke','${projectID}','agent:${agentID}','member','system','${now}');`,
        `INSERT INTO pm_plans (id,project_id,name,description,status,creator_ref,conversation_id,target_date,is_builtin,created_at,updated_at,version) VALUES ('plan-builtin-${projectID}','${projectID}','[Built-in]','','running','system','','',1,'${now}','${now}',1);`,
      ].join("\n");
      await execFile("sqlite3", [dbPath, seedSQL]);

      // create_task over the REAL agent-tools route (worker token + bound agent),
      // dispatched (unassigned) into the built-in pull pool.
      const create = await adminPOST(
        sockPath,
        "/admin/agent-tools/create_task",
        {
          agent_id: agentID,
          project_id: projectID,
          title: "smoke task",
          description: "deployed control-plane smoke",
          dispatch: true,
        },
        workerToken,
      );
      expect(create.status, "create_task: " + create.body).toBe(200);
      const taskID = (JSON.parse(create.body) as { task_id: string }).task_id;
      expect(taskID, "created task id").toBeTruthy();

      // list_tasks reflects the open assignment-pool task. get_my_work was retired
      // with AgentWorkItem; claim_task is the authoritative claimability check.
      const list = await adminPOST(
        sockPath,
        "/admin/agent-tools/list_tasks",
        { agent_id: agentID, project_id: projectID },
        workerToken,
      );
      expect(list.status, "list_tasks: " + list.body).toBe(200);
      expect(
        (JSON.parse(list.body) as { tasks: Array<{ id: string }> }).tasks.some((t) => t.id === taskID),
        "task present in list_tasks",
      ).toBe(true);

      // The agent CLAIMS the dispatched task: open → running. This is the
      // real pull-pool claim path (ClaimPoolTask) over the deployed socket.
      const claim = await adminPOST(
        sockPath,
        "/admin/agent-tools/claim_task",
        { agent_id: agentID, task_id: taskID },
        workerToken,
      );
      expect(claim.status, "claim_task: " + claim.body).toBe(200);
      const claimed = JSON.parse(claim.body) as { claimed: boolean; status: string };
      expect(claimed.claimed, "claim_task claimed=true").toBe(true);
      expect(claimed.status, "task status after claim").toBe("running");
    } finally {
      // Diagnostics on failure for triage.
      if (testInfo.status !== testInfo.expectedStatus) {
        await testInfo.attach("server-stderr.log", {
          body: Buffer.concat(serverStderr),
          contentType: "text/plain",
        });
        await testInfo.attach("server-stdout.log", {
          body: Buffer.concat(serverStdout),
          contentType: "text/plain",
        });
        if (worker) {
          await testInfo.attach("worker-stderr.log", {
            body: Buffer.concat(workerStderr),
            contentType: "text/plain",
          });
          await testInfo.attach("worker-stdout.log", {
            body: Buffer.concat(workerStdout),
            contentType: "text/plain",
          });
        }
      }
      if (worker) await killProc(worker);
      await killProc(server);
      await rm(tempDir, { recursive: true, force: true });
    }
  });
});
