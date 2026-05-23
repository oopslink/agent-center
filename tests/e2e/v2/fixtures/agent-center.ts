import { test as base } from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import { randomBytes } from "node:crypto";
import { chmod, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { pickFreePort } from "../helpers/ports.js";

const __filename = fileURLToPath(import.meta.url);
const REPO_ROOT = resolve(__filename, "../../../../..");
const BINARY = resolve(REPO_ROOT, "bin/agent-center");

export interface AgentCenter {
  baseURL: string;     // http://127.0.0.1:<webPort>
  apiURL: string;      // same as baseURL/api
  grpcPort: number;
  webPort: number;
  configPath: string;
  dbPath: string;
  tempDir: string;
}

export const test = base.extend<{ agentCenter: AgentCenter }>({
  // worker-scoped fixture would share one binary across tests in the
  // same worker; we picked test-scoped so a single broken test can't
  // corrupt later tests' DB state. Cost: ~1s setup per test.
  agentCenter: async ({}, use, testInfo) => {
    const tempDir = await mkdtemp(join(tmpdir(), "agent-center-e2e-"));
    const dbPath = join(tempDir, "agent-center.db");
    const sockPath = join(tempDir, "admin.sock");
    const masterKeyPath = join(tempDir, "master.key");
    const grpcPort = await pickFreePort();
    const webPort = await pickFreePort();
    const configPath = join(tempDir, "config.yaml");

    // Generate a random 32-byte master key, base64-encode, write 0600.
    // Lets SecretManagement BC wire up so /api/secrets works in tests.
    const masterKeyB64 = randomBytes(32).toString("base64");
    await writeFile(masterKeyPath, masterKeyB64 + "\n", "utf8");
    await chmod(masterKeyPath, 0o600);

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

    const proc: ChildProcess = spawn(
      BINARY,
      ["server", "--config", configPath],
      {
        stdio: ["ignore", "pipe", "pipe"],
        env: { ...process.env, AGENT_CENTER_INVOCATION_ID: "" },
      },
    );

    // Capture for debug-on-failure (attached to test info later).
    const stdoutChunks: Buffer[] = [];
    const stderrChunks: Buffer[] = [];
    proc.stdout?.on("data", (c) => stdoutChunks.push(c));
    proc.stderr?.on("data", (c) => stderrChunks.push(c));

    const baseURL = `http://127.0.0.1:${webPort}`;
    const apiURL = `${baseURL}/api`;

    // Poll until the web console responds. ~5s budget; binary
    // typically opens the listener within 100-200ms after spawn.
    let lastErr: unknown = null;
    const deadline = Date.now() + 5_000;
    while (Date.now() < deadline) {
      try {
        const r = await fetch(baseURL + "/");
        if (r.ok) {
          lastErr = null;
          break;
        }
        lastErr = new Error(`HTTP ${r.status}`);
      } catch (e) {
        lastErr = e;
      }
      await new Promise((r) => setTimeout(r, 75));
    }
    if (lastErr) {
      proc.kill("SIGKILL");
      const tail =
        Buffer.concat(stderrChunks).toString("utf8").slice(-2000) ||
        Buffer.concat(stdoutChunks).toString("utf8").slice(-2000);
      throw new Error(
        `agent-center server failed to come up on ${baseURL}: ${String(
          lastErr,
        )}\n--- server output ---\n${tail}`,
      );
    }

    try {
      await use({
        baseURL,
        apiURL,
        grpcPort,
        webPort,
        configPath,
        dbPath,
        tempDir,
      });
    } finally {
      // Attach server logs on failure for easier debug.
      if (testInfo.status !== testInfo.expectedStatus) {
        await testInfo.attach("server-stdout.log", {
          body: Buffer.concat(stdoutChunks),
          contentType: "text/plain",
        });
        await testInfo.attach("server-stderr.log", {
          body: Buffer.concat(stderrChunks),
          contentType: "text/plain",
        });
      }
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
        }, 2_000);
      });
      await rm(tempDir, { recursive: true, force: true });
    }
  },
});

export const expect = test.expect;
