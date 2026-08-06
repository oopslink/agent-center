import { execFile as execFileCb, type ChildProcess } from "node:child_process";
import { createHash } from "node:crypto";
import { lstat, readlink, realpath, stat } from "node:fs/promises";
import http from "node:http";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { promisify } from "node:util";

const execFile = promisify(execFileCb);

export type ComponentName = "center" | "worker" | "agent-runtime";

export type ArtifactExpectation = {
  binaryPath: string;
  binaryRealPath: string;
  version: string;
  commit: string;
  workerVersion: string;
  buildID: string;
  artifactMtimeMs: number;
  activatedAtMs: number;
  currentLink?: string;
  currentTarget?: string;
};

export type VersionObservation = {
  component: ComponentName;
  pid: number;
  startedAtMs?: number;
  startedAt?: string;
  version?: string;
  commit?: string;
  buildID?: string;
  executablePath?: string;
  parentPID?: number;
};

export type AgentRuntimeHealth = {
  agent_id: string;
  pid?: number;
  parent_pid?: number;
  started_at?: string;
  executable_path?: string;
  version?: string;
  commit?: string;
  branch?: string;
  built_at?: string;
  build_id?: string;
  current_target?: string;
};

type AssertionContext = {
  toleranceMs?: number;
  workerPid?: number;
  workerStartedAtMs?: number;
};

export async function readArtifactExpectation(
  binaryPath: string,
  opts: { currentLink?: string } = {},
): Promise<ArtifactExpectation> {
  const version = await readBinaryVersion(binaryPath);
  const st = await stat(binaryPath);
  const out: ArtifactExpectation = {
    binaryPath,
    binaryRealPath: await safeRealpath(binaryPath),
    version: version.version,
    commit: version.commit,
    workerVersion:
      version.commit && version.commit !== "unknown"
        ? `${version.version}+${version.commit}`
        : version.version,
    buildID: await goBuildID(binaryPath),
    artifactMtimeMs: st.mtimeMs,
    activatedAtMs: st.mtimeMs,
  };
  if (opts.currentLink) {
    const link = opts.currentLink;
    const lst = await lstat(link);
    const target = await readlink(link);
    out.currentLink = link;
    out.currentTarget = resolve(dirname(link), target);
    out.activatedAtMs = lst.mtimeMs;
  }
  return out;
}

export async function readBinaryVersion(
  binaryPath: string,
): Promise<{ version: string; commit: string }> {
  const { stdout } = await execFile(binaryPath, ["version"]);
  const text = stdout.trim();
  const m = text.match(/^agent-center\s+(.+?)\s+\(commit\s+(.+?)\)$/);
  if (!m) {
    throw new Error(`cannot parse ${binaryPath} version output: ${JSON.stringify(text)}`);
  }
  return { version: m[1], commit: m[2] };
}

export async function goBuildID(binaryPath: string): Promise<string> {
  try {
    const { stdout } = await execFile("go", ["tool", "buildid", binaryPath]);
    return stdout.trim();
  } catch (err) {
    return `unavailable:${String(err)}`;
  }
}

export async function processStartedAt(pid: number): Promise<{ ms?: number; text?: string }> {
  try {
    const { stdout } = await execFile("ps", ["-o", "lstart=", "-p", String(pid)]);
    const text = stdout.trim().replace(/\s+/g, " ");
    if (!text) return {};
    const ms = Date.parse(text);
    return Number.isNaN(ms) ? { text } : { ms, text };
  } catch {
    return {};
  }
}

export async function observeChildProcess(
  component: Exclude<ComponentName, "agent-runtime">,
  proc: ChildProcess,
  fields: { version?: string; commit?: string; executablePath: string; buildID?: string },
): Promise<VersionObservation> {
  if (!proc.pid) {
    throw new Error(`${component} process has no pid`);
  }
  const started = await processStartedAt(proc.pid);
  const executablePath = fields.executablePath;
  return {
    component,
    pid: proc.pid,
    startedAtMs: started.ms,
    startedAt: started.text,
    version: fields.version,
    commit: fields.commit,
    buildID: fields.buildID ?? (await goBuildID(executablePath)),
    executablePath,
  };
}

export async function observeAgentRuntimeHealth(
  health: AgentRuntimeHealth,
): Promise<VersionObservation> {
  if (!health.pid) {
    throw new Error(`agent-runtime health missing pid: ${JSON.stringify(health)}`);
  }
  const startedMs = health.started_at ? Date.parse(health.started_at) : undefined;
  return {
    component: "agent-runtime",
    pid: health.pid,
    parentPID: health.parent_pid,
    startedAtMs: startedMs != null && !Number.isNaN(startedMs) ? startedMs : undefined,
    startedAt: health.started_at,
    version: health.version,
    commit: health.commit,
    buildID:
      health.build_id ||
      (health.executable_path ? await goBuildID(health.executable_path) : undefined),
    executablePath: health.executable_path,
  };
}

export function assertRuntimeVersionConsistency(
  expected: ArtifactExpectation,
  observations: VersionObservation[],
  ctx: AssertionContext = {},
): void {
  const toleranceMs = ctx.toleranceMs ?? 5_000;
  const failures: string[] = [];
  for (const obs of observations) {
    const wantVersion = obs.component === "center" ? expected.version : expected.workerVersion;
    const wantCommit = expected.commit;
    const adopted = runtimeAdopted(obs, ctx, toleranceMs);
    const diag = formatDiagnostic(expected, obs, wantVersion, wantCommit, adopted);

    if (!obs.version) {
      failures.push(`${obs.component}: missing running version; ${diag}`);
    } else if (obs.version !== wantVersion) {
      failures.push(`${obs.component}: version mismatch; ${diag}`);
    }
    if (wantCommit && wantCommit !== "unknown" && obs.commit && obs.commit !== wantCommit) {
      failures.push(`${obs.component}: commit mismatch; ${diag}`);
    }
    if (obs.buildID && expected.buildID && obs.buildID !== expected.buildID) {
      failures.push(`${obs.component}: build_id mismatch; ${diag}`);
    }
    if (obs.startedAtMs != null && obs.startedAtMs < expected.activatedAtMs - toleranceMs) {
      failures.push(`${obs.component}: process started before tested artifact/current activation; ${diag}`);
    }
  }
  if (failures.length > 0) {
    throw new Error("runtime version consistency check failed\n" + failures.join("\n"));
  }
}

export function workerSockDir(workerID: string): string {
  return join(tmpdir(), "acw-" + createHash("sha1").update(workerID).digest("hex").slice(0, 12));
}

export function agentRuntimeSocketPath(workerID: string, agentID: string): string {
  const sock = "acs-" + createHash("sha1").update(agentID).digest("hex").slice(0, 16) + ".sock";
  return join(workerSockDir(workerID), sock);
}

export async function waitAgentRuntimeHealth(
  socketPath: string,
  agentID: string,
  deadlineMs: number,
): Promise<AgentRuntimeHealth> {
  const deadline = Date.now() + deadlineMs;
  let lastErr: unknown = null;
  while (Date.now() < deadline) {
    try {
      const health = await getUnixJSON<AgentRuntimeHealth>(socketPath, "/health");
      if (health.agent_id === agentID) return health;
      lastErr = new Error(`health served agent_id=${health.agent_id}, want ${agentID}`);
    } catch (err) {
      lastErr = err;
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`agent-runtime health not ready at ${socketPath} within ${deadlineMs}ms (last err=${String(lastErr)})`);
}

export async function readHTTPJSON<T>(url: string): Promise<T> {
  return new Promise((resolveP, rejectP) => {
    const req = http.request(url, { method: "GET" }, (res) => {
      const chunks: Buffer[] = [];
      res.on("data", (c) => chunks.push(c));
      res.on("end", () => {
        const body = Buffer.concat(chunks).toString("utf8");
        if ((res.statusCode ?? 0) < 200 || (res.statusCode ?? 0) >= 300) {
          rejectP(new Error(`GET ${url} returned ${res.statusCode}: ${body}`));
          return;
        }
        try {
          resolveP(JSON.parse(body) as T);
        } catch (err) {
          rejectP(err);
        }
      });
    });
    req.on("error", rejectP);
    req.end();
  });
}

async function getUnixJSON<T>(socketPath: string, path: string): Promise<T> {
  return new Promise((resolveP, rejectP) => {
    const req = http.request({ socketPath, method: "GET", path }, (res) => {
      const chunks: Buffer[] = [];
      res.on("data", (c) => chunks.push(c));
      res.on("end", () => {
        const body = Buffer.concat(chunks).toString("utf8");
        if ((res.statusCode ?? 0) < 200 || (res.statusCode ?? 0) >= 300) {
          rejectP(new Error(`GET unix:${socketPath}:${path} returned ${res.statusCode}: ${body}`));
          return;
        }
        try {
          resolveP(JSON.parse(body) as T);
        } catch (err) {
          rejectP(err);
        }
      });
    });
    req.on("error", rejectP);
    req.end();
  });
}

async function safeRealpath(path: string): Promise<string> {
  try {
    return await realpath(path);
  } catch {
    return path;
  }
}

function runtimeAdopted(obs: VersionObservation, ctx: AssertionContext, toleranceMs: number): boolean {
  if (obs.component !== "agent-runtime") return false;
  if (ctx.workerPid && obs.parentPID && obs.parentPID !== ctx.workerPid) return true;
  if (
    ctx.workerStartedAtMs != null &&
    obs.startedAtMs != null &&
    obs.startedAtMs < ctx.workerStartedAtMs - toleranceMs
  ) {
    return true;
  }
  return false;
}

function formatDiagnostic(
  expected: ArtifactExpectation,
  obs: VersionObservation,
  wantVersion: string,
  wantCommit: string,
  adopted: boolean,
): string {
  return [
    `component=${obs.component}`,
    `expected_version=${wantVersion}`,
    `running_version=${obs.version ?? "<missing>"}`,
    `expected_commit=${wantCommit}`,
    `running_commit=${obs.commit ?? "<missing>"}`,
    `expected_build_id=${expected.buildID || "<missing>"}`,
    `running_build_id=${obs.buildID ?? "<missing>"}`,
    `pid=${obs.pid}`,
    `started_at=${obs.startedAt ?? (obs.startedAtMs ? new Date(obs.startedAtMs).toISOString() : "<unknown>")}`,
    `executable=${obs.executablePath ?? "<unknown>"}`,
    `artifact=${expected.binaryPath}`,
    `artifact_mtime=${new Date(expected.artifactMtimeMs).toISOString()}`,
    `activated_at=${new Date(expected.activatedAtMs).toISOString()}`,
    `current_link=${expected.currentLink ?? "<none>"}`,
    `current_target=${expected.currentTarget ?? "<none>"}`,
    `parent_pid=${obs.parentPID ?? "<none>"}`,
    `adopted=${adopted}`,
  ].join(" ");
}
