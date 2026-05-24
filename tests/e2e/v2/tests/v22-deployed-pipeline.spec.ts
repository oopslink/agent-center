// v2.2 Phase D — deployed-binary e2e verification (slock task #16).
//
// Topology under test (LLM-free):
//
//   bin/agent-center server            ──┐  unix socket
//                                       admin endpoint
//   bin/agent-center-worker-daemon  ──┘
//        │
//        └── spawns bin/fakeagent --script=<jsonl>
//
// Asserts the full state-machine path closes:
//   task created (with conversation)
//     → execution submitted
//     → notify-working                  (Phase D fix gap #1)
//     → agent emits done event
//     → conclude                        (Phase D fix gap #1)
//     → execution.status = "completed"  + task.status = "done"
//
// No real LLM, no claude-code/codex/opencode, no network calls beyond
// loopback unix socket + 127.0.0.1 webconsole.

import { test, expect } from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import { randomBytes } from "node:crypto";
import { chmod, mkdtemp, rm, writeFile } from "node:fs/promises";
import http from "node:http";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { pickFreePort } from "../helpers/ports.js";

const __filename = fileURLToPath(import.meta.url);
const REPO_ROOT = resolve(__filename, "../../../../..");
const SERVER_BIN = resolve(REPO_ROOT, "bin/agent-center");
const WORKER_BIN = resolve(REPO_ROOT, "bin/agent-center-worker-daemon");
const FAKEAGENT_BIN = resolve(REPO_ROOT, "bin/fakeagent");

// adminPOST issues an HTTP POST over the admin unix socket and resolves
// to {status, body} or rejects on transport error.
function adminPOST(
  socketPath: string,
  path: string,
  body: unknown,
): Promise<{ status: number; body: string }> {
  return new Promise((resolveP, rejectP) => {
    const data = body == null ? "" : JSON.stringify(body);
    const req = http.request(
      {
        socketPath,
        method: "POST",
        path,
        headers: {
          "Content-Type": "application/json",
          "Content-Length": Buffer.byteLength(data).toString(),
        },
      },
      (res) => {
        const chunks: Buffer[] = [];
        res.on("data", (c) => chunks.push(c));
        res.on("end", () => {
          resolveP({
            status: res.statusCode ?? 0,
            body: Buffer.concat(chunks).toString("utf8"),
          });
        });
      },
    );
    req.on("error", rejectP);
    req.write(data);
    req.end();
  });
}

// adminGET issues an HTTP GET over the admin unix socket.
function adminGET(
  socketPath: string,
  path: string,
): Promise<{ status: number; body: string }> {
  return new Promise((resolveP, rejectP) => {
    const req = http.request(
      { socketPath, method: "GET", path },
      (res) => {
        const chunks: Buffer[] = [];
        res.on("data", (c) => chunks.push(c));
        res.on("end", () => {
          resolveP({
            status: res.statusCode ?? 0,
            body: Buffer.concat(chunks).toString("utf8"),
          });
        });
      },
    );
    req.on("error", rejectP);
    req.end();
  });
}

// sleep is a tiny polling helper.
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

test.describe("v2.2 Phase D — deployed-binary pipeline", () => {
  test("server + worker daemon + fakeagent drive task → done", async ({}, testInfo) => {
    test.setTimeout(45_000);

    // --- temp scaffolding -------------------------------------------------
    const tempDir = await mkdtemp(join(tmpdir(), "ac-v22d-"));
    const dbPath = join(tempDir, "agent-center.db");
    const sockPath = join(tempDir, "admin.sock");
    const masterKeyPath = join(tempDir, "master.key");
    const scriptPath = join(tempDir, "fakeagent-script.jsonl");
    const configPath = join(tempDir, "config.yaml");
    const webPort = await pickFreePort();
    const grpcPort = await pickFreePort();

    // Master key for SecretManagement BC (required by config loader if
    // master_key_file is set — we wire it for parity with smoke fixture).
    await writeFile(
      masterKeyPath,
      randomBytes(32).toString("base64") + "\n",
      "utf8",
    );
    await chmod(masterKeyPath, 0o600);

    // 3-event fakeagent script: start, progress, done.
    await writeFile(
      scriptPath,
      [
        `{"type":"start","text":"hello"}`,
        `{"type":"progress","milestone":"step_1","content":"working"}`,
        `{"type":"done","content":"all done"}`,
      ].join("\n") + "\n",
      "utf8",
    );

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
identity:
  default_user: "hayang"
`;
    await writeFile(configPath, config, "utf8");

    // --- spawn server -----------------------------------------------------
    const serverStdout: Buffer[] = [];
    const serverStderr: Buffer[] = [];
    const server = spawn(SERVER_BIN, ["server", "--config", configPath], {
      stdio: ["ignore", "pipe", "pipe"],
      env: { ...process.env, AGENT_CENTER_INVOCATION_ID: "" },
    });
    server.stdout?.on("data", (c) => serverStdout.push(c));
    server.stderr?.on("data", (c) => serverStderr.push(c));

    const workerStdout: Buffer[] = [];
    const workerStderr: Buffer[] = [];
    let worker: ChildProcess | null = null;

    try {
      // Wait for server's webconsole and the admin socket to be up.
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
      if (!serverReady) {
        throw new Error(
          "server did not come up: " +
            Buffer.concat(serverStderr).toString("utf8").slice(-1500),
        );
      }

      // --- seed via admin endpoint: project + task --------------------------
      // project add
      const pid = "p-v22d";
      let r = await adminPOST(sockPath, "/admin/workforce/project/add", {
        id: pid,
        name: "v22d-test",
        kind: "coding",
      });
      expect(r.status, "project add: " + r.body).toBe(200);

      // task create — with_conversation=true so ReportProgress doesn't
      // silently no-op per ADR-0017 (Phase D gap #2: TaskService.Create
      // already supports WithConversation; we exercise it here).
      r = await adminPOST(sockPath, "/admin/taskruntime/task/create", {
        project_id: pid,
        title: "v22d task",
        description: "fakeagent-script: " + scriptPath,
        priority: "medium",
        requires_worktree: false,
        with_conversation: true,
        conversation_title: "v22d conv",
      });
      expect(r.status, "task create: " + r.body).toBe(200);
      const created = JSON.parse(r.body) as {
        task_id: string;
        conversation_id: string;
      };
      expect(created.task_id).toBeTruthy();
      expect(created.conversation_id).toBeTruthy();

      // --- spawn worker daemon ----------------------------------------------
      worker = spawn(
        WORKER_BIN,
        [
          "--config",
          configPath,
          "--worker-id",
          "test-w-1",
          "--fake-agent",
          FAKEAGENT_BIN,
          "--poll-interval",
          "200ms",
        ],
        {
          stdio: ["ignore", "pipe", "pipe"],
          env: { ...process.env, AGENT_CENTER_INVOCATION_ID: "" },
        },
      );
      worker.stdout?.on("data", (c) => workerStdout.push(c));
      worker.stderr?.on("data", (c) => workerStderr.push(c));

      // Wait for worker enroll line in stderr (best-effort; we don't fail
      // if missing, the dispatch poll below will surface real errors).
      const enrollDeadline = Date.now() + 5_000;
      while (Date.now() < enrollDeadline) {
        if (
          Buffer.concat(workerStderr).toString("utf8").includes("enrolled")
        ) {
          break;
        }
        await sleep(100);
      }

      // --- dispatch the task to the worker ---------------------------------
      // v1 dispatch path (worker_id + agent_cli explicit); the
      // AgentResolver wiring is exercised separately by unit tests.
      r = await adminPOST(sockPath, "/admin/taskruntime/dispatch/dispatch", {
        task_id: created.task_id,
        worker_id: "test-w-1",
        agent_cli: "fakeagent",
        base_branch: "main",
      });
      expect(r.status, "dispatch: " + r.body).toBe(200);
      const disp = JSON.parse(r.body) as { execution_id: string };
      expect(disp.execution_id).toBeTruthy();

      // --- poll exec status until terminal (or timeout) --------------------
      const pollDeadline = Date.now() + 20_000;
      let lastStatus = "";
      let lastTaskStatus = "";
      while (Date.now() < pollDeadline) {
        const er = await adminGET(
          sockPath,
          "/admin/taskruntime/exec/find-by-id?id=" + disp.execution_id,
        );
        if (er.status === 200) {
          const e = JSON.parse(er.body) as { status: string };
          lastStatus = e.status;
          if (e.status === "completed" || e.status === "failed" || e.status === "killed") {
            break;
          }
        }
        const tr = await adminGET(
          sockPath,
          "/admin/taskruntime/task/find-by-id?id=" + created.task_id,
        );
        if (tr.status === 200) {
          lastTaskStatus = (JSON.parse(tr.body) as { status: string }).status;
        }
        await sleep(250);
      }

      // Final task status snapshot.
      const tr = await adminGET(
        sockPath,
        "/admin/taskruntime/task/find-by-id?id=" + created.task_id,
      );
      expect(tr.status).toBe(200);
      lastTaskStatus = (JSON.parse(tr.body) as { status: string }).status;

      // --- assertions -------------------------------------------------------
      expect(
        lastStatus,
        "exec status (worker stderr tail: " +
          Buffer.concat(workerStderr).toString("utf8").slice(-1500) +
          ")",
      ).toBe("completed");
      expect(lastTaskStatus).toBe("done");

      // Smoke the fleet snapshot via webconsole API (read-path sanity).
      const fleet = await fetch(`http://127.0.0.1:${webPort}/api/fleet`);
      expect(fleet.ok).toBe(true);
    } finally {
      // Attach diagnostics on failure for easier triage.
      if (testInfo.status !== testInfo.expectedStatus) {
        await testInfo.attach("server-stderr.log", {
          body: Buffer.concat(serverStderr),
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
