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
//   3. A fakeagent dispatch still drives task → done end-to-end when the
//      worker daemon is started with a scoped (NON-`*`) token holding
//      only the production set: dispatch:pull + secret:resolve + blob:put +
//      task:*.
//
// That subset is sufficient to prove the real-agent dispatch chain is
// alive — defaultAgentSpawner's AssemblePrompt + MCPInjector wiring is
// covered by internal/workerdaemon/runtime_real_agent_test.go (unit) so
// the e2e doesn't need to host an MCP-aware agent.

import { test, expect } from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import { randomBytes } from "node:crypto";
import { chmod, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import http from "node:http";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { pickFreePort } from "../helpers/ports.js";

const __filename = fileURLToPath(import.meta.url);
const REPO_ROOT = resolve(__filename, "../../../../..");
const SERVER_BIN = resolve(REPO_ROOT, "bin/agent-center");
// v2.7 (b) cutover: the worker runs as the unified `agent-center` binary
// (`agent-center worker run ...`); the standalone agent-center-worker-daemon is
// retired. os.Executable()=agent-center so the daemon routes the worker
// agent-supervisor / mcp-host subcommands it spawns.
const WORKER_BIN = resolve(REPO_ROOT, "bin/agent-center");
const FAKEAGENT_BIN = resolve(REPO_ROOT, "bin/fakeagent");

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

function adminGET(
  socketPath: string,
  path: string,
  token: string,
): Promise<AdminResp> {
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

test.describe("v2.3-3b — real-agent dispatch chain", () => {
  test("scoped tokens drive secret resolve + blob put + dispatch", async ({}, testInfo) => {
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

    const workerStdout: Buffer[] = [];
    const workerStderr: Buffer[] = [];
    let worker: ChildProcess | null = null;

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

      // --- step 3: fakeagent dispatch with scoped worker token ---------------
      const pid = "p-v23";
      r = await adminPOST(
        sockPath,
        "/admin/workforce/project/add",
        { id: pid, name: "v23-test", kind: "coding" },
        bootstrap,
      );
      expect(r.status, "project add: " + r.body).toBe(200);

      r = await adminPOST(
        sockPath,
        "/admin/taskruntime/task/create",
        {
          project_id: pid,
          title: "v23-3b task",
          description: "fakeagent-script: " + scriptPath,
          priority: "medium",
          requires_worktree: false,
          with_conversation: true,
          conversation_title: "v23 conv",
        },
        bootstrap,
      );
      expect(r.status, "task create: " + r.body).toBe(200);
      const created = JSON.parse(r.body) as {
        task_id: string;
        conversation_id: string;
      };

      worker = spawn(
        WORKER_BIN,
        [
          "worker",
          "run",
          "--config",
          configPath,
          "--worker-id",
          "test-w-1",
          "--fake-agent",
          FAKEAGENT_BIN,
          "--poll-interval",
          "200ms",
          "--admin-token",
          workerToken,
        ],
        {
          stdio: ["ignore", "pipe", "pipe"],
          env: { ...process.env, AGENT_CENTER_INVOCATION_ID: "" },
        },
      );
      worker.stdout?.on("data", (c) => workerStdout.push(c));
      worker.stderr?.on("data", (c) => workerStderr.push(c));

      const enrollDeadline = Date.now() + 5_000;
      while (Date.now() < enrollDeadline) {
        if (
          Buffer.concat(workerStderr).toString("utf8").includes("enrolled")
        ) {
          break;
        }
        await sleep(100);
      }

      r = await adminPOST(
        sockPath,
        "/admin/taskruntime/dispatch/dispatch",
        {
          task_id: created.task_id,
          worker_id: "test-w-1",
          agent_cli: "fakeagent",
          base_branch: "main",
        },
        bootstrap,
      );
      expect(r.status, "dispatch: " + r.body).toBe(200);
      const disp = JSON.parse(r.body) as { execution_id: string };

      const pollDeadline = Date.now() + 20_000;
      let lastStatus = "";
      while (Date.now() < pollDeadline) {
        const er = await adminGET(
          sockPath,
          "/admin/taskruntime/exec/find-by-id?id=" + disp.execution_id,
          bootstrap,
        );
        if (er.status === 200) {
          const e = JSON.parse(er.body) as { status: string };
          lastStatus = e.status;
          if (
            e.status === "completed" ||
            e.status === "failed" ||
            e.status === "killed"
          ) {
            break;
          }
        }
        await sleep(250);
      }
      expect(
        lastStatus,
        "exec status (worker stderr tail: " +
          Buffer.concat(workerStderr).toString("utf8").slice(-1500) +
          ")",
      ).toBe("completed");
    } finally {
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
        }
      }
      if (worker) await killProc(worker);
      await killProc(server);
      await rm(tempDir, { recursive: true, force: true });
    }
  });
});
