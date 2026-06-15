import { test, expect } from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import { randomBytes } from "node:crypto";
import { chmod, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import http from "node:http";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { pickFreePort } from "../helpers/ports.js";

// v2.10 Plan Shared Findings — DEPLOYED-BINARY smoke (testing.md § 2.3).
//
// Drives the REAL `bin/agent-center server` process over its REAL admin unix
// socket (no in-process shortcut) to prove the v2.10 feature is in the SHIPPED
// binary:
//   1. the server BOOTS — startup runs migrations, so migration 0060
//      (pm_plan_findings) applied cleanly in the real binary (a malformed
//      migration would abort startup and the boot-poll would throw);
//   2. the new agent-tool routes record_finding / list_findings are REGISTERED on
//      the real admin mux: with a valid bearer (past the socket's global token
//      gate), a malformed-JSON probe reaches the handler's decodeJSON → 400
//      invalid_json (a live handler), while an UNREGISTERED agent-tool path is a
//      ServeMux 404 — proving the former are genuine route hits.
//
// The full record→list→dispatch-injection BEHAVIOR (requireAgentOnWorker + the
// admission gate) is covered against the real HTTP handlers + real sqlite by
// internal/admin/api/agent_tools_findings_test.go and the pm service tests; this
// smoke closes the "actually in the shipped binary / served over the real
// transport" gap that in-process tests cannot.

const __filename = fileURLToPath(import.meta.url);
const REPO_ROOT = resolve(__filename, "../../../../..");
const BINARY = resolve(REPO_ROOT, "bin/agent-center");

type AdminResp = { status: number; body: string };

// adminRaw POSTs a RAW body string (so we can send malformed JSON) with a bearer.
function adminRaw(
  socketPath: string,
  path: string,
  raw: string,
  token: string,
): Promise<AdminResp> {
  return new Promise((resolveP, rejectP) => {
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      "Content-Length": Buffer.byteLength(raw).toString(),
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
    req.write(raw);
    req.end();
  });
}

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

async function readBootstrapToken(p: string, deadlineMs: number): Promise<string> {
  const deadline = Date.now() + deadlineMs;
  let lastErr: unknown = null;
  while (Date.now() < deadline) {
    try {
      const tok = (await readFile(p, "utf8")).trim();
      if (tok) return tok;
    } catch (e) {
      lastErr = e;
    }
    await sleep(75);
  }
  throw new Error(`bootstrap_token not written within ${deadlineMs}ms (last=${String(lastErr)})`);
}

test("v2.10 findings: record_finding/list_findings served by the real binary over the admin socket", async () => {
  test.setTimeout(45_000);
  const tempDir = await mkdtemp(join(tmpdir(), "ac-findings-smoke-"));
  const dbPath = join(tempDir, "agent-center.db");
  const sockPath = join(tempDir, "admin.sock");
  const masterKeyPath = join(tempDir, "master.key");
  const grpcPort = await pickFreePort();
  const webPort = await pickFreePort();
  const configPath = join(tempDir, "config.yaml");

  await writeFile(masterKeyPath, randomBytes(32).toString("base64") + "\n", "utf8");
  await chmod(masterKeyPath, 0o600);
  await writeFile(
    configPath,
    `
server:
  listen_addr: ":${grpcPort}"
  sqlite_path: "${dbPath}"
  admin_socket_path: "${sockPath}"
web_console:
  enabled: true
  listen_addr: "127.0.0.1:${webPort}"
secret_management:
  master_key_file: "${masterKeyPath}"
`,
    "utf8",
  );

  const proc: ChildProcess = spawn(BINARY, ["server", "--config", configPath], {
    stdio: ["ignore", "pipe", "pipe"],
    env: { ...process.env, AGENT_CENTER_INVOCATION_ID: "" },
  });
  const out: Buffer[] = [];
  proc.stdout?.on("data", (c) => out.push(c));
  proc.stderr?.on("data", (c) => out.push(c));

  try {
    // (1) Boot poll — a healthy response means migrations (incl. 0060) ran.
    let up = false;
    const deadline = Date.now() + 8_000;
    while (Date.now() < deadline) {
      try {
        const r = await fetch(`http://127.0.0.1:${webPort}/api/health`);
        if (r.ok) {
          up = true;
          break;
        }
      } catch {
        /* not up yet */
      }
      await sleep(75);
    }
    expect(
      up,
      `server did not boot (migration 0060 may have failed):\n${Buffer.concat(out).toString("utf8").slice(-2000)}`,
    ).toBe(true);

    // Mint a token (past the admin socket's global token gate).
    const bootstrap = await readBootstrapToken(join(tempDir, "bootstrap_token"), 5_000);
    const mint = await adminRaw(
      sockPath,
      "/admin/admintoken/create",
      JSON.stringify({ owner: "worker:smoke-w", scopes: ["*"], created_by: "test" }),
      bootstrap,
    );
    expect(mint.status, "mint token: " + mint.body).toBe(200);
    const token = (JSON.parse(mint.body) as { plaintext: string }).plaintext;
    expect(token).toBeTruthy();

    // (2) Registered routes: a malformed-JSON probe reaches the handler's
    //     decodeJSON → 400 invalid_json (NOT a 404).
    const rf = await adminRaw(sockPath, "/admin/agent-tools/record_finding", "{ not json", token);
    expect(rf.status, `record_finding live handler (got ${rf.status}: ${rf.body})`).toBe(400);
    expect(rf.body).toContain("invalid_json");

    const lf = await adminRaw(sockPath, "/admin/agent-tools/list_findings", "{ not json", token);
    expect(lf.status, `list_findings live handler (got ${lf.status}: ${lf.body})`).toBe(400);
    expect(lf.body).toContain("invalid_json");

    // (3) Control: an UNREGISTERED agent-tool path is a ServeMux 404 — proving
    //     the 400s above are real route hits, not a catch-all.
    const bogus = await adminRaw(sockPath, "/admin/agent-tools/bogus_finding_zzz", "{ not json", token);
    expect(bogus.status, "unregistered agent-tool route must 404").toBe(404);
  } finally {
    proc.kill("SIGTERM");
    await new Promise<void>((done) => {
      let settled = false;
      const finish = () => {
        if (!settled) {
          settled = true;
          done();
        }
      };
      proc.once("exit", finish);
      setTimeout(() => {
        if (!proc.killed) proc.kill("SIGKILL");
        finish();
      }, 2_000);
    });
    await rm(tempDir, { recursive: true, force: true });
  }
});
