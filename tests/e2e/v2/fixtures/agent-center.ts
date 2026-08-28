import {
  test as base,
  expect,
  request as playwrightRequest,
  type APIRequestContext,
} from "@playwright/test";
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

export interface AuthSession {
  identityID: string;
  organizationID: string;
  orgSlug: string;
  orgApiURL: string;
  sessionCookie: string;
}

async function createAuthSession(
  request: APIRequestContext,
  baseURL: string,
): Promise<AuthSession> {
  const suffix = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
  const displayName = `e2e-${suffix}`;
  const passcode = "E2ePass1!";
  const response = await request.post(baseURL + "/api/auth/signup", {
    data: {
      display_name: displayName,
      passcode,
      email: `${displayName}@example.test`,
      organization_name: `E2E Org ${suffix}`,
    },
  });
  expect(response.status(), `signup: ${await response.text()}`).toBe(201);
  const sessionCookie = /ac_session=([^;]+)/.exec(response.headers()["set-cookie"] || "")?.[1];
  expect(sessionCookie, "signup session cookie").toBeTruthy();
  const body = (await response.json()) as {
    identity_id: string;
    organization_id: string;
    organization_slug: string;
  };
  return {
    identityID: body.identity_id,
    organizationID: body.organization_id,
    orgSlug: body.organization_slug,
    orgApiURL: `${baseURL}/api/orgs/${body.organization_slug}`,
    sessionCookie: sessionCookie!,
  };
}

export const test = base.extend<{
  agentCenter: AgentCenter;
  authSession: AuthSession;
}>({
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
      await stopProc(proc, "agent-center server");
      await rm(tempDir, { recursive: true, force: true });
    }
  },
  authSession: async ({ agentCenter, context }, use) => {
    const signupRequest = await playwrightRequest.newContext();
    const session = await createAuthSession(signupRequest, agentCenter.baseURL);
    await signupRequest.dispose();
    await context.addCookies([
      {
        name: "ac_session",
        value: session.sessionCookie,
        url: agentCenter.baseURL,
        httpOnly: true,
        sameSite: "Lax",
      },
    ]);
    await use(session);
  },
  request: async ({ authSession }, use) => {
    const authenticatedRequest = await playwrightRequest.newContext({
      extraHTTPHeaders: {
        Cookie: `ac_session=${authSession.sessionCookie}`,
      },
    });
    await use(authenticatedRequest);
    await authenticatedRequest.dispose();
  },
});

export { expect };

async function stopProc(proc: ChildProcess, label: string, graceMs = 2_000): Promise<void> {
  if (proc.exitCode !== null || proc.signalCode !== null) return;
  proc.kill("SIGTERM");
  if (await waitForExit(proc, graceMs)) return;
  proc.kill("SIGKILL");
  if (await waitForExit(proc, graceMs)) return;
  throw new Error(`${label} did not exit after SIGTERM/SIGKILL`);
}

function waitForExit(proc: ChildProcess, timeoutMs: number): Promise<boolean> {
  if (proc.exitCode !== null || proc.signalCode !== null) return Promise.resolve(true);
  return new Promise((resolve) => {
    const timer = setTimeout(() => {
      proc.off("exit", onExit);
      resolve(false);
    }, timeoutMs);
    const onExit = () => {
      clearTimeout(timer);
      resolve(true);
    };
    proc.once("exit", onExit);
  });
}
