import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { chmod, copyFile, mkdir, readFile, readlink, realpath, stat, symlink, writeFile } from "node:fs/promises";
import http from "node:http";
import { tmpdir } from "node:os";
import { dirname, isAbsolute, join, resolve } from "node:path";

export interface BinaryIdentity {
  path: string;
  realPath: string;
  version: string;
  commit: string;
  buildID: string;
  mtime: string;
}

export interface CurrentArtifact {
  prefix: string;
  currentLink: string;
  target: string;
  targetRealPath: string;
  versionMarker: string;
  commitMarker: string;
  binary: BinaryIdentity;
}

export interface ProcessSnapshot {
  pid: number;
  ppid?: number;
  argv?: string;
  exePath?: string;
  startedAt?: string;
  startedAtMs?: number;
  binary?: BinaryIdentity;
}

export interface VersionReport {
  source: string;
  version?: string;
  commit?: string;
  builtAt?: string;
}

export interface VersionCheck {
  component: string;
  expected: BinaryIdentity;
  running?: BinaryIdentity;
  process?: ProcessSnapshot;
  current?: CurrentArtifact;
  reported?: VersionReport;
  expectedReportedVersion?: string;
  agentID?: string;
  runtimeParent?: string;
  adopted?: boolean;
  pidStorePath?: string;
}

export interface StagedCurrentLayout {
  prefix: string;
  currentBin: string;
  currentFakeAgent?: string;
  expected: BinaryIdentity;
  current: CurrentArtifact;
}

interface AdminResp {
  status: number;
  body: string;
}

interface WorkerRow {
  worker_id: string;
  status: string;
  system_info?: {
    agent_center_version?: string;
    worker_version?: string;
  };
}

export function parseAgentCenterVersionOutput(out: string): { version: string; commit: string } {
  const line = out.trim().split(/\r?\n/).find(Boolean) ?? "";
  const m = /^agent-center\s+(\S+)(?:\s+\(commit\s+([^)]+)\))?$/.exec(line);
  if (!m) {
    throw new Error(`cannot parse agent-center version output: ${JSON.stringify(out)}`);
  }
  return { version: m[1], commit: m[2] ?? "unknown" };
}

export function expectedWorkerReportedVersion(id: Pick<BinaryIdentity, "version" | "commit">): string {
  if (id.commit && id.commit !== "unknown") {
    return `${id.version}+${id.commit}`;
  }
  return id.version;
}

export async function readAgentCenterBinaryIdentity(binaryPath: string): Promise<BinaryIdentity> {
  const realPath = await realpath(binaryPath).catch(() => binaryPath);
  const version = parseAgentCenterVersionOutput(await execText(binaryPath, ["version"]));
  const buildID = (await execText("go", ["tool", "buildid", realPath])).trim();
  if (!buildID) {
    throw new Error(`go tool buildid returned empty output for ${realPath}`);
  }
  const st = await stat(realPath);
  return {
    path: binaryPath,
    realPath,
    version: version.version,
    commit: version.commit,
    buildID,
    mtime: new Date(st.mtimeMs).toISOString(),
  };
}

export async function stageCurrentInstallLayout(
  root: string,
  sourceAgentCenter: string,
  sourceFakeAgent?: string,
): Promise<StagedCurrentLayout> {
  const source = await readAgentCenterBinaryIdentity(sourceAgentCenter);
  const prefix = join(root, "deployed-current");
  const versionedDir = join(prefix, "versions", source.version);
  const binDir = join(versionedDir, "bin");
  await mkdir(binDir, { recursive: true });

  const stagedBin = join(binDir, "agent-center");
  await copyFile(sourceAgentCenter, stagedBin);
  await chmod(stagedBin, 0o755);
  if (sourceFakeAgent) {
    const stagedFake = join(binDir, "fakeagent");
    await copyFile(sourceFakeAgent, stagedFake);
    await chmod(stagedFake, 0o755);
  }
  await writeFile(join(versionedDir, "VERSION"), `${source.version}\n`, "utf8");
  await writeFile(join(versionedDir, "COMMIT"), `${source.commit}\n`, "utf8");
  await symlink(versionedDir, join(prefix, "current"));

  const current = await readCurrentArtifact(prefix);
  return {
    prefix,
    currentBin: join(prefix, "current", "bin", "agent-center"),
    currentFakeAgent: sourceFakeAgent ? join(prefix, "current", "bin", "fakeagent") : undefined,
    expected: current.binary,
    current,
  };
}

export async function readCurrentArtifact(prefix: string): Promise<CurrentArtifact> {
  const currentLink = join(prefix, "current");
  const rawTarget = await readlink(currentLink);
  const target = isAbsolute(rawTarget) ? rawTarget : resolve(dirname(currentLink), rawTarget);
  const targetRealPath = await realpath(target).catch(() => target);
  const versionMarker = (await readFile(join(currentLink, "VERSION"), "utf8")).trim();
  const commitMarker = (await readFile(join(currentLink, "COMMIT"), "utf8")).trim();
  const binary = await readAgentCenterBinaryIdentity(join(currentLink, "bin", "agent-center"));
  return { prefix, currentLink, target, targetRealPath, versionMarker, commitMarker, binary };
}

export async function inspectProcess(pid: number): Promise<ProcessSnapshot> {
  const ppid = await psNumber(pid, "ppid");
  const argv = await psText(pid, "command");
  const started = await processStart(pid);
  const exePath = await processExecutablePath(pid, argv);
  let binary: BinaryIdentity | undefined;
  if (exePath) {
    binary = await readAgentCenterBinaryIdentity(exePath);
  }
  return { pid, ppid, argv, exePath, startedAt: started.raw, startedAtMs: started.ms, binary };
}

export async function collectDeployedBinaryVersionChecks(input: {
  expected: BinaryIdentity;
  currentPrefix: string;
  startedAfter: Date;
  center: { pid: number; webURL: string };
  workers: Array<{ pid: number; workerID: string; socketPath: string; adminToken: string }>;
}): Promise<VersionCheck[]> {
  const current = await readCurrentArtifact(input.currentPrefix);
  const checks: VersionCheck[] = [];

  const centerProc = await inspectProcess(input.center.pid);
  const centerReported = await fetchCenterVersion(input.center.webURL);
  checks.push({
    component: "center",
    expected: input.expected,
    running: centerProc.binary,
    process: centerProc,
    current,
    reported: centerReported,
  });

  for (const worker of input.workers) {
    const workerProc = await inspectProcess(worker.pid);
    const workerReport = await waitWorkerVersionReport(worker.socketPath, worker.adminToken, worker.workerID, 8_000);
    checks.push({
      component: "worker",
      expected: input.expected,
      running: workerProc.binary,
      process: workerProc,
      current,
      reported: workerReport,
      expectedReportedVersion: expectedWorkerReportedVersion(input.expected),
    });

    for (const runtime of await inspectAgentRuntimePIDs(worker.workerID, worker.pid, input.expected, current)) {
      checks.push(runtime);
    }
  }

  assertVersionChecks(checks, { startedAfter: input.startedAfter });
  return checks;
}

export async function inspectAgentRuntimePIDs(
  workerID: string,
  workerPID: number,
  expected: BinaryIdentity,
  current: CurrentArtifact,
): Promise<VersionCheck[]> {
  const pidStorePath = agentRuntimePIDStorePath(workerID);
  let raw: string;
  try {
    raw = await readFile(pidStorePath, "utf8");
  } catch {
    return [];
  }
  const parsed = JSON.parse(raw) as Record<string, number>;
  const out: VersionCheck[] = [];
  for (const [agentID, pid] of Object.entries(parsed)) {
    if (!pid || pid <= 0) continue;
    const proc = await inspectProcess(pid);
    const adopted = proc.ppid !== undefined && proc.ppid !== workerPID;
    out.push({
      component: "agent-runtime",
      agentID,
      expected,
      running: proc.binary,
      process: proc,
      current,
      runtimeParent: adopted ? "adopted-orphan" : "worker",
      adopted,
      pidStorePath,
    });
  }
  return out;
}

export function agentRuntimePIDStorePath(workerID: string): string {
  const hash = createHash("sha1").update(workerID).digest("hex").slice(0, 12);
  return join(tmpdir(), `acw-${hash}`, "agent-pids.json");
}

export function assertVersionChecks(checks: VersionCheck[], opts: { startedAfter?: Date } = {}): void {
  const failures: string[] = [];
  const earliestStart = opts.startedAfter ? opts.startedAfter.getTime() - 2_000 : 0;

  for (const check of checks) {
    const prefix = formatCheck(check);
    const running = check.running;
    if (!running) {
      failures.push(`${prefix} reason=missing_running_binary_identity`);
      continue;
    }
    if (running.version !== check.expected.version) {
      failures.push(`${prefix} reason=version_mismatch`);
    }
    if (running.commit !== check.expected.commit) {
      failures.push(`${prefix} reason=commit_mismatch`);
    }
    if (running.buildID !== check.expected.buildID) {
      failures.push(`${prefix} reason=build_id_mismatch`);
    }
    if (check.current) {
      if (check.current.versionMarker !== check.expected.version) {
        failures.push(`${prefix} reason=current_version_marker_mismatch current_marker=${check.current.versionMarker}`);
      }
      if (check.current.commitMarker !== check.expected.commit) {
        failures.push(`${prefix} reason=current_commit_marker_mismatch current_marker=${check.current.commitMarker}`);
      }
      if (check.current.binary.buildID !== check.expected.buildID) {
        failures.push(`${prefix} reason=current_build_id_mismatch current_build_id=${shortBuildID(check.current.binary.buildID)}`);
      }
      if (running.buildID !== check.current.binary.buildID) {
        failures.push(`${prefix} reason=current_vs_process_build_id_mismatch current_build_id=${shortBuildID(check.current.binary.buildID)}`);
      }
    }
    if (check.reported) {
      const expectedReported = check.expectedReportedVersion ?? check.expected.version;
      if (check.reported.version !== expectedReported) {
        failures.push(`${prefix} reason=reported_version_mismatch reported_source=${check.reported.source} reported_version=${check.reported.version ?? "-"}`);
      }
      if (check.reported.commit !== undefined && check.reported.commit !== check.expected.commit) {
        failures.push(`${prefix} reason=reported_commit_mismatch reported_source=${check.reported.source} reported_commit=${check.reported.commit}`);
      }
      if (
        check.component === "center" &&
        (check.reported.builtAt === undefined || check.reported.builtAt === "" || check.reported.builtAt === "unknown")
      ) {
        failures.push(`${prefix} reason=center_built_at_missing reported_source=${check.reported.source}`);
      }
    }
    if (earliestStart > 0 && check.process?.startedAtMs && check.process.startedAtMs < earliestStart) {
      failures.push(`${prefix} reason=process_started_before_smoke started_after=${new Date(earliestStart).toISOString()}`);
    }
  }

  if (failures.length > 0) {
    throw new Error("deployed-binary runtime version consistency failed\n" + failures.join("\n"));
  }
}

function formatCheck(check: VersionCheck): string {
  const process = check.process;
  const running = check.running;
  const parts = [
    `component=${check.component}`,
    check.agentID ? `agent_id=${check.agentID}` : "",
    `expected version=${check.expected.version}`,
    `expected commit=${check.expected.commit}`,
    `expected build_id=${shortBuildID(check.expected.buildID)}`,
    `running version=${running?.version ?? "-"}`,
    `running commit=${running?.commit ?? "-"}`,
    `running build_id=${running?.buildID ? shortBuildID(running.buildID) : "-"}`,
    process ? `pid=${process.pid}` : "",
    process?.ppid !== undefined ? `ppid=${process.ppid}` : "",
    process?.startedAt ? `started_at=${quote(process.startedAt)}` : "",
    process?.exePath ? `process_exe=${quote(process.exePath)}` : "",
    check.current ? `current_target=${quote(check.current.targetRealPath)}` : "",
    check.runtimeParent ? `runtime_parent=${check.runtimeParent}` : "",
    check.adopted !== undefined ? `adopt=${check.adopted ? "adopted" : "spawned"}` : "",
    check.pidStorePath ? `pid_store=${quote(check.pidStorePath)}` : "",
  ];
  return parts.filter(Boolean).join(" ");
}

function shortBuildID(buildID: string): string {
  if (buildID.length <= 24) return buildID;
  return `${buildID.slice(0, 12)}...${buildID.slice(-8)}`;
}

function quote(s: string): string {
  return JSON.stringify(s);
}

async function fetchCenterVersion(webURL: string): Promise<VersionReport> {
  const res = await fetch(`${webURL.replace(/\/$/, "")}/api/system/version`);
  const body = (await res.json()) as { version?: string; commit?: string; built_at?: string };
  if (!res.ok) {
    throw new Error(`center /api/system/version HTTP ${res.status}`);
  }
  return {
    source: "center /api/system/version",
    version: body.version,
    commit: body.commit,
    builtAt: body.built_at,
  };
}

async function waitWorkerVersionReport(
  socketPath: string,
  adminToken: string,
  workerID: string,
  deadlineMs: number,
): Promise<VersionReport> {
  const deadline = Date.now() + deadlineMs;
  let last = "";
  while (Date.now() < deadline) {
    const r = await adminSocketGET(socketPath, "/admin/workforce/worker/find-all", adminToken);
    last = r.body;
    if (r.status === 200) {
      const rows = JSON.parse(r.body) as WorkerRow[];
      const row = rows.find((w) => w.worker_id === workerID);
      const workerVersion = row?.system_info?.worker_version;
      if (workerVersion) {
        return { source: `admin worker/find-all system_info worker_id=${workerID}`, version: workerVersion };
      }
    }
    await sleep(150);
  }
  throw new Error(`worker ${workerID} did not report system_info.worker_version within ${deadlineMs}ms; last=${last.slice(-1000)}`);
}

function adminSocketGET(socketPath: string, path: string, token: string): Promise<AdminResp> {
  return new Promise((resolveP, rejectP) => {
    const headers: Record<string, string> = {};
    if (token) headers.Authorization = "Bearer " + token;
    const req = http.request({ socketPath, method: "GET", path, headers }, (res) => {
      const chunks: Buffer[] = [];
      res.on("data", (c) => chunks.push(c));
      res.on("end", () =>
        resolveP({
          status: res.statusCode ?? 0,
          body: Buffer.concat(chunks).toString("utf8"),
        }),
      );
    });
    req.on("error", rejectP);
    req.end();
  });
}

async function processExecutablePath(pid: number, argv?: string): Promise<string | undefined> {
  if (process.platform === "linux") {
    const p = await readlink(`/proc/${pid}/exe`).catch(() => "");
    if (p) return p.replace(/\s+\(deleted\)$/, "");
  }
  const lsof = await execText("lsof", ["-a", "-p", String(pid), "-d", "txt", "-Fn"]).catch(() => "");
  const names = lsof
    .split(/\r?\n/)
    .filter((line) => line.startsWith("n"))
    .map((line) => line.slice(1))
    .filter((line) => line && !line.endsWith("/dyld"));
  const agentCenter = names.find((name) => name.endsWith("/agent-center"));
  if (agentCenter) return agentCenter;
  if (names.length > 0) return names[0];

  const argv0 = argv?.trim().split(/\s+/)[0];
  if (argv0 && isAbsolute(argv0)) {
    return argv0;
  }
  return undefined;
}

async function processStart(pid: number): Promise<{ raw?: string; ms?: number }> {
  const raw = (await psText(pid, "lstart")).trim();
  if (!raw) return {};
  const ms = Date.parse(raw);
  return Number.isNaN(ms) ? { raw } : { raw, ms };
}

async function psText(pid: number, field: string): Promise<string> {
  return (await execText("ps", ["-p", String(pid), "-o", `${field}=`])).trim();
}

async function psNumber(pid: number, field: string): Promise<number | undefined> {
  const raw = await psText(pid, field).catch(() => "");
  const n = Number.parseInt(raw, 10);
  return Number.isFinite(n) ? n : undefined;
}

function execText(file: string, args: string[]): Promise<string> {
  return new Promise((resolveP, rejectP) => {
    execFile(file, args, { encoding: "utf8", maxBuffer: 4 * 1024 * 1024 }, (err, stdout, stderr) => {
      if (err) {
        err.message = `${err.message}; stderr=${String(stderr).slice(-1000)}`;
        rejectP(err);
        return;
      }
      resolveP(String(stdout));
    });
  });
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolveP) => setTimeout(resolveP, ms));
}
